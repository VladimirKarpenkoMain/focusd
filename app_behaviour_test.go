package main

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"focusd/internal/config"
	"focusd/internal/focus"
	"focusd/internal/proxy"
	"focusd/internal/winsys"
)

// ---- Оформление ----

func TestEffectiveTheme(t *testing.T) {
	_, s, _, _ := newTestApp(t)

	if got := EffectiveTheme(config.ThemeDark); got != "dark" {
		t.Errorf("тёмная тема: %q", got)
	}
	if got := EffectiveTheme(config.ThemeLight); got != "light" {
		t.Errorf("светлая тема: %q", got)
	}

	// «Как в системе» — единственный случай, когда ответ приходит из Windows.
	s.SystemPrefersDark = func() bool { return false }
	if got := EffectiveTheme(config.ThemeSystem); got != "light" {
		t.Errorf("системная светлая тема: %q", got)
	}
	s.SystemPrefersDark = func() bool { return true }
	if got := EffectiveTheme(config.ThemeSystem); got != "dark" {
		t.Errorf("системная тёмная тема: %q", got)
	}
	// Неизвестное значение из старого файла ведёт себя как «как в системе».
	if got := EffectiveTheme(config.Theme("мусор")); got != "dark" {
		t.Errorf("неизвестная тема: %q", got)
	}
}

func TestWindowColourFollowsTheme(t *testing.T) {
	_, s, _, _ := newTestApp(t)
	s.SystemPrefersDark = func() bool { return true }

	if got := windowColour(config.ThemeDark); got == nil || *got != windowColourDark {
		t.Errorf("тёмная тема дала %+v", got)
	}
	if got := windowColour(config.ThemeLight); got == nil || *got != windowColourLight {
		t.Errorf("светлая тема дала %+v", got)
	}
}

// ---- Запуск и выход ----

func TestStartupEngagesProxy(t *testing.T) {
	useLocalProxyTarget(t)

	var proxied []string
	var guard []bool

	a, s, r, _ := newTestApp(t)
	s.SetBrowserProxy = func(addr string) error { proxied = append(proxied, addr); return nil }
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		return winsys.SystemProxySettings{Enable: true, Server: "10.0.0.1:8080"}, nil
	}
	s.SetProxyGuard = func(_ context.Context, enable bool, _ string) error {
		guard = append(guard, enable)
		return nil
	}

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if len(proxied) == 0 {
		t.Fatal("политика прокси не прописана")
	}
	for _, addr := range proxied {
		if !strings.HasPrefix(addr, "127.0.0.1:") {
			t.Fatalf("в политику пописан чужой адрес: %q", addr)
		}
	}
	if got := a.proxyModeNow(); got != proxyWatching {
		t.Fatalf("режим прокси = %d, ожидался «сторожим»", got)
	}
	if len(guard) != 1 || !guard[0] {
		t.Fatalf("страховка прокси не поставлена: %v", guard)
	}
	backup := a.store.Get().SystemProxy
	if backup == nil || backup.Server != "10.0.0.1:8080" || !backup.Enable {
		t.Fatalf("снимок прежних настроек не сохранён: %+v", backup)
	}
	if len(r.emitted) == 0 {
		t.Fatal("состояние не отправлено в интерфейс")
	}
}

// Прокси, который не поднялся, не должен утаскивать за собой политику: браузеры
// ушли бы в мёртвый порт. Блокировку берёт на себя запасной слой.
func TestStartupWithoutProxy(t *testing.T) {
	occupied := occupyPorts(t, proxy.PortScanLimit)

	var proxied int
	a, s, _, _ := newTestApp(t, func(a *App) { a.prox = proxy.New(occupied, quietLogger()) })
	s.SetBrowserProxy = func(string) error { proxied++; return nil }

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if a.prox.Running() {
		t.Fatal("прокси не должен был подняться")
	}
	if proxied != 0 {
		t.Fatal("политика не должна прописываться неработающему прокси")
	}
	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", got)
	}
}

// Прокси слушает, но не водит трафик: прописывать его браузерам нельзя — ни
// сразу, ни после того, как кончится (точнее, так и не начнётся) сессия.
func TestStartupWithBrokenProxy(t *testing.T) {
	bad := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		http.Error(w, "нет", http.StatusBadGateway)
	}))
	t.Cleanup(bad.Close)
	saved := proxyTestURL
	proxyTestURL = bad.URL
	t.Cleanup(func() { proxyTestURL = saved })

	var proxied int
	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.SetBrowserProxy = func(string) error { proxied++; return nil }

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if proxied != 0 {
		t.Fatalf("политика прописана прокси, который не ходит в сеть: %d раз", proxied)
	}
	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", got)
	}
}

// Политику не удалось записать — режим тоже не включается: считать слой
// работающим было бы неправдой.
func TestStartupWithoutPolicyWrite(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, _ := newTestApp(t)
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", got)
	}
}

// Сессия, пережившая перезапуск, обязана подняться вместе с блокировкой.
func TestStartupResumesSession(t *testing.T) {
	useLocalProxyTarget(t)

	a, _, _, f := newTestApp(t)
	f.sess = softSession()
	f.active = true
	f.view = focus.View{Active: true, Strictness: config.Soft}

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if !a.isEngaged() {
		t.Fatal("сессия не была возобновлена")
	}
	if a.prox.RuleCount() == 0 {
		t.Fatal("правила блокировки не применены")
	}
}

// Ошибка включения блокировки должна быть видна человеку, а не потеряться.
func TestStartupReportsEngageError(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, f := newTestApp(t)
	f.sess = softSession()
	f.active = true
	f.view = focus.View{Active: true, Strictness: config.Soft}
	s.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if a.currentError() == "" {
		t.Fatal("ошибка включения блокировки потерялась")
	}
}

