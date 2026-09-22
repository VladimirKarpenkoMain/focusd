package proxy

import (
	"bufio"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"strings"
	"testing"
	"time"
)

func testServer(t *testing.T) *Server {
	t.Helper()
	s := New(0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(); err != nil {
		t.Fatalf("не удалось поднять прокси: %v", err)
	}
	t.Cleanup(func() { _ = s.Stop() })
	return s
}

// proxyClient возвращает http.Client, который ходит через тестовый прокси.
func proxyClient(t *testing.T, s *Server) *http.Client {
	t.Helper()
	u, err := url.Parse("http://" + s.Addr())
	if err != nil {
		t.Fatalf("не разобрать адрес прокси: %v", err)
	}
	return &http.Client{
		Timeout:   5 * time.Second,
		Transport: &http.Transport{Proxy: http.ProxyURL(u)},
	}
}

// connectThroughProxy отправляет CONNECT и возвращает ответ прокси.
func connectThroughProxy(t *testing.T, s *Server, hostport string) *http.Response {
	t.Helper()
	conn, err := net.DialTimeout("tcp", s.Addr(), 3*time.Second)
	if err != nil {
		t.Fatalf("не подключиться к прокси: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })

	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", hostport, hostport); err != nil {
		t.Fatalf("не отправить CONNECT: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("не прочитать ответ на CONNECT: %v", err)
	}
	t.Cleanup(func() { _ = resp.Body.Close() })
	return resp
}

func TestConnectToBlockedHostIsCut(t *testing.T) {
	s := testServer(t)
	s.SetRules([]string{"youtube.com"})
	s.SetEnabled(true)

	resp := connectThroughProxy(t, s, "youtube.com:443")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("ожидался 403, получено %d", resp.StatusCode)
	}

	if _, blocked := s.Stats(); blocked != 1 {
		t.Fatalf("счётчик отсечённых: ожидалось 1, получено %d", blocked)
	}
	if got := s.LastBlocked(); got != "youtube.com" {
		t.Fatalf("последний отсечённый: ожидалось youtube.com, получено %q", got)
	}
}

// Правило накрывает поддомены — иначе блокировка обходилась бы одним «www.».
func TestSubdomainIsCovered(t *testing.T) {
	s := testServer(t)
	s.SetRules([]string{"youtube.com"})
	s.SetEnabled(true)

	resp := connectThroughProxy(t, s, "music.youtube.com:443")
	if resp.StatusCode != http.StatusForbidden {
		t.Fatalf("поддомен не заблокирован: получен %d", resp.StatusCode)
	}
}

// Исключение перебивает блокировку: это обещание интерфейса, а не деталь.
func TestAllowlistBeatsBlocklist(t *testing.T) {
	s := testServer(t)
	s.SetRules([]string{"youtube.com"})
	s.SetAllowlist([]string{"music.youtube.com"})
	s.SetEnabled(true)

	resp := connectThroughProxy(t, s, "music.youtube.com:443")
	if resp.StatusCode == http.StatusForbidden {
		t.Fatal("исключение не сработало: запрос отсечён")
	}
	// Адресат недоступен, но это уже не наша блокировка.
	if _, blocked := s.Stats(); blocked != 0 {
		t.Fatalf("исключённый запрос попал в счётчик отсечённых: %d", blocked)
	}
}

// Вне сессии прокси обязан молча пропускать трафик: политика могла остаться
// с прошлого раза, и превращать это в блокировку нельзя.
//
// Проверка смотрит именно на успешный туннель, а не на «не 403». Это важно:
// однажды выключенная ветка звала общий forward для CONNECT, туннель не
// открывался, и после конца сессии у человека переставали открываться вообще
// все сайты — браузер показывал ERR_TUNNEL_CONNECTION_FAILED на каждом.
func TestDisabledProxyForwards(t *testing.T) {
	// Локальный адресат: проверяем сам факт туннеля, а не чужой сервер.
	origin, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось поднять адресата: %v", err)
	}
	defer origin.Close()
	go func() {
		for {
			conn, err := origin.Accept()
			if err != nil {
				return
			}
			_ = conn.Close()
		}
	}()

	s := testServer(t)
	s.SetRules([]string{"youtube.com"})

	resp := connectThroughProxy(t, s, origin.Addr().String())
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("выключенный прокси не открыл туннель: %s", resp.Status)
	}

	requests, blocked := s.Stats()
	if requests != 0 || blocked != 0 {
		t.Fatalf("выключенный прокси считает запросы: requests=%d blocked=%d", requests, blocked)
	}
}

