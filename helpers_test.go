package main

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"focusd/internal/config"
	"focusd/internal/focus"
	"focusd/internal/proxy"
	"focusd/internal/winsys"
)

// helpers_test.go собирает подмены, общие для тестов приложения: систему,
// окно Wails, менеджер сессий и сам App. Настоящие политики реестра, задачи
// планировщика и окно WebView2 в тесте недоступны, а решения приложения —
// когда снять блокировку, что вернуть человеку, что показать — проверить
// нужно.

// quietLogger пишет в никуда: журнал в тестах не нужен, но приложение без
// logger'а не собирается.
func quietLogger() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

// ---- Система ----

// sysStub — systemAPI с безобидными ответами по умолчанию. Тест подменяет
// только те вызовы, которые проверяет, — до install.
type sysStub struct {
	systemAPI
}

func newSysStub() *sysStub {
	s := &sysStub{}
	s.IsElevated = func() bool { return true }
	s.SystemPrefersDark = func() bool { return false }
	// Язык системы в тестах — русский: русский текст и есть исходный, поэтому
	// проверки читаются как обычные литералы, а не как таблица переводов.
	s.SystemLanguage = func() string { return "ru-RU" }
	s.RunningBrowsers = func(context.Context) []string { return nil }
	s.SitesBlockedInBrowsers = func() bool { return false }
	s.BlockSitesInBrowsers = func([]string, []string) error { return nil }
	s.UnblockSitesInBrowsers = func() error { return nil }
	s.SetBrowserProxy = func(string) error { return nil }
	s.ClearBrowserProxy = func() error { return nil }
	s.BrowserProxyAddr = func() string { return "" }
	s.SetSystemProxy = func(string) (winsys.SystemProxySettings, error) {
		return winsys.SystemProxySettings{}, nil
	}
	s.RestoreSystemProxy = func(winsys.SystemProxySettings) error { return nil }
	s.SystemProxyAddr = func() string { return "" }
	s.DisableSystemProxy = func() error { return nil }
	s.ExecutablePath = func() (string, error) { return `C:\Focusd\Focusd.exe`, nil }
	s.SetGuard = func(context.Context, bool, string) error { return nil }
	s.SetProxyGuard = func(context.Context, bool, string) error { return nil }
	s.SetAutostart = func(context.Context, bool, string) error { return nil }
	s.AcquireLock = func(string) (*winsys.LockFile, error) { return nil, nil }
	s.IsLocked = func(string) bool { return false }
	return s
}

// install подставляет подмену на время теста. Указатель на ту же структуру, а
// не копия: тест донастраивает отдельные ответы уже после установки.
func (s *sysStub) install(t *testing.T) *sysStub {
	t.Helper()
	saved := sys
	sys = &s.systemAPI
	t.Cleanup(func() { sys = saved })
	return s
}

// ---- Окно Wails ----

type runtimeStub struct {
	wailsRuntime

	emitted     []string
	minSizes    [][2]int
	sizes       [][2]int
	positions   [][2]int
	onTop       []bool
	backgrounds [][4]uint8
	centred     int
	shown       int
	unminimised int
	moved       [2]int

	screens   []runtime.Screen
	screenErr error
}

func newRuntimeStub() *runtimeStub {
	r := &runtimeStub{}
	r.EventsEmit = func(_ context.Context, name string, _ ...any) {
		r.emitted = append(r.emitted, name)
	}
	r.ScreenGetAll = func(context.Context) ([]runtime.Screen, error) { return r.screens, r.screenErr }
	r.WindowCenter = func(context.Context) { r.centred++ }
	r.WindowGetPosition = func(context.Context) (int, int) { return r.moved[0], r.moved[1] }
	r.WindowSetAlwaysOnTop = func(_ context.Context, onTop bool) { r.onTop = append(r.onTop, onTop) }
	r.WindowSetBackgroundColour = func(_ context.Context, red, g, b, a uint8) {
		r.backgrounds = append(r.backgrounds, [4]uint8{red, g, b, a})
	}
	r.WindowSetMinSize = func(_ context.Context, w, h int) { r.minSizes = append(r.minSizes, [2]int{w, h}) }
	r.WindowSetPosition = func(_ context.Context, x, y int) { r.positions = append(r.positions, [2]int{x, y}) }
	r.WindowSetSize = func(_ context.Context, w, h int) { r.sizes = append(r.sizes, [2]int{w, h}) }
	r.WindowShow = func(context.Context) { r.shown++ }
	r.WindowUnminimise = func(context.Context) { r.unminimised++ }
	return r
}

func (r *runtimeStub) install(t *testing.T) *runtimeStub {
	t.Helper()
	saved := appRuntime
	appRuntime = r.wailsRuntime
	t.Cleanup(func() { appRuntime = saved })
	return r
}

// ---- Сессии ----