// Компактный режим переживает перезапуск вместе с настройками.
func TestStartupRestoresWidget(t *testing.T) {
	useLocalProxyTarget(t)

	a, _, r, _ := newTestApp(t)
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Widget = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	a.startup(context.Background())
	t.Cleanup(func() { close(a.stopCh) })

	if len(r.sizes) == 0 || r.sizes[len(r.sizes)-1] != [2]int{barWidth, barHeight} {
		t.Fatalf("окно не сжато до виджета: %v", r.sizes)
	}
}

func TestShutdownReturnsSystemToNormal(t *testing.T) {
	useLocalProxyTarget(t)

	var cleared int
	a, s, _, _ := newTestApp(t)
	s.ClearBrowserProxy = func() error { cleared++; return nil }
	s.RestoreSystemProxy = func(winsys.SystemProxySettings) error { return nil }
	s.SystemProxyAddr = func() string { return proxy.DefaultAddr() }

	a.startup(context.Background())
	a.shutdown(context.Background())

	if cleared != 1 {
		t.Fatalf("политика прокси снята %d раз, ожидался 1", cleared)
	}
	if a.prox.Running() {
		t.Fatal("прокси должен быть остановлен при выходе")
	}
}

// Ошибка снятия политики не должна мешать остальной уборке: иначе человек
// останется с прокси, ведущим в мёртвый порт.
func TestShutdownKeepsGoingAfterErrors(t *testing.T) {
	useLocalProxyTarget(t)

	var restored int
	a, s, _, _ := newTestApp(t)
	s.ClearBrowserProxy = func() error { return errors.New("отказано в доступе") }
	s.RestoreSystemProxy = func(winsys.SystemProxySettings) error { restored++; return nil }
	if err := a.store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Enable: true, Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	a.startup(context.Background())
	a.shutdown(context.Background())

	if restored < 1 {
		t.Fatalf("прежние настройки системного прокси не возвращены: %d", restored)
	}
}

// ---- Состояние ----

