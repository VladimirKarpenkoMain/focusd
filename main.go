// Focusd — приложение для фокусировки. Направляет браузеры через локальный
// прокси и гасит отвлекающие сайты на время сессии.
package main

import (
	"context"
	"embed"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"
	"github.com/wailsapp/wails/v2/pkg/options/assetserver"
	"github.com/wailsapp/wails/v2/pkg/options/windows"

	"focusd/internal/config"
)

//go:embed all:frontend/dist
var assets embed.FS

const instanceLockName = "focusd.instance.lock"

func main() {
	dir := dataDir()
	log, logFile := setupLogging(dir)
	defer logFile.Close()

	switch arg(1) {
	case "--guard":
		runGuard(dir, log)
		return
	case "--restore":
		runRestore(dir, log)
		return
	case "--proxyguard":
		runProxyGuard(dir, log)
		return
	}

	store, err := config.Open(dir)
	if err != nil {
		log.Error("не удалось открыть конфигурацию", "err", err)
		fatal("Focusd: не удалось открыть конфигурацию:\n" + err.Error())
		return
	}

	// Единственный экземпляр: сторожевой задаче нужно понимать, запущено ли
	// приложение, не поднимая его окно лишний раз.
	lock, err := sys.AcquireLock(filepath.Join(dir, instanceLockName))
	if err == nil {
		defer lock.Release()
	}

	app := newApp(store, log)
	err = runWails(&options.App{
		Title:     "Focusd",
		Width:     mainWidth,
		Height:    mainHeight,
		MinWidth:  minWidth,
		MinHeight: minHeight,
		// Окно без системной рамки: заголовок рисует сам интерфейс. Без этого
		// компактный режим («виджет») выглядел бы как обычное окно, которое
		// случайно стало маленьким, — с полосой заголовка и кнопками.
		Frameless: true,
		AssetServer: &assetserver.Options{
			Assets: assets,
		},
		BackgroundColour: windowColour(store.Get().Settings.Theme),
		OnStartup:        app.startup,
		OnShutdown:       app.shutdown,
		Bind:             []any{app},
		SingleInstanceLock: &options.SingleInstanceLock{
			UniqueId: "focusd-9f2a-single-instance",
			OnSecondInstanceLaunch: func(options.SecondInstanceData) {
				if app.ctx != nil {
					appRuntime.WindowShow(app.ctx)
					appRuntime.WindowUnminimise(app.ctx)
				}
			},
		},
		Windows: &windows.Options{
			WebviewIsTransparent:              false,
			WindowIsTranslucent:               false,
			DisableFramelessWindowDecorations: false,
			ZoomFactor:                        1.0,
		},
	})
	if err != nil {
		log.Error("приложение остановлено с ошибкой", "err", err)
	}
}

// windowColour возвращает цвет подложки окна для выбранного оформления.
func windowColour(t config.Theme) *options.RGBA {
	if EffectiveTheme(t) == "dark" {
		return &windowColourDark
	}
	return &windowColourLight
}

// runGuard — сторожевой режим. Планировщик задач вызывает его раз в минуту,
// пока идёт жёсткая сессия: он не даёт снять блокировку, закрыв приложение.
func runGuard(dir string, log *slog.Logger) {
	store, err := config.Open(dir)
	if err != nil {
		return
	}
	f := store.Get()
	now := time.Now()
	ctx := context.Background()

	active := f.Session != nil && now.Unix() < f.Session.EndsAt && f.Session.Strictness == config.Lock
	alive := sys.IsLocked(filepath.Join(dir, instanceLockName))

	// Политика прокси снимается только тогда, когда приложение закрыто:
	// прокси живёт внутри него, и политика на исчезнувший прокси оставила бы
	// браузер без интернета.
	//
	// Пока приложение работает, политику трогать нельзя. Она прописана ему на
	// всё время работы намеренно: Chromium не перечитывает прокси-настройки на
	// ходу, поэтому снятие политики не возвращает доступ, а отбирает его — до
	// следующего опроса браузер ходит в никуда.
	if !alive {
		if err := sys.ClearBrowserProxy(); err != nil {
			log.Warn("сторож: не удалось снять политику прокси", "err", err)
		}
		// Системный прокси уводит в мёртвый порт уже не только браузеры, но и
		// всё, что ходит через систему, поэтому настройки человека возвращаем.
		restoreStoredSystemProxy(store, log)
	}

	if !active {
		// Сессии больше нет — снимаем политики браузеров, если они остались.
		if err := sys.UnblockSitesInBrowsers(); err != nil {
			log.Warn("сторож: не удалось снять политики браузеров", "err", err)
		}
		if exe, err := sys.ExecutablePath(); err == nil {
			_ = sys.SetGuard(ctx, false, exe)
		}
		return
	}

	// Политика URLBlocklist — запасной слой защиты, и закрытие приложения её
	// снимать не должно: иначе жёсткую сессию обходили бы одним кликом.
	domains, _ := buildRuleset(f.Session.Groups, f.Session.CustomDomains, f.Session.Strictness)
	if err := sys.BlockSitesInBrowsers(domains, f.Session.Allowlist); err != nil {
		log.Warn("сторож: не удалось прописать политики браузеров", "err", err)
	}

	// Поднимаем приложение, только если оно действительно не запущено.
	if alive {
		return
	}
	exe, err := sys.ExecutablePath()
	if err != nil {
		return
	}
	cmd := execCommand(exe)
	if err := cmd.Start(); err != nil {
		log.Warn("сторож: не удалось запустить приложение", "err", err)
		return
	}
	log.Info("сторож: приложение было закрыто во время жёсткой сессии и перезапущено")
}

