package main

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"

	"focusd/internal/config"
	"focusd/internal/winsys"
)

// stubCommand подменяет запуск внешних программ: настоящий mshta показал бы
// окно, а настоящий explorer открыл бы проводник.
func stubCommand(t *testing.T, startErr error) *[]string {
	t.Helper()
	var calls []string
	saved := execCommand
	execCommand = func(name string, arg ...string) *exec.Cmd {
		calls = append(calls, name+" "+strings.Join(arg, " "))
		if startErr != nil {
			return exec.Command("нет-такой-программы-focusd")
		}
		return exec.Command("cmd", "/c", "exit", "0")
	}
	t.Cleanup(func() { execCommand = saved })
	return &calls
}

// ---- Каталог данных и журнал ----

func TestDataDir(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	if got := dataDir(); got != filepath.Join(dir, "Focusd") {
		t.Fatalf("dataDir = %q", got)
	}
}

func TestDataDirFallsBackToUserConfig(t *testing.T) {
	newSysStub().install(t)
	appData := t.TempDir()
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("AppData", appData)

	want := filepath.Join(appData, "Focusd")
	if got := dataDir(); got != want {
		t.Fatalf("dataDir = %q, ожидалось %q", got, want)
	}
}

// Когда и того и другого нет, приложение не падает: каталогом становится
// текущий.
func TestDataDirFallsBackToCurrent(t *testing.T) {
	newSysStub().install(t)
	t.Setenv("LOCALAPPDATA", "")
	t.Setenv("AppData", "")

	if got := dataDir(); got != "Focusd" {
		t.Fatalf("dataDir = %q", got)
	}
}

func TestSetupLogging(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	log, closer := setupLogging(dir)
	if log == nil || closer == nil {
		t.Fatal("журнал не заведён")
	}
	log.Info("проверка")
	closer.Close()

	raw, err := os.ReadFile(filepath.Join(dir, "focusd.log"))
	if err != nil {
		t.Fatalf("файл журнала: %v", err)
	}
	if !strings.Contains(string(raw), "проверка") {
		t.Fatalf("запись не попала в журнал: %q", raw)
	}
}

// Каталог данных недоступен: приложение обязано запуститься молча, а не упасть.
func TestSetupLoggingWithoutDir(t *testing.T) {
	newSysStub().install(t)
	base := t.TempDir()
	fileInstead := filepath.Join(base, "не-каталог")
	if err := os.WriteFile(fileInstead, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	log, closer := setupLogging(fileInstead)
	if log == nil || closer == nil {
		t.Fatal("при недоступном каталоге журнал всё равно нужен")
	}
	log.Warn("никому")
	closer.Close()
}

// Каталог есть, а файл журнала открыть нельзя: то же самое — запускаемся.
func TestSetupLoggingWithoutFile(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "focusd.log"), 0o755); err != nil {
		t.Fatal(err)
	}

	log, closer := setupLogging(dir)
	if log == nil || closer == nil {
		t.Fatal("при неудачном открытии файла журнал всё равно нужен")
	}
	closer.Close()
}

func TestArg(t *testing.T) {
	newSysStub().install(t)
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })

	os.Args = []string{"Focusd", "--guard"}
	if got := arg(1); got != "--guard" {
		t.Fatalf("arg(1) = %q", got)
	}
	if got := arg(2); got != "" {
		t.Fatalf("отсутствующий аргумент = %q", got)
	}
}