func TestSnapshot(t *testing.T) {
	a, s, _, f := newTestApp(t, withProxyRunning())
	s.BrowserProxyAddr = func() string { return a.prox.Addr() }
	if err := a.store.Update(func(file *config.File) error {
		file.Settings.DefaultGroups = []string{"social"}
		file.Settings.CustomDomains = []string{"example.com"}
		file.Settings.Allowlist = []string{"music.youtube.com"}
		file.Settings.Theme = config.ThemeDark
		file.Settings.Widget = true
		file.Settings.WidgetOnTop = false
		file.Settings.WidgetShape = config.WidgetRing
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a.mu.Lock()
	a.blocked = 42
	a.lastBlocked = "youtube.com"
	a.notice = "замечание"
	a.hint = "подсказка"
	a.mu.Unlock()
	a.setProxyMode(proxyBlocking)

	st := a.GetState()
	if !st.Elevated || st.BlockedToday != 42 || st.LastBlocked != "youtube.com" {
		t.Fatalf("снимок неверен: %+v", st)
	}
	if len(st.ActiveGroups) != 1 || st.ActiveGroups[0] != "social" {
		t.Fatalf("группы: %v", st.ActiveGroups)
	}
	if !st.BrowserBlock || !st.ProxyBlock {
		t.Fatalf("признаки блокировки: %+v", st)
	}
	if st.Theme != string(config.ThemeDark) || st.ResolvedTheme != "dark" {
		t.Fatalf("оформление: %+v", st)
	}
	if st.WidgetShape != string(config.WidgetRing) || !st.Widget || st.WidgetOnTop {
		t.Fatalf("виджет: %+v", st)
	}
	if st.Version != Version || st.DataDir == "" || st.Notice != "замечание" || st.Hint != "подсказка" {
		t.Fatalf("прочие поля: %+v", st)
	}
	if st.TotalRules == 0 {
		t.Fatal("число правил не посчитано")
	}

	// Активная сессия диктует свои списки и своё число правил.
	f.active, f.sess = true, softSession()
	f.view = focus.View{Active: true, Strictness: config.Soft, Remaining: 600}
	st = a.GetState()
	if !st.Session.Active || st.Session.Remaining != 600 {
		t.Fatalf("сессия не попала в снимок: %+v", st.Session)
	}
	if len(st.ActiveGroups) != 1 || st.ActiveGroups[0] != "social" {
		t.Fatalf("списки сессии не подхвачены: %v", st.ActiveGroups)
	}
}

// Запасной слой — тоже блокировка: без прокси признак обязан сработать.
func TestSnapshotSeesBrowserPolicy(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.SitesBlockedInBrowsers = func() bool { return true }

	if !a.GetState().BrowserBlock {
		t.Fatal("политика браузеров не считается блокировкой")
	}
}

func TestBlockInfo(t *testing.T) {
	a, _, _, f := newTestApp(t)
	a.mu.Lock()
	a.blocked = 7
	a.mu.Unlock()

	info := a.blockInfo("youtube.com")
	if info.Active || info.Host != "youtube.com" || info.Blocked != 7 {
		t.Fatalf("без сессии: %+v", info)
	}

	f.active = true
	f.view = focus.View{Active: true, Remaining: 120, Strictness: config.Lock}
	info = a.blockInfo("youtube.com")
	if !info.Active || info.Remaining != 120 || info.Strictness != "lock" {
		t.Fatalf("с сессией: %+v", info)
	}
}

func TestRenderBlockPage(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodGet, "http://youtube.com/", nil)

	a.renderBlockPage(rec, req, "youtube.com")

	if rec.Code != http.StatusForbidden {
		t.Fatalf("код ответа %d", rec.Code)
	}
	if got := rec.Header().Get("Content-Type"); !strings.HasPrefix(got, "text/html") {
		t.Fatalf("тип содержимого %q", got)
	}
	if got := rec.Header().Get("Cache-Control"); got != "no-store" {
		t.Fatalf("кэширование не запрещено: %q", got)
	}
	if !strings.Contains(rec.Body.String(), "youtube.com") {
		t.Fatal("хост не попал на страницу-заглушку")
	}
}

func TestEmitWithoutWindow(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	a.ctx = nil
	a.emit()
	if len(r.emitted) != 0 {
		t.Fatal("без окна события отправлять некуда")
	}
}

func TestErrorsAndNotices(t *testing.T) {
	a, _, _, _ := newTestApp(t)

	a.setError(nil)
	if a.currentError() != "" {
		t.Fatal("nil-ошибка не должна ничего записывать")
	}
	a.setError(errors.New("поломка"))
	if a.currentError() != "поломка" {
		t.Fatalf("ошибка не записалась: %q", a.currentError())
	}
	a.clearError()
	if a.currentError() != "" {
		t.Fatalf("ошибка не сброшена: %q", a.currentError())
	}

	a.setNotice("")
	if a.currentNotice() != "" {
		t.Fatal("пустое замечание записывать некуда")
	}
	a.setNotice("слой переключён")
	if a.currentNotice() != "слой переключён" {
		t.Fatalf("замечание не записано: %q", a.currentNotice())
	}
}

// ---- Счётчики ----

func TestCounterDelta(t *testing.T) {
	var prev int64
	if got := counterDelta(5, &prev); got != 5 {
		t.Fatalf("первый прирост = %d", got)
	}
	if got := counterDelta(5, &prev); got != 0 {
		t.Fatalf("без изменений прирост = %d", got)
	}
	if got := counterDelta(9, &prev); got != 4 {
		t.Fatalf("прирост = %d, ожидалось 4", got)
	}
	// Счётчик слоя сбросился: прибавлять чужое число нельзя.
	if got := counterDelta(1, &prev); got != 0 {
		t.Fatalf("после сброса прирост = %d, ожидался 0", got)
	}
	if prev != 1 {
		t.Fatalf("точка отсчёта не обновлена: %d", prev)
	}
}

func TestSyncCountersUsesProxy(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	a.prox.SetEnabled(true)
	a.prox.SetRules([]string{"youtube.com"})

	// Прокси — единственный честный источник счётчика: он стоит на пути
	// трафика. Без отсечённых запросов суточный счётчик расти не должен.
	a.syncCounters()
	if got := a.currentBlocked(); got != 0 {
		t.Fatalf("без отсечённых запросов счётчик вырос: %d", got)
	}
}

func TestSyncCountersPicksUpLastBlocked(t *testing.T) {
	a, _, _, _ := newTestApp(t)

	// Прокси отсечёт запрос к заблокированному имени и запомнит его.
	a.prox.SetRules([]string{"vk.com"})
	a.prox.SetEnabled(true)
	a.prox.SetBlockPage(func(w http.ResponseWriter, _ *http.Request, _ string) {
		w.WriteHeader(http.StatusForbidden)
	})
	a.prox.ServeHTTP(httptest.NewRecorder(), httptest.NewRequest(http.MethodGet, "http://vk.com/feed", nil))

	a.syncCounters()
	if got := a.currentBlocked(); got != 1 {
		t.Fatalf("суточный счётчик = %d, ожидался 1", got)
	}
	if got := a.currentLastBlocked(); got != "vk.com" {
		t.Fatalf("последний отсечённый сайт = %q", got)
	}

	// Повторный вызов не должен прибавлять то же самое дважды.
	a.syncCounters()
	if got := a.currentBlocked(); got != 1 {
		t.Fatalf("счётчик задвоился: %d", got)
	}
}

func TestRollDayResetsCounter(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	a.setBlocked(10, "2020-01-01")

	a.rollDay(time.Date(2020, 1, 1, 23, 59, 0, 0, time.UTC))
	if got := a.currentBlocked(); got != 10 {
		t.Fatalf("в тот же день счётчик сброшен: %d", got)
	}

	a.rollDay(time.Date(2020, 1, 2, 0, 0, 1, 0, time.UTC))
	if got := a.currentBlocked(); got != 0 {
		t.Fatalf("на новый день счётчик не сброшен: %d", got)
	}
	if day := a.currentDay(); day != "2020-01-02" {
		t.Fatalf("день не обновлён: %q", day)
	}
}

func TestPersistCounters(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	a.setBlocked(15, "2024-05-05")

	a.persistCounters()

	f := a.store.Get()
	if f.BlockedToday != 15 || f.BlockedDate != "2024-05-05" {
		t.Fatalf("счётчики не сохранены: %+v", f)
	}
}

// ---- Сессии ----

func TestStartSessionRequiresRights(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.IsElevated = func() bool { return false }

	err := a.StartSession(StartOptions{DurationMin: 25})
	if err == nil || !strings.Contains(err.Error(), "права администратора") {
		t.Fatalf("ожидалась жалоба на права, получено %v", err)
	}
}

func TestStartSession(t *testing.T) {
	useLocalProxyTarget(t)

	a, _, r, f := newTestApp(t)
	f.sess, f.active = softSession(), true
	f.view = focus.View{Active: true, Remaining: 1500, Strictness: config.Soft}

	if err := a.StartSession(StartOptions{
		DurationMin:   25,
		Strictness:    string(config.Soft),
		Groups:        []string{"social"},
		CustomDomains: []string{"example.com"},
		Allowlist:     []string{"music.youtube.com"},
		Label:         "Работа",
	}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if len(f.started) != 1 || f.started[0].Label != "Работа" || f.started[0].DurationMin != 25 {
		t.Fatalf("параметры сессии не дошли: %+v", f.started)
	}
	if !a.isEngaged() {
		t.Fatal("блокировка не включена")
	}
	if len(r.emitted) == 0 {
		t.Fatal("состояние не отправлено в интерфейс")
	}
	if a.prox.RuleCount() == 0 {
		t.Fatal("правила не применены к прокси")
	}
}

func TestStartSessionBadDuration(t *testing.T) {
	a, _, _, f := newTestApp(t)
	f.startErr = focus.ErrBadDuration

	if err := a.StartSession(StartOptions{DurationMin: 0}); err == nil {
		t.Fatal("ожидалась ошибка длительности")
	}
}

// Сессия не сохранилась — молча продолжать нельзя: человек решил бы, что фокус
// начался.
func TestStartSessionNotSaved(t *testing.T) {
	a, _, _, _ := newTestApp(t)

	err := a.StartSession(StartOptions{DurationMin: 25, Strictness: string(config.Soft)})
	if err == nil || !strings.Contains(err.Error(), "не сохранилась") {
		t.Fatalf("ожидалась ошибка сохранения, получено %v", err)
	}
}

func TestStartSessionReportsEngageError(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, f := newTestApp(t, withProxyRunning())
	f.sess, f.active = softSession(), true
	s.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }

	if err := a.StartSession(StartOptions{DurationMin: 25, Strictness: string(config.Soft)}); err != nil {
		t.Fatalf("StartSession: %v", err)
	}
	if a.currentError() == "" {
		t.Fatal("ошибка включения блокировки не показана")
	}
}

func TestRequestCancel(t *testing.T) {
	a, _, _, f := newTestApp(t)

	f.cancelWait, f.cancelErr = 45, nil
	info, err := a.RequestCancel()
	if err != nil {
		t.Fatalf("RequestCancel: %v", err)
	}
	if info.WaitSec != 45 || !strings.Contains(info.Message, "45") {
		t.Fatalf("сообщение об ожидании: %+v", info)
	}

	f.cancelWait = 0
	info, err = a.RequestCancel()
	if err != nil || info.WaitSec != 0 || info.Message == "" {
		t.Fatalf("немедленная отмена: %+v (%v)", info, err)
	}

	f.cancelErr = focus.ErrNotActive
	if _, err := a.RequestCancel(); err == nil {
		t.Fatal("ожидалась ошибка отсутствия сессии")
	}
}

func TestConfirmCancel(t *testing.T) {
	useLocalProxyTarget(t)

	a, _, r, f := newTestApp(t)
	f.confirmErr = focus.ErrTooEarly
	if err := a.ConfirmCancel(); err == nil {
		t.Fatal("ожидалась ошибка отмены")
	}

	f.confirmErr = nil
	if err := a.ConfirmCancel(); err != nil {
		t.Fatalf("ConfirmCancel: %v", err)
	}
	if a.isEngaged() {
		t.Fatal("после отмены блокировка должна быть снята")
	}
	if len(r.emitted) == 0 {
		t.Fatal("состояние не отправлено в интерфейс")
	}
}

func TestEmergencyRestore(t *testing.T) {
	useLocalProxyTarget(t)

	a, _, r, f := newTestApp(t)
	f.forceErr = errors.New("хранилище недоступно")

	if err := a.EmergencyRestore(); err != nil {
		t.Fatalf("EmergencyRestore: %v", err)
	}
	if a.currentError() == "" {
		t.Fatal("ошибка аварийного снятия не показана")
	}
	if len(r.emitted) == 0 {
		t.Fatal("состояние не отправлено в интерфейс")
	}
}

// ---- Блокировка ----

func TestEngageFallsBackToBrowserPolicies(t *testing.T) {
	useLocalProxyTarget(t)

	var blocked []string
	a, s, _, _ := newTestApp(t)
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }
	s.BlockSitesInBrowsers = func(domains, _ []string) error {
		blocked = append(blocked, domains...)
		return nil
	}
	s.RunningBrowsers = func(context.Context) []string { return []string{"Chrome", "Edge"} }

	if err := a.engage(softSession()); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if len(blocked) == 0 {
		t.Fatal("запасной слой не включён")
	}
	hint := a.currentHint()
	if !strings.Contains(hint, "Chrome") || !strings.Contains(hint, "Edge") {
		t.Fatalf("подсказка про перезапуск браузеров: %q", hint)
	}
	if !a.isEngaged() {
		t.Fatal("сессия не помечена как идущая")
	}
}

