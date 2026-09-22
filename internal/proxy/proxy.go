// Package proxy реализует локальный HTTP-прокси — основной слой блокировки
// сайтов в браузерах.
//
// Зачем он нужен, если уже есть политика URLBlocklist. Политика обрывает запрос
// внутри браузера, до выхода в сеть, и потому непробиваема — но она же делает
// блокировку ненаблюдаемой: снаружи нельзя узнать, сколько запросов отсечено и
// какие именно сайты пытались открыть. Прокси стоит на пути трафика, поэтому
// видит каждую попытку и может её посчитать, а заодно показать нашу
// страницу-заглушку.
//
// Наружу прокси смотрит только вбок: он слушает 127.0.0.1 и говорит на языке
// HTTP. Заблокированные имена он не пропускает ни в каком виде — ни через
// CONNECT для HTTPS, ни обычным запросом с абсолютным URL. DoH и VPN этому слою
// не мешают: браузер обращается к прокси раньше, чем куда-либо ещё.
package proxy

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"focusd/internal/rules"
)

// DefaultPort — порт прокси по умолчанию. Значение выбрано вдали от популярных
// (3000, 8080, 8888): прокси живёт всё время работы приложения, и занимать
// чужой порт нельзя.
const DefaultPort = 47653

// PortScanLimit — сколько портов подряд перебрать, если стандартный занят.
const PortScanLimit = 24

// DefaultAddr — адрес, который прокси занимает по умолчанию.
//
// Нужен тем, кто разбирает настройки без запущенного прокси: сторожу прокси и
// аварийному восстановлению. Они узнают по нему собственные следы в реестре,
// когда возвращать настройки уже нечем.
func DefaultAddr() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(DefaultPort))
}

const dialTimeout = 10 * time.Second

// BlockPage рисует страницу-заглушку для заблокированного HTTP-запроса.
type BlockPage func(w http.ResponseWriter, r *http.Request, host string)

// Server — локальный прокси.
type Server struct {
	addr  string
	rules rules.Atomic
	// allow хранит исключения: они перебивают блокировку.
	allow   rules.Atomic
	enabled atomic.Bool

	requests atomic.Int64
	blocked  atomic.Int64
	// forwarded — запросы, доехавшие до адресата, failed — те, где прокси не
	// смог установить соединение. Разница между ними и есть признак поломки:
	// если браузер стучится, а forwarded не растёт, значит прокси ломает сеть,
	// и политику нужно немедленно снять.
	forwarded atomic.Int64
	failed    atomic.Int64
	// bypassed — запросы, пропущенные из-за исключения. Нужны интерфейсу,
	// чтобы «исключение» не выглядело как «блокировка не сработала».
	bypassed atomic.Int64

	lastBlocked atomic.Pointer[string]

	page BlockPage
	tr   *http.Transport

	mu      sync.Mutex
	ln      net.Listener
	srv     *http.Server
	running bool

	// tunnels — живые CONNECT-туннели. Нужны, чтобы рвать уже открытые
	// соединения к заблокированным сайтам в момент начала сессии: см.
	// cutBlockedTunnels.
	tmu     sync.Mutex
	tunnels map[*tunnel]struct{}

	log *slog.Logger
}

// tunnel — открытый CONNECT-туннель: имя адресата и способ его закрыть.
type tunnel struct {
	host  string
	close func()
}

// New создаёт прокси, который будет слушать 127.0.0.1:port. Порт 0 означает
// «любой свободный» — так прокси поднимается в тестах.
func New(port int, log *slog.Logger) *Server {
	if log == nil {
		log = slog.Default()
	}
	if port < 0 || port > 65535 {
		port = DefaultPort
	}
	return &Server{
		addr: net.JoinHostPort("127.0.0.1", strconv.Itoa(port)),
		tr: &http.Transport{
			DialContext: (&net.Dialer{
				Timeout:   dialTimeout,
				KeepAlive: 30 * time.Second,
			}).DialContext,
			MaxIdleConns:          64,
			MaxIdleConnsPerHost:   8,
			IdleConnTimeout:       60 * time.Second,
			TLSHandshakeTimeout:   dialTimeout,
			ExpectContinueTimeout: time.Second,
			ForceAttemptHTTP2:     true,
		},
		log: log,
	}
}

// SetRules заменяет набор блокируемых доменов. Безопасно вызывать на ходу.
func (s *Server) SetRules(domains []string) { s.rules.Store(rules.New(domains)) }

// SetAllowlist заменяет набор исключений.
func (s *Server) SetAllowlist(domains []string) { s.allow.Store(rules.New(domains)) }