func TestEscapeJS(t *testing.T) {
	newSysStub().install(t)
	cases := []struct {
		in   string
		want string
	}{
		{"просто текст", "просто текст"},
		{"не закрыть'", `не закрыть\'`},
		{`обратный \ слэш`, `обратный \\ слэш`},
		{"первая\nвторая", "первая вторая"},
		{"первая\r\nвторая", "первая  вторая"},
	}
	for _, c := range cases {
		if got := escapeJS(c.in); got != c.want {
			t.Errorf("escapeJS(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestFatalShowsSystemWindow(t *testing.T) {
	newSysStub().install(t)
	calls := stubCommand(t, nil)
	fatal("Focusd: всё сломалось'\nвторая строка")

	if len(*calls) != 1 || !strings.HasPrefix((*calls)[0], "mshta ") {
		t.Fatalf("системное окно не показано: %v", *calls)
	}
	if strings.Contains((*calls)[0], "\n") {
		t.Fatalf("перевод строки попал в скрипт: %q", (*calls)[0])
	}
}

// ---- Режим сторожа ----

func TestRunGuardReopensAppDuringLock(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.Session = &config.Session{
			Strictness: config.Lock,
			EndsAt:     time.Now().Add(time.Hour).Unix(),
			Groups:     []string{"social"},
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var blocked int
	var started []string
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { return nil }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { return nil }
	sys.BlockSitesInBrowsers = func([]string, []string) error { blocked++; return nil }
	sys.ExecutablePath = func() (string, error) { return `C:\Focusd.exe`, nil }

	saved := execCommand
	execCommand = func(name string, arg ...string) *exec.Cmd {
		started = append(started, name)
		return exec.Command("cmd", "/c", "exit", "0")
	}
	t.Cleanup(func() { execCommand = saved })

	runGuard(dir, quietLogger())

	if len(started) == 0 {
		t.Fatal("приложение не перезапущено")
	}
	if blocked != 1 {
		t.Fatalf("списки блокировки не прописаны: %d", blocked)
	}
}

// Пока приложение живо, политику прокси трогать нельзя: снятие не возвращает
// доступ, а отбирает его.
func TestRunGuardLeavesAliveAppAlone(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.Session = &config.Session{
			Strictness: config.Lock,
			EndsAt:     time.Now().Add(time.Hour).Unix(),
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var cleared, blocked int
	sys.IsLocked = func(string) bool { return true }
	sys.ClearBrowserProxy = func() error { cleared++; return nil }
	sys.BlockSitesInBrowsers = func([]string, []string) error { blocked++; return nil }
	sys.UnblockSitesInBrowsers = func() error { return nil }

	runGuard(dir, quietLogger())

	if cleared != 0 {
		t.Fatal("политику прокси сняли у работающего приложения")
	}
	if blocked != 1 {
		t.Fatalf("жёсткая сессия не подперта политикой браузеров: %d", blocked)
	}
}

func TestRunGuardEndsExpiredSession(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	openStoreIn(t, dir)

	var unblocked int
	var guard []bool
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { return nil }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { return nil }
	sys.UnblockSitesInBrowsers = func() error { unblocked++; return nil }
	sys.SetGuard = func(_ context.Context, enable bool, _ string) error {
		guard = append(guard, enable)
		return nil
	}

	runGuard(dir, quietLogger())

	if unblocked != 1 {
		t.Fatalf("политики браузеров не сняты: %d", unblocked)
	}
	if len(guard) != 1 || guard[0] {
		t.Fatalf("сторожевая задача не снята: %v", guard)
	}
}

// Ошибки системных вызовов в сторожевом режиме не должны прерывать уборку.
func TestRunGuardKeepsGoingAfterErrors(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	openStoreIn(t, dir)

	var restored int
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { return errors.New("отказано в доступе") }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { restored++; return nil }
	sys.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }
	sys.ExecutablePath = func() (string, error) { return "", errors.New("путь неизвестен") }
	if err := os.Mkdir(filepath.Join(dir, "config.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := openStoreIn(t, dir).Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		// Запись сломана намеренно — настройки не сохранятся, и это нормально.
		_ = err
	}

	runGuard(dir, quietLogger())
}

// Ошибки политик и отсутствие пути к приложению сторож не должен считать
// поводом бросить сессию: он лишь говорит о них в журнале.
func TestRunGuardReportsErrors(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.Session = &config.Session{
			Strictness: config.Lock,
			EndsAt:     time.Now().Add(time.Hour).Unix(),
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	sys.IsLocked = func(string) bool { return false }
	sys.BlockSitesInBrowsers = func([]string, []string) error { return errors.New("отказано в доступе") }
	sys.ExecutablePath = func() (string, error) { return "", errors.New("путь неизвестен") }

	log, buf := loggerToBuffer()
	runGuard(dir, log)

	if !strings.Contains(buf.String(), "не удалось прописать политики браузеров") {
		t.Fatalf("о сбое политик не сказано: %s", buf.String())
	}
}

func TestRunGuardStopsOnBrokenStore(t *testing.T) {
	newSysStub().install(t)
	base := t.TempDir()
	fileInstead := filepath.Join(base, "не-каталог")
	if err := os.WriteFile(fileInstead, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	sys.ClearBrowserProxy = func() error {
		t.Fatal("без хранилища ничего делать не нужно")
		return nil
	}
	runGuard(fileInstead, quietLogger())
}

// Запуск приложения не удался: сторож не должен падать, но и молчать о причине
// не должен — она уходит в журнал.
func TestRunGuardRestartFailure(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.Session = &config.Session{
			Strictness: config.Lock,
			EndsAt:     time.Now().Add(time.Hour).Unix(),
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	calls := stubCommand(t, errors.New("нет файла"))
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { return nil }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { return nil }
	sys.BlockSitesInBrowsers = func([]string, []string) error { return nil }

	log, buf := loggerToBuffer()
	runGuard(dir, log)

	if !strings.Contains(buf.String(), "не удалось запустить приложение") {
		t.Fatalf("о сбое запуска не сказано: %s", buf.String())
	}
	_ = calls
}

// ---- Страховка прокси ----

func TestRunProxyGuardSkipsWhenAppAlive(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	sys.IsLocked = func(string) bool { return true }
	sys.ClearBrowserProxy = func() error {
		t.Fatal("у работающего приложения настройки трогать нельзя")
		return nil
	}

	runProxyGuard(dir, quietLogger())
}

func TestRunProxyGuardReturnsSettings(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Enable: true, Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var cleared int
	var restored []winsys.SystemProxySettings
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { cleared++; return nil }
	sys.RestoreSystemProxy = func(p winsys.SystemProxySettings) error {
		restored = append(restored, p)
		return nil
	}

	runProxyGuard(dir, quietLogger())

	if cleared != 1 {
		t.Fatalf("политика прокси не снята: %d", cleared)
	}
	if len(restored) != 1 || restored[0].Server != "10.0.0.1:8080" {
		t.Fatalf("настройки человека не возвращены: %+v", restored)
	}
}

func TestRunProxyGuardKeepsGoingAfterErrors(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	openStoreIn(t, dir)

	var restored int
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { return errors.New("отказано в доступе") }
	sys.SystemProxyAddr = func() string { return "" }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { restored++; return nil }

	runProxyGuard(dir, quietLogger())
	if restored != 0 {
		t.Fatal("возвращать было нечего, а настройки всё же трогали")
	}
}

func TestRunProxyGuardStopsOnBrokenStore(t *testing.T) {
	newSysStub().install(t)
	base := t.TempDir()
	fileInstead := filepath.Join(base, "не-каталог")
	if err := os.WriteFile(fileInstead, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error {
		t.Fatal("без хранилища трогать нечего")
		return nil
	}

	runProxyGuard(fileInstead, quietLogger())
}

// ---- Аварийное восстановление ----

func TestRunRestore(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.Session = &config.Session{Strictness: config.Lock, EndsAt: time.Now().Add(time.Hour).Unix()}
		f.SystemProxy = &config.SystemProxyBackup{Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var unblocked, cleared, restored int
	var guards, proxyGuards []bool
	sys.UnblockSitesInBrowsers = func() error { unblocked++; return nil }
	sys.ClearBrowserProxy = func() error { cleared++; return nil }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { restored++; return nil }
	sys.SetGuard = func(_ context.Context, enable bool, _ string) error {
		guards = append(guards, enable)
		return nil
	}
	sys.SetProxyGuard = func(_ context.Context, enable bool, _ string) error {
		proxyGuards = append(proxyGuards, enable)
		return nil
	}

	log, buf := loggerToBuffer()
	runRestore(dir, log)

	if unblocked != 1 || cleared != 1 || restored != 1 {
		t.Fatalf("уборка неполная: политики %d, прокси %d, настройки %d", unblocked, cleared, restored)
	}
	if len(guards) != 1 || guards[0] || len(proxyGuards) != 1 || proxyGuards[0] {
		t.Fatalf("задачи не сняты: %v / %v", guards, proxyGuards)
	}
	if reopened := openStoreIn(t, dir); reopened.Get().Session != nil {
		t.Fatal("сессия не снята")
	}
	if !strings.Contains(buf.String(), "аварийное восстановление выполнено") {
		t.Fatalf("о восстановлении не сказано: %s", buf.String())
	}
}

// Без пути к приложению снять задачи нечем — остальная уборка всё равно идёт.
func TestRunRestoreWithoutExePath(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	openStoreIn(t, dir)

	var guards int
	sys.ExecutablePath = func() (string, error) { return "", errors.New("путь неизвестен") }
	sys.SetGuard = func(context.Context, bool, string) error { guards++; return nil }
	sys.SetProxyGuard = func(context.Context, bool, string) error { guards++; return nil }

	runRestore(dir, quietLogger())
	if guards != 0 {
		t.Fatalf("задачи тронуты без пути к приложению: %d", guards)
	}
}

func TestRunRestoreKeepsGoingAfterErrors(t *testing.T) {
	newSysStub().install(t)
	dir := t.TempDir()
	store := openStoreIn(t, dir)
	if err := store.Update(func(f *config.File) error {
		f.SystemProxy = &config.SystemProxyBackup{Server: "10.0.0.1:8080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	var restored int
	sys.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }
	sys.ClearBrowserProxy = func() error { return errors.New("отказано в доступе") }
	sys.RestoreSystemProxy = func(winsys.SystemProxySettings) error { restored++; return nil }
	sys.SetGuard = func(context.Context, bool, string) error { return errors.New("schtasks отказал") }
	sys.SetProxyGuard = func(context.Context, bool, string) error { return errors.New("schtasks отказал") }

	runRestore(dir, quietLogger())

	if restored != 1 {
		t.Fatalf("настройки системного прокси не возвращены: %d", restored)
	}
	if openStoreIn(t, dir).Get().Session != nil {
		t.Fatal("сессия не снята")
	}
}

func TestRunRestoreStopsOnBrokenStore(t *testing.T) {
	newSysStub().install(t)
	base := t.TempDir()
	fileInstead := filepath.Join(base, "не-каталог")
	if err := os.WriteFile(fileInstead, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	sys.UnblockSitesInBrowsers = func() error {
		t.Fatal("без хранилища ничего делать не нужно")
		return nil
	}

	runRestore(fileInstead, quietLogger())
}

// ---- Точка входа ----

// useDataDir уводит каталог данных приложения во временный.
func useDataDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	t.Setenv("LOCALAPPDATA", dir)
	return dir
}

func runMain(t *testing.T, args ...string) {
	t.Helper()
	saved := os.Args
	os.Args = append([]string{"Focusd"}, args...)
	t.Cleanup(func() { os.Args = saved })
	main()
}

func TestMainRestoreMode(t *testing.T) {
	newSysStub().install(t)
	useDataDir(t)
	var unblocked int
	sys.UnblockSitesInBrowsers = func() error { unblocked++; return nil }
	stubCommand(t, nil)

	runMain(t, "--restore")

	if unblocked != 1 {
		t.Fatalf("аварийное восстановление не запущено: %d", unblocked)
	}
}

func TestMainGuardMode(t *testing.T) {
	newSysStub().install(t)
	useDataDir(t)
	var unblocked int
	sys.IsLocked = func(string) bool { return false }
	sys.UnblockSitesInBrowsers = func() error { unblocked++; return nil }

	runMain(t, "--guard")

	if unblocked != 1 {
		t.Fatalf("сторож не отработал: %d", unblocked)
	}
}

func TestMainProxyGuardMode(t *testing.T) {
	newSysStub().install(t)
	useDataDir(t)
	var cleared int
	sys.IsLocked = func(string) bool { return false }
	sys.ClearBrowserProxy = func() error { cleared++; return nil }

	runMain(t, "--proxyguard")

	if cleared != 1 {
		t.Fatalf("страховка прокси не отработала: %d", cleared)
	}
}

// Обычный запуск: доходит до окна и передаёт ему приложение.
func TestMainRunsWindow(t *testing.T) {
	newSysStub().install(t)
	dir := useDataDir(t)
	var opened int
	var title string
	var locks []string
	sys.AcquireLock = func(path string) (*winsys.LockFile, error) {
		locks = append(locks, path)
		return nil, nil
	}
	savedRun := runWails
	runWails = func(opts *options.App) error {
		opened++
		title = opts.Title
		// Приложение уже живёт: окно поднято, контекст есть.
		for _, bound := range opts.Bind {
			if app, ok := bound.(*App); ok {
				app.ctx = context.Background()
			}
		}
		// Второй экземпляр должен показать уже существующее окно.
		if opts.SingleInstanceLock != nil {
			opts.SingleInstanceLock.OnSecondInstanceLaunch(options.SecondInstanceData{})
		}
		return errors.New("окно закрыто тестом")
	}
	t.Cleanup(func() { runWails = savedRun })

	var shown int
	r := newRuntimeStub()
	r.WindowShow = func(context.Context) { shown++ }
	r.WindowUnminimise = func(context.Context) { shown++ }
	r.install(t)

	runMain(t)

	if opened != 1 {
		t.Fatalf("окно не поднималось: %d", opened)
	}
	if title != "Focusd" {
		t.Fatalf("заголовок окна: %q", title)
	}
	// Файл блокировки единственного экземпляра лежит рядом с конфигурацией.
	if len(locks) != 1 || locks[0] != filepath.Join(dir, "Focusd", instanceLockName) {
		t.Fatalf("файл единственного экземпляра: %v", locks)
	}
	// Окно ещё не создано (OnStartup не звался), поэтому показывать нечего.
	if shown != 2 {
		t.Fatalf("существующее окно не показано: %d", shown)
	}
}

// Открыть конфигурацию не удалось: приложение обязано сказать об этом в
// системном окне, а не упасть молча.
func TestMainReportsBrokenConfig(t *testing.T) {
	newSysStub().install(t)
	base := t.TempDir()
	fileInstead := filepath.Join(base, "Focusd")
	if err := os.WriteFile(fileInstead, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Setenv("LOCALAPPDATA", base)

	calls := stubCommand(t, nil)
	savedRun := runWails
	runWails = func(*options.App) error {
		t.Fatal("окно не должно подниматься без конфигурации")
		return nil
	}
	t.Cleanup(func() { runWails = savedRun })

	runMain(t)

	if len(*calls) == 0 || !strings.Contains((*calls)[0], "не удалось открыть конфигурацию") {
		t.Fatalf("о поломке не сказано: %v", *calls)
	}
}

// ---- Вспомогательное ----

// openStoreIn открывает хранилище в готовом каталоге.
func openStoreIn(t *testing.T, dir string) *config.Store {
	t.Helper()
	store, err := config.Open(dir)
	if err != nil {
		t.Fatalf("не удалось открыть хранилище: %v", err)
	}
	return store
}

// loggerToBuffer отдаёт журнал, который можно прочитать в тесте.
func loggerToBuffer() (*slog.Logger, *strings.Builder) {
	var buf strings.Builder
	return slog.New(slog.NewTextHandler(&buf, nil)), &buf
}