func TestEngageWithoutHintWhenNoBrowsers(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, _ := newTestApp(t)
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }
	s.RunningBrowsers = func(context.Context) []string { return nil }

	if err := a.engage(softSession()); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if hint := a.currentHint(); hint != "" {
		t.Fatalf("подсказка про перезапуск не нужна без открытых браузеров: %q", hint)
	}
}

func TestEngageKeepsProxyAndHidesBrowserPolicy(t *testing.T) {
	useLocalProxyTarget(t)

	var unblocked int
	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.UnblockSitesInBrowsers = func() error { unblocked++; return nil }

	if err := a.engage(softSession()); err != nil {
		t.Fatalf("engage: %v", err)
	}
	if unblocked != 1 {
		t.Fatal("списки URLBlocklist не сняты: с ними прокси не увидел бы запросы")
	}
	if got := a.proxyModeNow(); got != proxyBlocking {
		t.Fatalf("режим прокси = %d, ожидался «режем»", got)
	}
}

func TestEngageReportsBrowserPolicyError(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }

	if err := a.engage(softSession()); err == nil {
		t.Fatal("ожидалась ошибка снятия политик браузеров")
	}
}

func TestEngageProxyWithoutAddr(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var blocked int
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

func TestEngageProxyReportsFallbackError(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.BlockSitesInBrowsers = func([]string, []string) error { return errors.New("реестр недоступен") }

	fallback, err := a.engageProxy([]string{"youtube.com"}, nil)
	if err == nil || !fallback {
		t.Fatalf("ожидалась ошибка запасного слоя: fallback=%v, err=%v", fallback, err)
	}
}

func TestFallbackBrowserBlock(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	if err := a.fallbackBrowserBlock([]string{"youtube.com"}, nil); err != nil {
		t.Fatalf("fallbackBrowserBlock: %v", err)
	}

	s.BlockSitesInBrowsers = func([]string, []string) error { return errors.New("нет прав") }
	err := a.fallbackBrowserBlock([]string{"youtube.com"}, nil)
	if err == nil || !strings.Contains(err.Error(), "блокировка сайтов в браузерах") {
		t.Fatalf("ошибка запасного слоя потеряла причину: %v", err)
	}
}

// ---- Политика прокси после конца сессии ----

func TestKeepProxyPolicyReappliesWhenLost(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	var written []string
	s.BrowserProxyAddr = func() string { return "" }
	s.SetBrowserProxy = func(addr string) error { written = append(written, addr); return nil }

	if err := a.keepProxyPolicy(); err != nil {
		t.Fatalf("keepProxyPolicy: %v", err)
	}
	if len(written) != 1 {
		t.Fatalf("политика не восстановлена: %v", written)
	}
	if got := a.proxyModeNow(); got != proxyWatching {
		t.Fatalf("режим прокси = %d", got)
	}
}

func TestKeepProxyPolicyKeepsExisting(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	var written int
	s.BrowserProxyAddr = func() string { return a.prox.Addr() }
	s.SetBrowserProxy = func(string) error { written++; return nil }

	if err := a.keepProxyPolicy(); err != nil {
		t.Fatalf("keepProxyPolicy: %v", err)
	}
	if written != 0 {
		t.Fatal("политику не нужно переписывать: браузеру нечего перечитывать")
	}
}

func TestKeepProxyPolicyReportsWriteError(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.BrowserProxyAddr = func() string { return "" }
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }

	if err := a.keepProxyPolicy(); err == nil {
		t.Fatal("ожидалась ошибка записи политики")
	}
	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d, ожидался выключенный", got)
	}
}