// SetEnabled включает и выключает блокировку, не останавливая прокси.
//
// Включение рвёт уже открытые туннели к заблокированным сайтам. Без этого
// блокировка решала судьбу только новых соединений: ютуб, открытый до начала
// фокуса, продолжал играть по туннелю, установленному раньше, и выглядело это
// как «фокус не работает». Проверяется это просто: в приватном окне того же
// браузера тот же сайт уже не открывается — там соединений ещё нет.
func (s *Server) SetEnabled(v bool) {
	s.enabled.Store(v)
	if v {
		s.cutBlockedTunnels()
	}
}

// cutBlockedTunnels закрывает туннели, адресат которых попал под блокировку.
func (s *Server) cutBlockedTunnels() {
	cut := s.tunnelsMatching(false)
	for _, t := range cut {
		t.close()
	}
	if len(cut) > 0 {
		s.log.Info("закрыты соединения к заблокированным сайтам", "count", len(cut))
	}
}

// tunnelsMatching возвращает живые туннели: при all — все, иначе только те, чей
// адресат заблокирован и не исключён.
func (s *Server) tunnelsMatching(all bool) []*tunnel {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	var out []*tunnel
	for t := range s.tunnels {
		if all || (s.rules.Match(t.host) && !s.allow.Match(t.host)) {
			out = append(out, t)
		}
	}
	return out
}

func (s *Server) addTunnel(t *tunnel) {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	if s.tunnels == nil {
		s.tunnels = make(map[*tunnel]struct{})
	}
	s.tunnels[t] = struct{}{}
}

func (s *Server) removeTunnel(t *tunnel) {
	s.tmu.Lock()
	defer s.tmu.Unlock()
	delete(s.tunnels, t)
}

// SetBlockPage задаёт отрисовку страницы-заглушки.
func (s *Server) SetBlockPage(p BlockPage) { s.page = p }

// Addr возвращает адрес, который фактически слушает прокси, — именно его нужно
// прописать браузерам. Пустая строка, пока прокси не запущен.
func (s *Server) Addr() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.ln == nil {
		return ""
	}
	return s.ln.Addr().String()
}

// Running сообщает, слушает ли прокси сокет.
func (s *Server) Running() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.running
}

// RuleCount возвращает число доменов в текущем наборе правил.
func (s *Server) RuleCount() int { return s.rules.Len() }

// Stats возвращает число пропущенных через прокси запросов и число отсечённых.
func (s *Server) Stats() (requests, blocked int64) {
	return s.requests.Load(), s.blocked.Load()
}

// Health возвращает число запросов, доехавших до адресата, и число неудачных
// попыток соединения. По ним видно, работает ли прокси на самом деле: сам факт
// «запросы идут» ничего не доказывает, а вот нулевой forwarded при растущем
// requests означает, что через прокси не открывается вообще ничего.
func (s *Server) Health() (forwarded, failed int64) {
	return s.forwarded.Load(), s.failed.Load()
}

// SelfTest проверяет на живом запросе, что прокси умеет выходить в интернет.
//
// Это страховка от худшего исхода: если прокси поднялся, но наружу не ходит,
// прописанная браузерам политика оставит человека без единой открывающейся
// страницы. Лучше не включать этот слой вовсе, чем включать сломанным.
func (s *Server) SelfTest(target string, timeout time.Duration) error {
	if !s.Running() {
		return errors.New("прокси не слушает порт")
	}
	addr := s.Addr()
	if addr == "" {
		return errors.New("неизвестен адрес прокси")
	}

	client := &http.Client{
		Timeout: timeout,
		Transport: &http.Transport{
			Proxy: func(*http.Request) (*url.URL, error) {
				return url.Parse("http://" + addr)
			},
			DisableKeepAlives: true,
		},
	}
	resp, err := client.Get(target)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	_, _ = io.Copy(io.Discard, io.LimitReader(resp.Body, 4096))
	if resp.StatusCode >= 400 {
		return fmt.Errorf("адресат ответил %d", resp.StatusCode)
	}
	return nil
}

// LastBlocked возвращает последнее отсечённое имя, если оно было.
func (s *Server) LastBlocked() string {
	if p := s.lastBlocked.Load(); p != nil {
		return *p
	}
	return ""
}