// Обычный HTTP вне сессии тоже проходит, и тоже без счёта.
func TestDisabledProxyForwardsPlainRequest(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ок")
	}))
	defer origin.Close()

	s := testServer(t)
	s.SetRules([]string{"youtube.com"})

	resp, err := proxyClient(t, s).Get(origin.URL)
	if err != nil {
		t.Fatalf("запрос через выключенный прокси не прошёл: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("ожидался 200, получен %d", resp.StatusCode)
	}
	if requests, blocked := s.Stats(); requests != 0 || blocked != 0 {
		t.Fatalf("выключенный прокси считает запросы: requests=%d blocked=%d", requests, blocked)
	}
}

// Туннель, открытый ДО начала сессии, обязан быть закрыт, когда сессия
// началась.
//
// Это и есть та поломка, из-за которой блокировка «не работала»: браузер держит
// соединение к сайту живым, и ютуб, открытый до фокуса, продолжал играть через
// него. Проверялось это так: в приватном окне того же браузера сайт уже не
// открывался — там соединений ещё нет. Отсюда и требование: закрывать чужие
// живые туннели к заблокированным адресатам в момент включения блокировки.
func TestEnablingCutsTunnelsToBlockedHosts(t *testing.T) {
	blocked, blockedPort := localEcho(t)
	defer blocked.Close()
	other, otherPort := localEcho(t)
	defer other.Close()

	s := testServer(t)
	// Сессии ещё нет — ровно как у человека, который открыл ютуб до фокуса.
	// Адресаты локальные: проверяем настоящие туннели, а не «не 403».
	// Имя адресата берётся из самого CONNECT, а правило из одной метки
	// («localhost») недействительно: набор правил такие отбрасывает. Поэтому
	// блокируем по адресу, а «соседа» берём под именем, на которое правило не
	// распространяется.
	blockedConn := openTunnel(t, s, net.JoinHostPort("127.0.0.1", blockedPort))
	otherConn := openTunnel(t, s, net.JoinHostPort("localhost", otherPort))

	s.SetRules([]string{"127.0.0.1"})
	s.SetEnabled(true)

	if !closedWithin(blockedConn) {
		t.Fatal("туннель к заблокированному сайту пережил начало сессии")
	}
	// Соседний туннель трогать нельзя: блокировка — не «оборвать всё».
	if closedWithin(otherConn) {
		t.Fatal("начало сессии оборвало туннель к незаблокированному сайту")
	}
}

// Исключение спасает живой туннель так же, как спасает новый запрос.
func TestEnablingKeepsAllowlistedTunnel(t *testing.T) {
	origin, port := localEcho(t)
	defer origin.Close()

	s := testServer(t)
	conn := openTunnel(t, s, net.JoinHostPort("127.0.0.1", port))

	s.SetRules([]string{"127.0.0.1"})
	s.SetAllowlist([]string{"127.0.0.1"})
	s.SetEnabled(true)

	if closedWithin(conn) {
		t.Fatal("исключение не спасло живой туннель: соединение закрыто")
	}
}

// localEcho поднимает локального адресата и возвращает его слушателя и порт.
func localEcho(t *testing.T) (net.Listener, string) {
	t.Helper()
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось поднять адресата: %v", err)
	}
	go func() {
		for {
			conn, err := ln.Accept()
			if err != nil {
				return
			}
			go func() { _, _ = io.Copy(io.Discard, conn) }()
		}
	}()
	_, port, err := net.SplitHostPort(ln.Addr().String())
	if err != nil {
		t.Fatalf("не разобрать адрес адресата: %v", err)
	}
	return ln, port
}

