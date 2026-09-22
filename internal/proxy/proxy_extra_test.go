package proxy

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strconv"
	"strings"
	"testing"
	"time"
)

func quietLog() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func TestDefaultAddr(t *testing.T) {
	want := "127.0.0.1:47653"
	if got := DefaultAddr(); got != want {
		t.Fatalf("DefaultAddr() = %q, ожидалось %q", got, want)
	}
}

// Порт и лог проверяются отдельно: и то, и другое подставляется молча, и
// ошибка здесь означала бы прокси, слушающий не то, что от него ждут.
func TestNewFixesBadArguments(t *testing.T) {
	for _, port := range []int{-1, 65536, 1 << 20} {
		if got := New(port, nil).addr; got != DefaultAddr() {
			t.Fatalf("порт %d не заменён на стандартный: %q", port, got)
		}
	}
	// Лог не задан: прокси обязан обойтись logger'ом по умолчанию, а не упасть
	// на первой же записи.
	if got := New(0, nil).log; got == nil {
		t.Fatal("без лога прокси остался без logger'а")
	}
}

func TestAddrAndRunningBeforeStart(t *testing.T) {
	s := New(0, quietLog())
	if got := s.Addr(); got != "" {
		t.Fatalf("не запущенный прокси сообщает адрес %q", got)
	}
	if s.Running() {
		t.Fatal("не запущенный прокси считает себя работающим")
	}
	// Остановка не запущенного прокси — не ошибка: выход из приложения не
	// должен зависеть от того, успел ли прокси подняться.
	if err := s.Stop(); err != nil {
		t.Fatalf("Stop до запуска вернул ошибку: %v", err)
	}
}

func TestStartIsIdempotent(t *testing.T) {
	s := testServer(t)
	addr := s.Addr()
	if err := s.Start(); err != nil {
		t.Fatalf("повторный Start вернул ошибку: %v", err)
	}
	if got := s.Addr(); got != addr {
		t.Fatalf("повторный Start сменил адрес: %q → %q", addr, got)
	}
}

func TestRuleCountFollowsRules(t *testing.T) {
	s := New(0, quietLog())
	if got := s.RuleCount(); got != 0 {
		t.Fatalf("без правил RuleCount = %d", got)
	}
	s.SetRules([]string{"youtube.com", "vk.com", "youtube.com"})
	if got := s.RuleCount(); got != 2 {
		t.Fatalf("RuleCount = %d, ожидалось 2", got)
	}
}

// До первой блокировки последнего отсечённого сайта нет — интерфейс должен
// получить пустую строку, а не панику на nil-указателе.
func TestLastBlockedIsEmptyInitially(t *testing.T) {
	if got := New(0, quietLog()).LastBlocked(); got != "" {
		t.Fatalf("LastBlocked() = %q, ожидалось пусто", got)
	}
}

// Прокси, который слушает порт, но не имеет адреса, прописывать браузерам
// нельзя: SelfTest обязан отказаться от такой проверки.
func TestSelfTestRejectsUnknownAddr(t *testing.T) {
	s := New(0, quietLog())
	// Состояние «слушает, но адреса нет» иначе не собрать: без слушателя
	// SelfTest откажется ещё раньше.
	s.mu.Lock()
	s.running = true
	s.mu.Unlock()

	if err := s.SelfTest("http://example.com/", time.Second); err == nil {
		t.Fatal("прокси без адреса не должен проходить проверку")
	}
}

// Запрос, который клиент не может даже отправить, — тоже провал проверки:
// слой нельзя включать, пока не доказано, что через него что-то открывается.
func TestSelfTestPropagatesClientError(t *testing.T) {
	s := testServer(t)
	if err := s.SelfTest("ftp://example.com/", time.Second); err == nil {
		t.Fatal("неподдерживаемая схема должна считаться провалом проверки")
	}
}

// ---- Разбор адреса и перебор портов ----

// withListen подменяет прослушивание сокета на время теста.
func withListen(t *testing.T, fn func(network, address string) (net.Listener, error)) {
	t.Helper()
	saved := listenTCP
	listenTCP = fn
	t.Cleanup(func() { listenTCP = saved })
}