// Start поднимает слушающий сокет. Если заданный порт занят, берётся следующий
// свободный: прокси — часть защиты, и отказываться от неё из-за чужой программы
// на порту нельзя.
func (s *Server) Start() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.running {
		return nil
	}

	ln, err := s.listen()
	if err != nil {
		return err
	}

	s.srv = &http.Server{
		Handler:           s,
		ReadHeaderTimeout: 10 * time.Second,
		IdleTimeout:       90 * time.Second,
	}
	s.ln = ln
	s.running = true

	srv := s.srv
	go func() {
		if err := srv.Serve(ln); err != nil && !errors.Is(err, http.ErrServerClosed) && !errors.Is(err, net.ErrClosed) {
			s.log.Warn("прокси остановлен", "addr", ln.Addr().String(), "err", err)
		}
	}()

	s.log.Info("прокси запущен", "addr", ln.Addr().String())
	return nil
}

// listenTCP — точка подмены для тестов. Настоящий net.Listen здесь не
// пригодится: ветки «адрес не разобрать» и «все порты заняты» иначе не
// исполнить, не занимая два десятка настоящих портов и не полагаясь на то, что
// стандартный порт свободен.
var listenTCP func(network, address string) (net.Listener, error) = net.Listen

func (s *Server) listen() (net.Listener, error) {
	host, port, err := net.SplitHostPort(s.addr)
	if err != nil {
		host, port = "127.0.0.1", strconv.Itoa(DefaultPort)
	}
	start, err := strconv.Atoi(port)
	if err != nil {
		start = DefaultPort
	}
	if start == 0 {
		// Явно запрошен любой свободный порт — перебирать нечего.
		return listenTCP("tcp", net.JoinHostPort(host, "0"))
	}

	var lastErr error
	for i := 0; i < PortScanLimit; i++ {
		addr := net.JoinHostPort(host, strconv.Itoa(start+i))
		ln, err := listenTCP("tcp", addr)
		if err == nil {
			return ln, nil
		}
		lastErr = err
	}
	return nil, errors.New("не удалось занять ни один порт для прокси: " + lastErr.Error())
}

// Stop гасит прокси. Перед этим обязательно нужно снять политику браузеров:
// прокси, который держат в политике, но не подняли, оставит браузер без сети.
func (s *Server) Stop() error {
	s.mu.Lock()
	srv, ln := s.srv, s.ln
	s.srv, s.ln, s.running = nil, nil, false
	s.mu.Unlock()

	if srv == nil {
		return nil
	}
	s.tr.CloseIdleConnections()
	// http.Server не закрывает соединения, отданные в Hijack, поэтому туннели
	// рвём сами: иначе они переживут остановку прокси.
	for _, t := range s.tunnelsMatching(true) {
		t.close()
	}
	if ln != nil {
		_ = ln.Close()
	}
	return srv.Close()
}

// ServeHTTP разбирает запрос и решает, пускать его дальше или нет.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	host := requestHost(r)
	if host == "" {
		http.Error(w, "Focusd: не удалось определить адресата запроса", http.StatusBadRequest)
		return
	}

	if !s.enabled.Load() {
		// Вне сессии прокси ничего не блокирует и не считает: он просто
		// прозрачно пропускает трафик.
		//
		// Туннель для HTTPS обязателен и здесь. Однажды эта ветка звала forward
		// для всех запросов подряд, включая CONNECT, — и получалось, что после
		// выключения фокуса браузер переставал открывать вообще всё, потому что
		// HTTPS через прокси переставал работать вовсе. Отпускать блокировку
		// нужно так же аккуратно, как и ставить её.
		if r.Method == http.MethodConnect {
			s.tunnel(w, r, host)
			return
		}
		s.forward(w, r)
		return
	}

	s.requests.Add(1)

	if s.allow.Match(host) {
		s.bypassed.Add(1)
	} else if s.rules.Match(host) {
		s.blocked.Add(1)
		h := host
		s.lastBlocked.Store(&h)
		s.serveBlocked(w, r, host)
		return
	}

	if r.Method == http.MethodConnect {
		s.tunnel(w, r, host)
		return
	}
	s.forward(w, r)
}

func (s *Server) serveBlocked(w http.ResponseWriter, r *http.Request, host string) {
	if r.Method == http.MethodConnect {
		// Через CONNECT идёт HTTPS: ответ прокси браузер не покажет, он лишь
		// скажет, что соединение не установлено. Страницу рисуем только для
		// обычного HTTP, где есть куда её вставить.
		http.Error(w, "Заблокировано Focusd", http.StatusForbidden)
		return
	}
	if s.page != nil {
		s.page(w, r, host)
		return
	}
	http.Error(w, "Заблокировано Focusd", http.StatusForbidden)
}