// openTunnel открывает CONNECT-туннель и возвращает клиентское соединение.
func openTunnel(t *testing.T, s *Server, hostport string) net.Conn {
	t.Helper()
	conn, err := net.DialTimeout("tcp", s.Addr(), 3*time.Second)
	if err != nil {
		t.Fatalf("не подключиться к прокси: %v", err)
	}
	t.Cleanup(func() { _ = conn.Close() })
	if _, err := fmt.Fprintf(conn, "CONNECT %s HTTP/1.1\r\nHost: %s\r\n\r\n", hostport, hostport); err != nil {
		t.Fatalf("не отправить CONNECT: %v", err)
	}
	_ = conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	resp, err := http.ReadResponse(bufio.NewReader(conn), &http.Request{Method: http.MethodConnect})
	if err != nil {
		t.Fatalf("не прочитать ответ на CONNECT: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("туннель не открылся: %s", resp.Status)
	}
	_ = conn.SetReadDeadline(time.Time{})
	return conn
}

// closedWithin сообщает, закрылся ли туннель: чтение обязано вернуть ошибку
// сразу, а не дождаться таймаута.
func closedWithin(conn net.Conn) bool {
	_ = conn.SetReadDeadline(time.Now().Add(2 * time.Second))
	buf := make([]byte, 1)
	_, err := conn.Read(buf)
	if err == nil {
		return false
	}
	if ne, ok := err.(net.Error); ok && ne.Timeout() {
		return false
	}
	return true
}

// Обычный HTTP-запрос проходит через прокси и доходит до сервера.
func TestPlainRequestIsForwarded(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, "привет из интернета")
	}))
	defer origin.Close()

	s := testServer(t)
	s.SetRules([]string{"youtube.com"})
	s.SetEnabled(true)

	resp, err := proxyClient(t, s).Get(origin.URL + "/page")
	if err != nil {
		t.Fatalf("запрос через прокси не прошёл: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if string(body) != "привет из интернета" {
		t.Fatalf("неожиданный ответ: %q", body)
	}
	if _, blocked := s.Stats(); blocked != 0 {
		t.Fatalf("разрешённый запрос попал в счётчик отсечённых: %d", blocked)
	}
}

// Заблокированный HTTP-сайт получает нашу страницу-заглушку, а не пустую ошибку.
func TestBlockedPlainRequestShowsPage(t *testing.T) {
	s := testServer(t)
	var shownHost string
	s.SetBlockPage(func(w http.ResponseWriter, r *http.Request, host string) {
		shownHost = host
		w.WriteHeader(http.StatusForbidden)
		_, _ = io.WriteString(w, "Ты в фокусе")
	})
	s.SetRules([]string{"vk.com"})
	s.SetEnabled(true)

	resp, err := proxyClient(t, s).Get("http://vk.com/feed")
	if err != nil {
		t.Fatalf("запрос не дошёл до прокси: %v", err)
	}
	defer resp.Body.Close()

	body, _ := io.ReadAll(resp.Body)
	if !strings.Contains(string(body), "Ты в фокусе") {
		t.Fatalf("показана не заглушка: %q", body)
	}
	if shownHost != "vk.com" {
		t.Fatalf("в заглушку передан неверный хост: %q", shownHost)
	}
}

// Порт по умолчанию может быть занят чужой программой: прокси обязан взять
// следующий свободный, а не отказаться работать.
func TestPortFallback(t *testing.T) {
	busy, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось занять порт: %v", err)
	}
	defer busy.Close()

	_, portStr, _ := net.SplitHostPort(busy.Addr().String())
	port := 0
	_, _ = fmt.Sscanf(portStr, "%d", &port)

	s := New(port, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.Start(); err != nil {
		t.Fatalf("прокси не поднялся: %v", err)
	}
	defer s.Stop()

	if s.Addr() == busy.Addr().String() {
		t.Fatal("прокси занял чужой порт")
	}
	if s.Addr() == "" {
		t.Fatal("прокси не сообщил адрес")
	}
}