func TestListenFallsBackOnBrokenAddr(t *testing.T) {
	var first, calls string
	withListen(t, func(_, address string) (net.Listener, error) {
		if calls == "" {
			first = address
		}
		calls += "x"
		return nil, errors.New("занято")
	})

	for _, addr := range []string{"без-двоеточия", "127.0.0.1:не-число"} {
		s := New(0, quietLog())
		s.addr = addr
		first, calls = "", ""
		if _, err := s.listen(); err == nil {
			t.Fatalf("адрес %q должен был привести к ошибке", addr)
		}
		// Оба негодных адреса обязаны свестись к стандартному: иначе прокси
		// молча слушал бы не то, что прописано браузерам.
		if first != DefaultAddr() {
			t.Fatalf("для адреса %q запрошен %q, ожидался %q", addr, first, DefaultAddr())
		}
	}
}

func TestListenExhaustsPorts(t *testing.T) {
	withListen(t, func(_, _ string) (net.Listener, error) {
		return nil, errors.New("занято")
	})

	s := New(DefaultPort, quietLog())
	_, err := s.listen()
	if err == nil {
		t.Fatal("при занятых портах ожидалась ошибка")
	}
	if !strings.Contains(err.Error(), "не удалось занять ни один порт") {
		t.Fatalf("неожиданная ошибка: %v", err)
	}

	// Start обязан сообщить ту же ошибку наружу: без прокси приложение
	// переходит на запасной слой, и оно должно об этом узнать.
	if err := s.Start(); err == nil {
		t.Fatal("Start при занятых портах должен вернуть ошибку")
	}
	if s.Running() {
		t.Fatal("прокси не поднялся, но считает себя работающим")
	}
}

func TestListenTakesNextFreePort(t *testing.T) {
	// Первый запрошенный порт «занят», второй свободен: перебор обязан дойти
	// до второго, а не сдаться.
	var asked []string
	withListen(t, func(_, address string) (net.Listener, error) {
		asked = append(asked, address)
		if len(asked) == 1 {
			return nil, errors.New("занято")
		}
		// Отдаём настоящий слушатель: дальше проверяется уже другое — что
		// прокси использует второй порт.
		ln, err := net.Listen("tcp", "127.0.0.1:0")
		return ln, err
	})

	s := New(DefaultPort, quietLog())
	ln, err := s.listen()
	if err != nil {
		t.Fatalf("перебор портов не дошёл до свободного: %v", err)
	}
	defer ln.Close()

	if len(asked) != 2 {
		t.Fatalf("запрошено %d адресов, ожидалось 2: %v", len(asked), asked)
	}
	if asked[0] != DefaultAddr() {
		t.Fatalf("первым запрошен %q, ожидался %q", asked[0], DefaultAddr())
	}
	want := net.JoinHostPort("127.0.0.1", strconv.Itoa(DefaultPort+1))
	if asked[1] != want {
		t.Fatalf("вторым запрошен %q, ожидался %q", asked[1], want)
	}
}

// serveFail — слушатель, который сразу отдаёт ошибку: так проверяется ветка
// «Serve упал не из-за закрытия», до которой на живом сокете не добраться.
type serveFail struct{ err error }

func (l serveFail) Accept() (net.Conn, error) { return nil, l.err }
func (l serveFail) Close() error              { return nil }
func (l serveFail) Addr() net.Addr            { return fakeAddr("127.0.0.1:1") }

type fakeAddr string

func (a fakeAddr) Network() string { return "tcp" }
func (a fakeAddr) String() string  { return string(a) }