// Прокси не слушает — политике на него браузеры не место.
func TestKeepProxyPolicyWithoutProxy(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }
	s.SystemProxyAddr = func() string { return "127.0.0.1:1" }

	if err := a.keepProxyPolicy(); err != nil {
		t.Fatalf("keepProxyPolicy: %v", err)
	}
	if cleared != 1 {
		t.Fatal("политика прокси не снята у неработающего прокси")
	}
	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d", got)
	}
}

func TestKeepProxyPolicyReportsClearError(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.ClearBrowserProxy = func() error { return errors.New("отказано в доступе") }

	if err := a.keepProxyPolicy(); err == nil {
		t.Fatal("ожидалась ошибка снятия политики")
	}
}

func TestKeepSystemProxyRestoresAfterTampering(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyWatching)

	var installed int
	s.SystemProxyAddr = func() string { return "10.0.0.1:8080" }
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		installed++
		return winsys.SystemProxySettings{}, nil
	}
	a.keepSystemProxy()
	if installed != 1 {
		t.Fatalf("системный прокси не возвращён на Focusd: %d", installed)
	}

	// Уже наш — писать нечего.
	installed = 0
	s.SystemProxyAddr = func() string { return a.prox.Addr() }
	a.keepSystemProxy()
	if installed != 0 {
		t.Fatal("лишняя запись в реестр")
	}
}

func TestKeepSystemProxySkippedWhenOffOrStopped(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var installed int
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		installed++
		return winsys.SystemProxySettings{}, nil
	}

	// Слой выключен.
	a.setProxyMode(proxyOff)
	a.keepSystemProxy()

	// Прокси не слушает.
	a.setProxyMode(proxyWatching)
	a.keepSystemProxy()

	if installed != 0 {
		t.Fatalf("системный прокси тронут зря: %d", installed)
	}
}

func TestSetProxyModeIsAtomic(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	a.setProxyMode(proxyBlocking)
	if got := a.proxyModeNow(); got != proxyBlocking {
		t.Fatalf("режим = %d", got)
	}
}

// ---- Системный прокси ----

func TestInstallSystemProxyWithoutWindowOrAddr(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var installed int
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		installed++
		return winsys.SystemProxySettings{}, nil
	}

	a.ctx = nil
	a.installSystemProxy()

	a.ctx = context.Background()
	a.installSystemProxy()

	if installed != 0 {
		t.Fatalf("подмена состоялась без окна или адреса: %d", installed)
	}
}

func TestInstallSystemProxyReportsError(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		return winsys.SystemProxySettings{}, errors.New("отказано в доступе")
	}

	a.installSystemProxy()
	if a.store.Get().SystemProxy != nil {
		t.Fatal("снимок настроек сохранён при неудачной подмене")
	}
}

// Снимок делается один раз: со второго раза мы сохранили бы свои же настройки
// и потеряли настройки человека.
func TestInstallSystemProxyKeepsFirstBackup(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	if err := a.store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Enable: true, Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		return winsys.SystemProxySettings{Server: "127.0.0.1:1"}, nil
	}

	a.installSystemProxy()

	backup := a.store.Get().SystemProxy
	if backup == nil || backup.Server != "10.0.0.1:8080" {
		t.Fatalf("прежний снимок затёрт: %+v", backup)
	}
}

// Снимок не сохранился — подмену нужно немедленно отменить: вернуть настройки
// будет нечем.
func TestInstallSystemProxyUndoesWhenBackupFails(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	prev := winsys.SystemProxySettings{Enable: true, Server: "10.0.0.1:8080"}
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) { return prev, nil }

	var restored []winsys.SystemProxySettings
	s.RestoreSystemProxy = func(p winsys.SystemProxySettings) error {
		restored = append(restored, p)
		return nil
	}

	// Хранилище отказывает на записи: временный файл занят каталогом.
	if err := breakStoreDir(a.store.Dir()); err != nil {
		t.Fatal(err)
	}
	a.installSystemProxy()

	if len(restored) != 1 || restored[0] != prev {
		t.Fatalf("подмена не отменена: %v", restored)
	}
}

func TestDropSystemProxy(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var restored int
	var guard []bool
	s.RestoreSystemProxy = func(winsys.SystemProxySettings) error { restored++; return nil }
	s.SetProxyGuard = func(_ context.Context, enable bool, _ string) error {
		guard = append(guard, enable)
		return nil
	}
	if err := a.store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	// Страховка сначала поставлена — иначе снимать нечего и schtasks зря не
	// дёргается.
	a.keepProxyGuard(true)
	guard = nil

	a.dropSystemProxy()

	if restored != 1 {
		t.Fatalf("настройки не возвращены: %d", restored)
	}
	if len(guard) != 1 || guard[0] {
		t.Fatalf("страховка не снята: %v", guard)
	}
	if a.store.Get().SystemProxy != nil {
		t.Fatal("отметка о подмене осталась")
	}
}