// runProxyGuard — страховка от прокси, который остался прописан, когда самого
// приложения уже нет.
//
// Прокси живёт внутри процесса, а политика браузеров и системный прокси лежат в
// реестре и переживают всё: и закрытие приложения, и перезагрузку. Если Focusd
// не дожил до своего выхода, эти настройки уводят браузеры в порт, которого
// никто не слушает, — и человек видит ERR_PROXY_CONNECTION_FAILED на любом
// сайте. Ровно это и случилось после перезагрузки компьютера.
//
// Режим вызывается задачами планировщика: при входе в систему и раз в минуту.
// Пока приложение работает, он не делает ничего — его настройки трогать нельзя.
func runProxyGuard(dir string, log *slog.Logger) {
	if sys.IsLocked(filepath.Join(dir, instanceLockName)) {
		return // приложение работает
	}
	store, err := config.Open(dir)
	if err != nil {
		return
	}
	// Только политика прокси. Списки URLBlocklist не трогаем: они держат
	// жёсткую сессию, и снять их — значит подарить обход одним запуском.
	if err := sys.ClearBrowserProxy(); err != nil {
		log.Warn("сторож прокси: не удалось снять политику прокси", "err", err)
	}
	restoreStoredSystemProxy(store, log)
}

// runRestore — аварийная уборка: снять блокировку и вернуть браузеры в норму.
func runRestore(dir string, log *slog.Logger) {
	store, err := config.Open(dir)
	if err != nil {
		return
	}
	ctx := context.Background()
	_ = sys.UnblockSitesInBrowsers()
	_ = sys.ClearBrowserProxy()
	// Системный прокси тоже возвращаем: после аварийного восстановления он не
	// должен остаться направленным на Focusd.
	restoreStoredSystemProxy(store, log)

	if exe, err := sys.ExecutablePath(); err == nil {
		_ = sys.SetGuard(ctx, false, exe)
		_ = sys.SetProxyGuard(ctx, false, exe)
	}
	_ = store.Update(func(f *config.File) error {
		f.Session = nil
		return nil
	})
	log.Info("аварийное восстановление выполнено")
}

// dataDir возвращает каталог для конфигурации и журнала.
func dataDir() string {
	base := os.Getenv("LOCALAPPDATA")
	if base == "" {
		if d, err := os.UserConfigDir(); err == nil {
			base = d
		} else {
			base = "."
		}
	}
	return filepath.Join(base, "Focusd")
}

func setupLogging(dir string) (*slog.Logger, io.Closer) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), io.NopCloser(nil)
	}
	f, err := os.OpenFile(filepath.Join(dir, "focusd.log"), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o644)
	if err != nil {
		return slog.New(slog.NewTextHandler(io.Discard, nil)), io.NopCloser(nil)
	}
	return slog.New(slog.NewTextHandler(f, &slog.HandlerOptions{Level: slog.LevelInfo})), f
}

func arg(i int) string {
	if len(os.Args) > i {
		return os.Args[i]
	}
	return ""
}

func fatal(msg string) {
	// Окна консоли у GUI-приложения нет, поэтому показываем системное окно.
	_ = execCommand("mshta",
		`javascript:alert('`+escapeJS(msg)+`');close()`).Run()
}

func escapeJS(s string) string {
	out := make([]rune, 0, len(s))
	for _, r := range s {
		switch r {
		case '\'', '\\':
			out = append(out, '\\', r)
		case '\n', '\r':
			out = append(out, ' ')
		default:
			out = append(out, r)
		}
	}
	return string(out)
}