func TestStartLogsUnexpectedServeFailure(t *testing.T) {
	var logs bytes.Buffer
	withListen(t, func(_, _ string) (net.Listener, error) {
		return serveFail{err: errors.New("сокет отвалился")}, nil
	})

	s := New(DefaultPort, slog.New(slog.NewTextHandler(&logs, nil)))
	if err := s.Start(); err != nil {
		t.Fatalf("Start вернул ошибку: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })

	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		if strings.Contains(logs.String(), "прокси остановлен") {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("сбой Serve не попал в журнал: %s", logs.String())
}

// ---- Запросы ----

// Запрос без адресата разбирать нечем: прокси обязан ответить 400, а не
// угадывать, кого блокировать.
func TestServeHTTPWithoutHost(t *testing.T) {
	s := New(0, quietLog())
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, &http.Request{
		Method: http.MethodGet,
		Host:   "",
		URL:    &url.URL{},
		Header: http.Header{},
	})
	if rec.Code != http.StatusBadRequest {
		t.Fatalf("ожидался 400, получен %d", rec.Code)
	}
}

// Если своя страница-заглушка не задана, заблокированный HTTP-сайт обязан
// получить хотя бы внятный ответ, а не пустое соединение.
func TestBlockedRequestWithoutCustomPage(t *testing.T) {
	s := New(0, quietLog())
	s.SetRules([]string{"vk.com"})
	s.SetEnabled(true)

	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, &http.Request{
		Method: http.MethodGet,
		Host:   "vk.com",
		URL:    &url.URL{Host: "vk.com", Path: "/"},
		Header: http.Header{},
	})
	if rec.Code != http.StatusForbidden {
		t.Fatalf("ожидался 403, получен %d", rec.Code)
	}
	if !strings.Contains(rec.Body.String(), "Заблокировано Focusd") {
		t.Fatalf("неожиданное тело ответа: %q", rec.Body.String())
	}
}

// CONNECT без порта тоже встречается: считаем, что это HTTPS.
func TestConnectWithoutPortIsRejected(t *testing.T) {
	s := testServer(t)

	conn, err := net.DialTimeout("tcp", s.Addr(), 3*time.Second)
	if err != nil {
		t.Fatalf("не подключиться к прокси: %v", err)
	}
	defer conn.Close()

	if _, err := fmt.Fprintf(conn, "CONNECT 127.0.0.1 HTTP/1.1\r\nHost: 127.0.0.1\r\n\r\n"); err != nil {
		t.Fatalf("не отправить CONNECT: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("не прочитать ответ: %v", err)
	}
	defer resp.Body.Close()

	// На 127.0.0.1:443 никто не слушает — значит, адресат недоступен.
	if resp.StatusCode != http.StatusBadGateway {
		t.Fatalf("ожидался 502, получен %d", resp.StatusCode)
	}
	if _, failed := s.Health(); failed != 1 {
		t.Fatalf("неудачная попытка не посчитана: failed=%d", failed)
	}
}

// ResponseWriter, который не умеет перехватывать соединение: CONNECT через
// такой обязан обернуться 500 и закрытым соединением к адресату.
func TestTunnelWithoutHijacker(t *testing.T) {
	origin, port := localEcho(t)
	defer origin.Close()

	s := New(0, quietLog())
	req := &http.Request{
		Method: http.MethodConnect,
		Host:   net.JoinHostPort("127.0.0.1", port),
		Header: http.Header{},
	}
	rec := httptest.NewRecorder()
	s.tunnel(rec, req, "127.0.0.1")

	if rec.Code != http.StatusInternalServerError {
		t.Fatalf("ожидался 500, получен %d", rec.Code)
	}
	if _, failed := s.Health(); failed != 1 {
		t.Fatalf("неудачная попытка не посчитана: failed=%d", failed)
	}
}

// hijackFake — ResponseWriter с перехватом соединения: отдаёт заранее
// подготовленную пару, поэтому проверяются ветки, недостижимые на живом
// сокете (ошибка перехвата, ошибка записи, ошибка сброса буфера).
type hijackFake struct {
	header http.Header
	conn   net.Conn
	rw     *bufio.ReadWriter
	err    error
}

func (f *hijackFake) Header() http.Header         { return f.header }
func (f *hijackFake) Write(b []byte) (int, error) { return len(b), nil }
func (f *hijackFake) WriteHeader(int)             {}

func (f *hijackFake) Hijack() (net.Conn, *bufio.ReadWriter, error) {
	if f.err != nil {
		return nil, nil, f.err
	}
	return f.conn, f.rw, nil
}

// deadConn — соединение, любая запись в которое обрывается ошибкой.
type deadConn struct{ err error }

func (c deadConn) Read([]byte) (int, error)         { return 0, c.err }
func (c deadConn) Write([]byte) (int, error)        { return 0, c.err }
func (c deadConn) Close() error                     { return nil }
func (c deadConn) LocalAddr() net.Addr              { return fakeAddr("local") }
func (c deadConn) RemoteAddr() net.Addr             { return fakeAddr("remote") }
func (c deadConn) SetDeadline(time.Time) error      { return nil }
func (c deadConn) SetReadDeadline(time.Time) error  { return nil }
func (c deadConn) SetWriteDeadline(time.Time) error { return nil }

func tunnelRequest(t *testing.T, port string) *http.Request {
	t.Helper()
	return &http.Request{
		Method: http.MethodConnect,
		Host:   net.JoinHostPort("127.0.0.1", port),
		Header: http.Header{},
	}
}

func TestTunnelHijackFails(t *testing.T) {
	origin, port := localEcho(t)
	defer origin.Close()

	s := New(0, quietLog())
	w := &hijackFake{header: http.Header{}, err: errors.New("перехват не удался")}
	s.tunnel(w, tunnelRequest(t, port), "127.0.0.1")

	if _, failed := s.Health(); failed != 1 {
		t.Fatalf("неудачная попытка не посчитана: failed=%d", failed)
	}
}

// Буфер меньше строки ответа: запись в него обязана вернуть ошибку, и туннель
// не должен остаться висеть.
func TestTunnelWriteFails(t *testing.T) {
	origin, port := localEcho(t)
	defer origin.Close()

	s := New(0, quietLog())
	boom := errors.New("сокет закрыт")
	w := &hijackFake{
		header: http.Header{},
		conn:   deadConn{err: boom},
		rw: bufio.NewReadWriter(
			bufio.NewReader(deadConn{err: boom}),
			bufio.NewWriterSize(deadConn{err: boom}, 4),
		),
	}
	s.tunnel(w, tunnelRequest(t, port), "127.0.0.1")

	if _, failed := s.Health(); failed != 1 {
		t.Fatalf("неудачная попытка не посчитана: failed=%d", failed)
	}
	if forwarded, _ := s.Health(); forwarded != 0 {
		t.Fatal("сорванный туннель не должен считаться пропущенным")
	}
}

// Буфер достаточно велик, чтобы ответ записался, но сброс на диск обязан
// упасть — и это тоже срыв туннеля.
func TestTunnelFlushFails(t *testing.T) {
	origin, port := localEcho(t)
	defer origin.Close()

	s := New(0, quietLog())
	boom := errors.New("сокет закрыт")
	w := &hijackFake{
		header: http.Header{},
		conn:   deadConn{err: boom},
		rw: bufio.NewReadWriter(
			bufio.NewReader(deadConn{err: boom}),
			bufio.NewWriterSize(deadConn{err: boom}, 1024),
		),
	}
	s.tunnel(w, tunnelRequest(t, port), "127.0.0.1")

	if _, failed := s.Health(); failed != 1 {
		t.Fatalf("неудачная попытка не посчитана: failed=%d", failed)
	}
}

// Заголовки, относящиеся только к текущему соединению, дальше не идут — и
// перечисленные в Connection тоже.
func TestStripHopHeaders(t *testing.T) {
	h := http.Header{}
	h.Set("Connection", "keep-alive, X-Своё")
	h.Set("X-Своё", "значение")
	h.Set("Keep-Alive", "timeout=5")
	h.Set("Proxy-Authorization", "Basic секрет")
	h.Set("X-Нужное", "остаётся")

	stripHopHeaders(h)

	if got := h.Get("X-Нужное"); got != "остаётся" {
		t.Errorf("полезный заголовок потерялся: %q", got)
	}
	for _, name := range []string{"Connection", "Keep-Alive", "Proxy-Authorization", "X-Своё"} {
		if got := h.Get(name); got != "" {
			t.Errorf("заголовок %q не отброшен: %q", name, got)
		}
	}

	// Соединение с пустыми именами в списке не должно ничего ломать.
	h2 := http.Header{"Connection": {" , , "}, "X-Остаток": {"да"}}
	stripHopHeaders(h2)
	if h2.Get("X-Остаток") != "да" {
		t.Error("разбор пустого списка Connection испортил заголовки")
	}
}
