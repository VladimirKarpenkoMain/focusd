package main

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"focusd/internal/config"
	"focusd/internal/proxy"
)

// Доборы к основным тестам: ветки, до которых не добраться общим ходом.

// Прокси слушает, но проверку на живом запросе не проходит — включается
// запасной слой, и в счётчик отсечённых он ничего не добавит.
func TestEngageProxySelfTestFails(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "нет", http.StatusBadGateway)
	}))
	t.Cleanup(bad.Close)
	saved := proxyTestURL
	proxyTestURL = bad.URL
	t.Cleanup(func() { proxyTestURL = saved })

	var blocked int
	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.BlockSitesInBrowsers = func([]string, []string) error { blocked++; return nil }

	fallback, err := a.engageProxy([]string{"youtube.com"}, nil)
	if err != nil {
		t.Fatalf("engageProxy: %v", err)
	}
	if !fallback || blocked != 1 {
		t.Fatalf("ожидался запасной слой: fallback=%v, вызовов=%d", fallback, blocked)
	}
	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", got)
	}
}

// Политику прокси записать не удалось — тоже запасной слой, и ошибка обязана
// дойти наверх.
func TestEngageProxyPolicyWriteFails(t *testing.T) {
	useLocalProxyTarget(t)

	var blocked int
	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }
	s.BlockSitesInBrowsers = func([]string, []string) error { blocked++; return nil }

	fallback, err := a.engageProxy([]string{"youtube.com"}, nil)
	if err == nil {
		t.Fatal("ожидалась ошибка записи политики")
	}
	if !fallback || blocked != 1 {
		t.Fatalf("ожидался запасной слой: fallback=%v, вызовов=%d", fallback, blocked)
	}
}

// Настройки вернулись, а отметку о подмене снять не удалось: это замечание, а
// не отказ — прокси уже не наш.
func TestRestoreStoredSystemProxyCannotClearMark(t *testing.T) {
	store := testStore(t)
	newSysStub().install(t)
	if err := store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Временный файл занят каталогом — запись не пройдёт.
	if err := breakStoreDir(store.Dir()); err != nil {
		t.Fatal(err)
	}

	restoreStoredSystemProxy(store, quietLogger())

	if store.Get().SystemProxy == nil {
		t.Fatal("отметка снята, хотя запись не прошла")
	}
}

// Прокси потерял сокет и заново не поднимается: только тогда снимаем политику.
func TestProxyWatchdogCannotRestartProxy(t *testing.T) {
	base := freePortRun(t, proxy.PortScanLimit)

	a, s, _, _ := newTestApp(t, func(a *App) { a.prox = proxy.New(base, quietLogger()) })
	if err := a.prox.Start(); err != nil {
		t.Fatalf("прокси не поднялся: %v", err)
	}
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	proxyStalledRequests(a)
	_ = a.prox.Stop()
	// Тот же порт нужен прокси при перезапуске — занимаем его целиком.
	holdPortRun(t, base, proxy.PortScanLimit)
	a.setStalled(1)

	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }

	a.proxyWatchdog()

	if a.prox.Running() {
		t.Fatal("прокси не должен был подняться: порты заняты")
	}
	if cleared != 1 {
		t.Fatalf("политика не снята у сломанного прокси: %d", cleared)
	}
	if a.proxyModeNow() != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", a.proxyModeNow())
	}
}

// Снять политику не удалось: об этом нужно сказать, но уборку не бросать —
// иначе прокси, ведущий в мёртвый порт, останется прописанным.
func TestProxyWatchdogClearPolicyFails(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	proxyStalledRequests(a)
	a.setStalled(1)

	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return errors.New("отказано в доступе") }
	s.SystemProxyAddr = func() string { return proxy.DefaultAddr() }
	var disabled int
	s.DisableSystemProxy = func() error { disabled++; return nil }

	a.proxyWatchdog()

	if cleared != 1 {
		t.Fatalf("попытки снять политику не было: %d", cleared)
	}
	if disabled != 1 {
		t.Fatal("системный прокси остался направленным на мёртвый порт")
	}
	if a.proxyModeNow() != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", a.proxyModeNow())
	}
}

// Конец сессии, а политику прокси оставить не удалось: это замечание в журнале,
// а не повод бросать уборку — запасной слой всё равно снимается.
func TestDisengageReportsKeepPolicyError(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyWatching)
	var unblocked int
	s.BrowserProxyAddr = func() string { return "" }
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }
	s.UnblockSitesInBrowsers = func() error { unblocked++; return nil }

	a.disengage()

	if unblocked != 1 {
		t.Fatalf("запасной слой не снят: %d", unblocked)
	}
}