func TestKeepProxyGuardOnlyOnChanges(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var calls []bool
	s.SetProxyGuard = func(_ context.Context, enable bool, _ string) error {
		calls = append(calls, enable)
		return nil
	}

	a.keepProxyGuard(true)
	a.keepProxyGuard(true)
	a.keepProxyGuard(false)
	a.keepProxyGuard(false)

	if len(calls) != 2 || !calls[0] || calls[1] {
		t.Fatalf("schtasks дёргается зря: %v", calls)
	}
}

func TestKeepProxyGuardWithoutExePath(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.ExecutablePath = func() (string, error) { return "", errors.New("путь неизвестен") }
	var calls int
	s.SetProxyGuard = func(context.Context, bool, string) error { calls++; return nil }

	a.keepProxyGuard(true)
	if calls != 0 {
		t.Fatal("задача поставлена без пути к приложению")
	}
}

func TestKeepProxyGuardReportsError(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.SetProxyGuard = func(context.Context, bool, string) error { return errors.New("schtasks отказал") }
	a.keepProxyGuard(true)
	// Состояние запоминается даже при отказе: иначе задачи ставились бы на
	// каждой итерации петли.
	if !a.proxyGuardState() {
		t.Fatal("состояние страховки не запомнено")
	}
}

// ---- Возврат настроек прокси без снимка ----

func TestRestoreStoredSystemProxyWithoutBackup(t *testing.T) {
	store := testStore(t)
	s := newSysStub().install(t)
	log := quietLogger()

	var disabled int
	s.SystemProxyAddr = func() string { return proxy.DefaultAddr() }
	s.DisableSystemProxy = func() error { disabled++; return nil }
	restoreStoredSystemProxy(store, log)
	if disabled != 1 {
		t.Fatalf("прокси на мёртвый порт не выключен: %d", disabled)
	}

	// Прокси выключен или указывает не на нас — трогать нечего.
	disabled = 0
	s.SystemProxyAddr = func() string { return "" }
	restoreStoredSystemProxy(store, log)
	if disabled != 0 {
		t.Fatal("чужие настройки тронуты")
	}
}

func TestRestoreStoredSystemProxyDisableError(t *testing.T) {
	store := testStore(t)
	s := newSysStub().install(t)
	s.SystemProxyAddr = func() string { return proxy.DefaultAddr() }
	s.DisableSystemProxy = func() error { return errors.New("отказано в доступе") }

	restoreStoredSystemProxy(store, quietLogger())
}

func TestRestoreStoredSystemProxy(t *testing.T) {
	store := testStore(t)
	s := newSysStub().install(t)
	if err := store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{
			Enable: true, Server: "10.0.0.1:8080", AutoConfigURL: "http://wpad/wpad.dat",
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var got []winsys.SystemProxySettings
	s.RestoreSystemProxy = func(p winsys.SystemProxySettings) error {
		got = append(got, p)
		return nil
	}

	restoreStoredSystemProxy(store, quietLogger())

	if len(got) != 1 || got[0].Server != "10.0.0.1:8080" || got[0].AutoConfigURL != "http://wpad/wpad.dat" {
		t.Fatalf("настройки возвращены неверно: %+v", got)
	}
	if store.Get().SystemProxy != nil {
		t.Fatal("отметка о подмене осталась")
	}
}

func TestRestoreStoredSystemProxyReportsRestoreError(t *testing.T) {
	store := testStore(t)
	s := newSysStub().install(t)
	if err := store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	s.RestoreSystemProxy = func(winsys.SystemProxySettings) error { return errors.New("отказано в доступе") }

	restoreStoredSystemProxy(store, quietLogger())
	if store.Get().SystemProxy == nil {
		t.Fatal("отметка снята, хотя настройки не вернулись")
	}
}

// ---- Наблюдение за прокси ----

// proxyStalledRequests прогоняет через прокси несколько неудачных запросов:
// так поломка выглядит со стороны — браузер стучится, а forwarded не растёт.
func proxyStalledRequests(a *App) {
	a.prox.SetEnabled(true)
	a.prox.SetRules([]string{"youtube.com"})
	for i := 0; i < 3; i++ {
		a.prox.ServeHTTP(httptest.NewRecorder(),
			httptest.NewRequest(http.MethodGet, "http://127.0.0.1:1/", nil))
	}
}

func TestProxyWatchdogIgnoresHealthyProxy(t *testing.T) {
	a, _, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyWatching)
	a.setHealthBaseline(0, 0)

	// Ни одного сбоя — сторож молчит.
	a.proxyWatchdog()
	if got := a.currentStalled(); got != 0 {
		t.Fatalf("преждевременная тревога: %d", got)
	}
}

func TestProxyWatchdogSkipsWhenOff(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }

	a.setProxyMode(proxyOff)
	a.proxyWatchdog()
	if cleared != 0 {
		t.Fatal("выключенный слой трогать нечего")
	}
}

// Одна неудачная попытка — ещё не поломка: снимать политику на каждой
// случайности нельзя, браузер заметит это не сразу.
func TestProxyWatchdogNeedsTwoRounds(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	proxyStalledRequests(a)

	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }

	a.proxyWatchdog()
	if cleared != 0 {
		t.Fatal("политика снята после первого же наблюдения")
	}
	if got := a.currentStalled(); got != 1 {
		t.Fatalf("первое наблюдение не записано: %d", got)
	}
}