type focusStub struct {
	view        focus.View
	sess        *config.Session
	active      bool
	startErr    error
	cancelWait  int
	cancelErr   error
	confirmErr  error
	forceErr    error
	currentCall int
	started     []focus.Options
	cancelled   int
	forced      int
}

func newFocusStub() *focusStub { return &focusStub{} }

func (f *focusStub) Current(time.Time) focus.View {
	f.currentCall++
	if !f.active {
		return focus.View{}
	}
	return f.view
}

func (f *focusStub) Active(time.Time) (*config.Session, bool) { return f.sess, f.active }

func (f *focusStub) Start(o focus.Options, _ time.Time) error {
	if f.startErr != nil {
		return f.startErr
	}
	f.started = append(f.started, o)
	return nil
}

func (f *focusStub) RequestCancel(time.Time) (int, error) { return f.cancelWait, f.cancelErr }
func (f *focusStub) ConfirmCancel(time.Time) error        { return f.confirmErr }
func (f *focusStub) ForceComplete() error                 { return f.forceErr }

// ---- Приложение ----

// testStore открывает настоящее хранилище во временном каталоге: конфигурация
// тестам как раз и нужна настоящая — её поведение проверено отдельно.
func testStore(t *testing.T) *config.Store {
	t.Helper()
	s, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatalf("не удалось открыть хранилище: %v", err)
	}
	return s
}

// useLocalProxyTarget уводит проверку прокси на локальный сервер: слой не
// должен зависеть от интернета, а поведение проверяется то же самое.
func useLocalProxyTarget(t *testing.T) *httptest.Server {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = io.WriteString(w, "живой сайт")
	}))
	t.Cleanup(srv.Close)

	saved := proxyTestURL
	proxyTestURL = srv.URL
	t.Cleanup(func() { proxyTestURL = saved })
	return srv
}

// appOption настраивает тестовое приложение до подмены швов.
type appOption func(*App)

func withoutProxy() appOption {
	return func(a *App) { a.prox = proxy.New(0, quietLogger()) }
}

func withProxyRunning() appOption {
	return func(a *App) {
		if err := a.prox.Start(); err != nil {
			panic(err)
		}
	}
}

// newTestApp собирает приложение с подставными системой, окном и сессиями.
// Прокси настоящий: он слушает локальный порт, и это единственный способ
// проверить решения, которые от него зависят.
func newTestApp(t *testing.T, opts ...appOption) (*App, *sysStub, *runtimeStub, *focusStub) {
	t.Helper()
	s := newSysStub().install(t)
	r := newRuntimeStub().install(t)
	f := newFocusStub()

	store := testStore(t)
	log := quietLogger()
	a := &App{
		ctx:    context.Background(),
		store:  store,
		prox:   proxy.New(0, log),
		focus:  f,
		log:    log,
		stopCh: make(chan struct{}),
	}
	for _, opt := range opts {
		opt(a)
	}
	t.Cleanup(func() { _ = a.prox.Stop() })
	return a, s, r, f
}

// softSession — сессия, которая уже идёт и ещё не кончилась.
func softSession() *config.Session {
	now := time.Now()
	return &config.Session{
		Strictness:     config.Soft,
		StartedAt:      now.Unix(),
		EndsAt:         now.Add(10 * time.Minute).Unix(),
		DurationSec:    600,
		CancelUnlockAt: now.Unix(),
		Groups:         []string{"social"},
		Label:          "Фокус на 10 мин",
	}
}

// Чтение состояния приложения — через те же замки, что и в бою: часть полей
// может менять фоновая петля, и без замка тест ловил бы гонки.

func (a *App) currentError() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.errText
}

// Текст подсказки и замечания хранится ключом, а показывается переведённым:
// тест читает то же, что увидит человек. Русские литералы, пришедшие из теста,
// возвращаются как есть — неизвестный ключ переводить нечем.

func (a *App) currentNotice() string {
	a.mu.Lock()
	key := a.notice
	a.mu.Unlock()
	return a.text(key)
}

func (a *App) currentHint() string {
	a.mu.Lock()
	key, args := a.hint, a.hintArgs
	a.mu.Unlock()
	return a.text(key, args...)
}

func (a *App) isEngaged() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.engaged
}

func (a *App) currentStalled() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.proxyStalled
}

func (a *App) currentBlocked() int64 {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.blocked
}

func (a *App) currentLastBlocked() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.lastBlocked
}

func (a *App) setBlocked(n int64, day string) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.blocked, a.blockDay = n, day
}

func (a *App) setEngaged(v bool) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.engaged = v
}

func (a *App) setStalled(n int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.proxyStalled = n
}

func (a *App) setHealthBaseline(forwarded, failed int64) {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.proxyBaseForwarded, a.proxyBaseFailed = forwarded, failed
}

func (a *App) currentDay() string {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.blockDay
}

func (a *App) proxyGuardState() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.proxyGuard
}

func (a *App) widgetPosition() (int, int) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.widgetX, a.widgetY
}