// Самопроверка прокси — то, что спасает человека от «не открывается вообще
// ничего»: сломанный прокси не должен попадать в политику браузеров.
func TestSelfTestSeesWorkingProxy(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "живой сайт")
	}))
	defer origin.Close()

	s := testServer(t)
	s.SetEnabled(true)
	if err := s.SelfTest(origin.URL, 5*time.Second); err != nil {
		t.Fatalf("работающий прокси не прошёл проверку: %v", err)
	}
}

func TestSelfTestFailsWhenSiteBroken(t *testing.T) {
	// Адресат отвечает ошибкой — это тоже повод не включать слой.
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "нет", http.StatusBadGateway)
	}))
	defer bad.Close()

	s := testServer(t)
	if err := s.SelfTest(bad.URL, 5*time.Second); err == nil {
		t.Fatal("сайт с ошибкой 502 должен считаться провалом проверки")
	}
}

func TestSelfTestFailsOnStoppedProxy(t *testing.T) {
	s := New(0, slog.New(slog.NewTextHandler(io.Discard, nil)))
	if err := s.SelfTest("http://example.com/", time.Second); err == nil {
		t.Fatal("остановленный прокси не должен проходить проверку")
	}
}

// Счётчики здоровья: без них сломанный прокси неотличим от работающего — оба
// выглядят как «запросы идут».
func TestHealthCountsForwardedAndFailed(t *testing.T) {
	origin := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "ок")
	}))
	defer origin.Close()

	s := testServer(t)
	s.SetEnabled(true)
	client := proxyClient(t, s)

	resp, err := client.Get(origin.URL)
	if err != nil {
		t.Fatalf("запрос через прокси: %v", err)
	}
	_ = resp.Body.Close()

	forwarded, failed := s.Health()
	if forwarded != 1 {
		t.Errorf("пропущенных запросов: ожидался 1, получено %d", forwarded)
	}
	if failed != 0 {
		t.Errorf("неудачных попыток быть не должно, получено %d", failed)
	}

	// Адрес, на котором заведомо никто не слушает: порт заняли и сразу
	// освободили. Через закрытый httptest-сервер проверять нельзя — его порт
	// успевает занять кто-то другой, и запрос неожиданно проходит.
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatalf("не удалось занять порт: %v", err)
	}
	deadURL := "http://" + ln.Addr().String() + "/"
	_ = ln.Close()

	// Прокси отвечает 502 на недоступный адресат — это и есть «неудачная
	// попытка», по которой видно, что слой не водит трафик.
	forwardedBefore, failedBefore := s.Health()
	resp, err = client.Get(deadURL)
	if err != nil {
		t.Fatalf("прокси должен был ответить ошибкой, а не порвать соединение: %v", err)
	}
	_ = resp.Body.Close()
	if resp.StatusCode != http.StatusBadGateway {
		t.Errorf("ожидался 502, получен %d", resp.StatusCode)
	}
	if _, failed = s.Health(); failed != failedBefore+1 {
		t.Errorf("неудачных попыток: ожидалось %d, получено %d", failedBefore+1, failed)
	}
	if fwd, _ := s.Health(); fwd != forwardedBefore {
		t.Errorf("неудачная попытка не должна считаться пропущенной: %d → %d", forwardedBefore, fwd)
	}
}

func TestRequestHost(t *testing.T) {
	cases := []struct {
		name   string
		method string
		host   string
		url    string
		want   string
	}{
		{"connect", http.MethodConnect, "YouTube.com:443", "", "youtube.com"},
		{"absolute-url", http.MethodGet, "", "http://WWW.Vk.com/feed", "www.vk.com"},
		{"host-only", http.MethodGet, "vk.com:80", "", "vk.com"},
		{"ipv6", http.MethodConnect, "[::1]:443", "", "::1"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r := &http.Request{Method: c.method, Host: c.host, Header: http.Header{}}
			if c.url != "" {
				u, err := url.Parse(c.url)
				if err != nil {
					t.Fatalf("не разобрать URL: %v", err)
				}
				r.URL = u
			}
			if got := requestHost(r); got != c.want {
				t.Fatalf("ожидалось %q, получено %q", c.want, got)
			}
		})
	}
}