func TestProxyWatchdogSwitchesToFallback(t *testing.T) {
	a, s, _, f := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	a.setStalled(1) // первый раунд уже прошёл
	proxyStalledRequests(a)

	var cleared, blocked int
	s.ClearBrowserProxy = func() error { cleared++; return nil }
	s.BlockSitesInBrowsers = func([]string, []string) error { blocked++; return nil }
	f.active, f.sess = true, softSession()

	a.proxyWatchdog()

	if cleared != 1 {
		t.Fatalf("политика прокси не снята: %d", cleared)
	}
	if blocked != 1 {
		t.Fatalf("запасной слой не включён: %d", blocked)
	}
	if got := a.proxyModeNow(); got != proxyOff {
		t.Fatalf("режим прокси = %d", got)
	}
	if a.currentNotice() == "" {
		t.Fatal("человеку не сказали, почему счётчик стал неточным")
	}
}

func TestProxyWatchdogFallbackError(t *testing.T) {
	a, s, _, f := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	a.setStalled(1)
	proxyStalledRequests(a)

	f.active, f.sess = true, softSession()
	s.BlockSitesInBrowsers = func([]string, []string) error { return errors.New("нет прав") }

	a.proxyWatchdog()
	if a.currentError() == "" {
		t.Fatal("ошибка запасного слоя не показана")
	}
}

// Если прокси потерял сокет, достаточно поднять его заново: снимать политику
// в этот момент — значит отобрать доступ у всех, кто сейчас работает.
func TestProxyWatchdogRestartsLostProxy(t *testing.T) {
	// Прокси работал, потом потерял сокет: счётчик сбоев уже накоплен, а
	// forwarded стоит на месте.
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	proxyStalledRequests(a)
	if err := a.prox.Stop(); err != nil {
		t.Fatalf("не удалось остановить прокси: %v", err)
	}
	a.setStalled(1)
	s.SystemProxyAddr = func() string { return "" }

	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }

	a.proxyWatchdog()

	if !a.prox.Running() {
		t.Fatal("прокси не поднят заново")
	}
	if cleared != 0 {
		t.Fatal("политику сняли, хотя прокси удалось починить")
	}
	if got := a.currentStalled(); got != 0 {
		t.Fatalf("счётчик наблюдений не сброшен: %d", got)
	}
}

// Прокси жив, но не ходит: починить его нечем, значит снимаем политику.
func TestProxyWatchdogRunsWhenProxyAlreadyRunning(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyBlocking)
	a.setHealthBaseline(0, 0)
	a.setStalled(1)
	proxyStalledRequests(a)

	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }

	a.proxyWatchdog()
	if cleared != 1 {
		t.Fatalf("политика не снята у сломанного прокси: %d", cleared)
	}
}

// ---- Сторожевая задача ----

func TestSyncGuardTask(t *testing.T) {
	a, s, _, f := newTestApp(t)
	var calls []bool
	s.SetGuard = func(_ context.Context, enable bool, _ string) error {
		calls = append(calls, enable)
		return nil
	}

	// Без прав задачи планировщика недоступны — молчим.
	s.IsElevated = func() bool { return false }
	a.syncGuardTask()
	if len(calls) != 0 {
		t.Fatal("задача тронута без прав")
	}

	s.IsElevated = func() bool { return true }
	s.ExecutablePath = func() (string, error) { return "", errors.New("путь неизвестен") }
	a.syncGuardTask()
	if len(calls) != 0 {
		t.Fatal("задача поставлена без пути к приложению")
	}

	s.ExecutablePath = func() (string, error) { return `C:\Focusd.exe`, nil }
	a.syncGuardTask()
	if len(calls) != 1 || calls[0] {
		t.Fatalf("сторож поставлен без жёсткой сессии: %v", calls)
	}

	f.active = true
	f.sess = &config.Session{Strictness: config.Lock, EndsAt: time.Now().Add(time.Hour).Unix()}
	a.syncGuardTask()
	if len(calls) != 2 || !calls[1] {
		t.Fatalf("жёсткая сессия не охраняется: %v", calls)
	}
}

func TestSyncGuardTaskReportsError(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	s.SetGuard = func(context.Context, bool, string) error { return errors.New("schtasks отказал") }
	a.syncGuardTask()
}

// ---- Набор правил ----

func TestBuildRuleset(t *testing.T) {
	soft, _ := buildRuleset([]string{"social"}, []string{"example.com"}, config.Soft)
	strict, _ := buildRuleset([]string{"social"}, []string{"example.com"}, config.Strict)

	if len(soft) >= len(strict) {
		t.Fatalf("в строгом режиме доменов должно быть больше: %d и %d", len(soft), len(strict))
	}
	if !contains(strict, "dns.google") {
		t.Fatal("в строгом режиме DoH-резолверы не закрыты")
	}
	if contains(soft, "dns.google") {
		t.Fatal("в мягком режиме DoH закрывать не нужно")
	}
	if !contains(soft, "example.com") {
		t.Fatal("свои домены потерялись")
	}
}

// ---- Уборка ----

func TestDisengage(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, _ := newTestApp(t, withProxyRunning())
	var unblocked int
	s.UnblockSitesInBrowsers = func() error { unblocked++; return nil }
	s.BrowserProxyAddr = func() string { return a.prox.Addr() }
	a.setEngaged(true)
	a.setProxyMode(proxyBlocking)
	a.prox.SetEnabled(true)

	a.disengage()

	if a.isEngaged() {
		t.Fatal("сессия не помечена завершённой")
	}
	if a.currentHint() != "" {
		t.Fatal("подсказка не сброшена")
	}
	if unblocked == 0 {
		t.Fatal("запасной слой не снят")
	}
	if a.proxyModeNow() != proxyWatching {
		t.Fatalf("режим прокси = %d, ожидался «сторожим»", a.proxyModeNow())
	}
	// Политика прокси остаётся на месте: браузеру нечего перечитывать.
	if s.BrowserProxyAddr() == "" {
		t.Fatal("политика прокси снята вместе с сессией")
	}
}

func TestDisengageReportsErrors(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, _ := newTestApp(t, withProxyRunning())
	s.BrowserProxyAddr = func() string { return "" }
	s.SetBrowserProxy = func(string) error { return errors.New("отказано в доступе") }
	s.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }

	a.disengage()
	if a.proxyModeNow() != proxyOff {
		t.Fatalf("режим прокси = %d", a.proxyModeNow())
	}
}