// tunnel пропускает CONNECT-туннель (HTTPS) к незаблокированному адресату.
//
// host приходит из ServeHTTP: это имя адресата без порта, по нему туннель и
// опознаётся, когда приходит время его закрыть.
func (s *Server) tunnel(w http.ResponseWriter, r *http.Request, host string) {
	target := r.Host
	if _, _, err := net.SplitHostPort(target); err != nil {
		// Браузер всегда присылает «хост:порт», но CONNECT без порта тоже
		// встречается: считаем, что это HTTPS.
		target = net.JoinHostPort(target, "443")
	}

	dst, err := net.DialTimeout("tcp", target, dialTimeout)
	if err != nil {
		s.failed.Add(1)
		http.Error(w, "Focusd: адресат недоступен", http.StatusBadGateway)
		return
	}

	hj, ok := w.(http.Hijacker)
	if !ok {
		s.failed.Add(1)
		_ = dst.Close()
		http.Error(w, "Focusd: не удалось открыть туннель", http.StatusInternalServerError)
		return
	}
	client, buf, err := hj.Hijack()
	if err != nil {
		s.failed.Add(1)
		_ = dst.Close()
		return
	}

	if _, err := buf.WriteString("HTTP/1.1 200 Connection Established\r\n\r\n"); err != nil {
		s.failed.Add(1)
		_ = client.Close()
		_ = dst.Close()
		return
	}
	if err := buf.Flush(); err != nil {
		s.failed.Add(1)
		_ = client.Close()
		_ = dst.Close()
		return
	}

	s.forwarded.Add(1)

	// Туннель запоминается, чтобы его можно было закрыть, когда сессия начнётся
	// или когда набор блокируемых доменов изменится: браузер держит соединение
	// живым, и без этого блокировка его не касается.
	var once sync.Once
	closeBoth := func() {
		once.Do(func() {
			_ = client.Close()
			_ = dst.Close()
		})
	}
	t := &tunnel{host: host, close: closeBoth}
	s.addTunnel(t)
	defer s.removeTunnel(t)

	pipe(client, dst, closeBoth)
}

// forward передаёт обычный (не CONNECT) запрос адресату.
func (s *Server) forward(w http.ResponseWriter, r *http.Request) {
	out := r.Clone(r.Context())
	out.RequestURI = ""
	stripHopHeaders(out.Header)
	// Прокси не должен передавать адресату свою авторизацию.
	out.Header.Del("Proxy-Authorization")

	resp, err := s.tr.RoundTrip(out)
	if err != nil {
		s.failed.Add(1)
		http.Error(w, "Focusd: сайт недоступен", http.StatusBadGateway)
		return
	}
	defer resp.Body.Close()

	s.forwarded.Add(1)
	stripHopHeaders(resp.Header)
	copyHeader(w.Header(), resp.Header)
	w.WriteHeader(resp.StatusCode)
	_, _ = io.Copy(w, resp.Body)
}

// pipe соединяет два сокета в обе стороны и закрывает оба, когда любая из
// сторон закончила: так туннель не остаётся висеть после закрытия вкладки.
// closeBoth приходит снаружи — тот же способ закрыть туннель нужен и тому, кто
// решает оборвать его досрочно (например, начавшаяся сессия).
func pipe(a, b net.Conn, closeBoth func()) {
	go func() {
		_, _ = io.Copy(a, b)
		closeBoth()
	}()
	_, _ = io.Copy(b, a)
	closeBoth()
}

// requestHost достаёт имя хоста из запроса в любом из двух видов, в которых его
// присылает браузер: CONNECT host:port и абсолютный URL в строке запроса.
func requestHost(r *http.Request) string {
	raw := r.Host
	if r.URL != nil && r.URL.Host != "" {
		raw = r.URL.Host
	}
	if raw == "" {
		return ""
	}
	if h, _, err := net.SplitHostPort(raw); err == nil {
		raw = h
	}
	return strings.Trim(strings.ToLower(raw), "[]")
}

// hopHeaders — заголовки, которые описывают одно соединение и не должны
// пересылаться дальше.
var hopHeaders = []string{
	"Connection",
	"Proxy-Connection",
	"Keep-Alive",
	"Proxy-Authenticate",
	"Proxy-Authorization",
	"Te",
	"Trailer",
	"Transfer-Encoding",
	"Upgrade",
}

func stripHopHeaders(h http.Header) {
	// Connection перечисляет заголовки, относящиеся только к этому соединению.
	for _, v := range h.Values("Connection") {
		for _, name := range strings.Split(v, ",") {
			if name = strings.TrimSpace(name); name != "" {
				h.Del(name)
			}
		}
	}
	for _, name := range hopHeaders {
		h.Del(name)
	}
}

func copyHeader(dst, src http.Header) {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
}