// Слой прокси, который так и не заработал, при уборке не трогают: политику ему
// прописывать нечего — браузеры ушли бы в прокси, не видящий сети.
func TestDisengageWithoutProxy(t *testing.T) {
	a, s, _, _ := newTestApp(t)
	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }

	a.disengage()
	if cleared != 0 {
		t.Fatalf("неработающий слой прокси тронут зря: %d", cleared)
	}
}

// А вот работавший слой прокси уборка не выключает: политика остаётся, чтобы
// браузеру нечего было перечитывать.
func TestDisengageKeepsWorkingProxyPolicy(t *testing.T) {
	a, s, _, _ := newTestApp(t, withProxyRunning())
	a.setProxyMode(proxyWatching)
	var cleared int
	s.ClearBrowserProxy = func() error { cleared++; return nil }
	s.BrowserProxyAddr = func() string { return a.prox.Addr() }

	a.disengage()
	if cleared != 0 {
		t.Fatalf("политика работающего прокси снята: %d", cleared)
	}
	if a.proxyModeNow() != proxyWatching {
		t.Fatalf("режим прокси = %d, ожидался «сторожим»", a.proxyModeNow())
	}
}

// ---- Каталог ----

func TestGetCatalog(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	got := a.GetCatalog()
	if len(got) == 0 {
		t.Fatal("каталог пуст")
	}
	got[0].Domains[0] = "испорчено.example"
	if a.GetCatalog()[0].Domains[0] == "испорчено.example" {
		t.Fatal("GetCatalog отдаёт внутренние данные каталога")
	}
}

// ---- Вспомогательные функции ----

func TestOrEmptyCleanAndMerge(t *testing.T) {
	if got := orEmpty(nil); got == nil || len(got) != 0 {
		t.Fatalf("orEmpty(nil) = %v", got)
	}
	if got := orEmpty([]string{"a.com"}); len(got) != 1 {
		t.Fatalf("orEmpty = %v", got)
	}

	clean := cleanDomains([]string{" YouTube.com ", "youtube.com", "мусор", "", "https://vk.com/feed"})
	if len(clean) != 2 || clean[0] != "youtube.com" || clean[1] != "vk.com" {
		t.Fatalf("cleanDomains = %v", clean)
	}

	merged := mergeUnique([]string{"a.com", ""}, []string{"a.com", "b.com"})
	if len(merged) != 2 || merged[0] != "a.com" || merged[1] != "b.com" {
		t.Fatalf("mergeUnique = %v", merged)
	}

	removed := removeString([]string{"a.com", "b.com", "a.com"}, "a.com")
	if len(removed) != 1 || removed[0] != "b.com" {
		t.Fatalf("removeString = %v", removed)
	}
}

func TestWidgetGeometry(t *testing.T) {
	if w, h := widgetGeometry(config.WidgetRing); w != ringWidth || h != ringHeight {
		t.Fatalf("круг: %d×%d", w, h)
	}
	if w, h := widgetGeometry(config.WidgetBar); w != barWidth || h != barHeight {
		t.Fatalf("плашка: %d×%d", w, h)
	}
	if w, h := widgetGeometry("мусор"); w != barWidth || h != barHeight {
		t.Fatalf("неизвестная форма: %d×%d", w, h)
	}
}

func TestNewAppPicksUpCounters(t *testing.T) {
	store := testStore(t)
	if err := store.Update(func(f *config.File) error {
		f.BlockedToday = 12
		f.BlockedDate = "2024-01-01"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	a := newApp(store, quietLogger())
	t.Cleanup(func() { _ = a.prox.Stop() })

	if got := a.currentBlocked(); got != 12 {
		t.Fatalf("суточный счётчик = %d", got)
	}
	if day := a.currentDay(); day != "2024-01-01" {
		t.Fatalf("день счётчика = %q", day)
	}
	if a.stopCh == nil || a.prox == nil || a.focus == nil {
		t.Fatal("приложение собрано не полностью")
	}
}

// ---- Мелочи ----

func contains(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}

// freePortRun ищет начало отрезка из n свободных портов подряд.
func freePortRun(t *testing.T, n int) int {
	t.Helper()
	for attempt := 0; attempt < 20; attempt++ {
		probe, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		_, portStr, err := net.SplitHostPort(probe.Addr().String())
		if err != nil {
			t.Fatal(err)
		}
		base, err := strconv.Atoi(portStr)
		if err != nil {
			t.Fatal(err)
		}
		_ = probe.Close()

		listeners, ok := tryHold(base, n)
		for _, l := range listeners {
			_ = l.Close()
		}
		if ok {
			return base
		}
	}
	t.Skip("не удалось найти свободные порты подряд")
	return 0
}

// holdPortRun занимает отрезок портов: другого способа отказать прокси в порту
// из другого пакета нет.
func holdPortRun(t *testing.T, base, n int) {
	t.Helper()
	listeners, ok := tryHold(base, n)
	if !ok {
		t.Skipf("порты %d..%d заняты кем-то ещё", base, base+n-1)
	}
	t.Cleanup(func() {
		for _, l := range listeners {
			_ = l.Close()
		}
	})
}

func tryHold(base, n int) ([]net.Listener, bool) {
	listeners := make([]net.Listener, 0, n)
	for i := 0; i < n; i++ {
		l, err := net.Listen("tcp", net.JoinHostPort("127.0.0.1", strconv.Itoa(base+i)))
		if err != nil {
			return listeners, false
		}
		listeners = append(listeners, l)
	}
	return listeners, true
}

// occupyPorts занимает подряд n портов и возвращает первый из них. Нужен там,
// где прокси обязан не подняться: отказать ему в порту из другого пакета
// больше нечем.
func occupyPorts(t *testing.T, n int) int {
	t.Helper()
	base := freePortRun(t, n)
	holdPortRun(t, base, n)
	return base
}
