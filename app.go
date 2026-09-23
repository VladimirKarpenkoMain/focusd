package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"sync"
	"time"

	"github.com/wailsapp/wails/v2/pkg/options"

	"focusd/internal/blockpage"
	"focusd/internal/catalog"
	"focusd/internal/config"
	"focusd/internal/focus"
	"focusd/internal/i18n"
	"focusd/internal/proxy"
	"focusd/internal/rules"
	"focusd/internal/winsys"
)

// Version — версия приложения, показывается в интерфейсе.
const Version = "0.2.0"

// Размеры окна. Виджет — это то же окно в компактном режиме: отдельного окна
// Wails v2 не умеет, а поднимать ради таймера второй процесс незачем.
const (
	mainWidth, mainHeight = 1100, 740
	minWidth, minHeight   = 940, 620
	// Плашка и круг — две формы одного и того же компактного окна. Размеры
	// разные, потому что круг с кольцом прогресса требует квадрата, а под
	// кольцом ещё должна поместиться кнопка возврата в обычное окно.
	barWidth, barHeight   = 320, 140
	ringWidth, ringHeight = 224, 224
	widgetMargin          = 24
)

// Цвет подложки окна. Он виден в момент запуска и при изменении размера, пока
// WebView ещё не перерисовался, поэтому обязан совпадать с фоном интерфейса —
// иначе тёмная тема начинается с белой вспышки.
var (
	windowColourLight = options.RGBA{R: 0xF4, G: 0xF5, B: 0xF7, A: 1}
	windowColourDark  = options.RGBA{R: 0x0E, G: 0x0F, B: 0x13, A: 1}
)

// EffectiveTheme сводит настройку «как в системе» к конкретному оформлению.
func EffectiveTheme(t config.Theme) string {
	switch t {
	case config.ThemeDark:
		return "dark"
	case config.ThemeLight:
		return "light"
	}
	if sys.SystemPrefersDark() {
		return "dark"
	}
	return "light"
}

// EffectiveLanguage сводит настройку «как в системе» к конкретному языку.
func EffectiveLanguage(l config.Language) i18n.Lang {
	if lang, ok := i18n.Known(string(l)); ok {
		return lang
	}
	return i18n.FromSystem(sys.SystemLanguage())
}

// StartOptions — параметры запуска сессии, приходящие из интерфейса.
type StartOptions struct {
	DurationMin   int      `json:"durationMin"`
	Strictness    string   `json:"strictness"`
	Groups        []string `json:"groups"`
	CustomDomains []string `json:"customDomains"`
	Allowlist     []string `json:"allowlist"`
	Label         string   `json:"label"`
}

// State — полный снимок состояния приложения для интерфейса.
type State struct {
	Elevated      bool       `json:"elevated"`
	ActiveGroups  []string   `json:"activeGroups"`
	CustomDomains []string   `json:"customDomains"`
	Allowlist     []string   `json:"allowlist"`
	BlockedToday  int64      `json:"blockedToday"`
	TotalRules    int        `json:"totalRules"`
	LastBlocked   string     `json:"lastBlocked"`
	Session       focus.View `json:"session"`
	DataDir       string     `json:"dataDir"`
	Version       string     `json:"version"`
	LastError     string     `json:"lastError"`
	// BrowserBlock — главный признак того, что защита работает прямо сейчас.
	BrowserBlock bool `json:"browserBlock"`
	// ProxyBlock — браузеры ходят через локальный прокси Focusd именно сейчас,
	// и он же считает отсечённые запросы. URLBlocklist — запасной слой: он
	// обрывает запрос внутри браузера и снаружи не наблюдаем.
	ProxyBlock bool `json:"proxyBlock"`
	// Hint — что человеку сделать прямо сейчас, чтобы блокировка заработала.
	// Единственный случай: браузер был открыт до начала сессии.
	Hint string `json:"hint"`
	// Notice — состояние слоёв блокировки. Показывается в настройках, а не на
	// главном экране: на саму блокировку оно не влияет.
	Notice string `json:"notice"`
	// Theme и Widget — оформление и режим компактного таймера.
	Theme string `json:"theme"`
	// ResolvedTheme — то же самое, но «как в системе» уже сведено к светлому
	// или тёмному. Нужно, чтобы окно успело покраситься до первого кадра.
	ResolvedTheme string `json:"resolvedTheme"`
	// Language и ResolvedLanguage — то же самое для языка интерфейса: выбор
	// («auto», «ru», «en», «zh») и то, во что он разрешился. Интерфейс берёт
	// второй: разрешать «как в системе» дважды — значит однажды разойтись.
	Language         string `json:"language"`
	ResolvedLanguage string `json:"resolvedLanguage"`
	Widget           bool   `json:"widget"`
	// WidgetOnTop — держать компактный таймер поверх остальных окон.
	WidgetOnTop bool `json:"widgetOnTop"`
	// WidgetShape — форма, которую окно имеет сейчас: «bar» или «ring».
	// Отдаётся отдельно от настройки, потому что применить другую форму окно
	// может только когда сессия идёт, а показать выбранную нужно сразу.
	WidgetShape string `json:"widgetShape"`
}

// CancelInfo описывает, когда и как можно прервать сессию.
type CancelInfo struct {
	WaitSec int    `json:"waitSec"`
	Message string `json:"message"`
}

// sessions — то, что приложение просит у менеджера сессий.
//
// Интерфейс, а не конкретный тип, ради одной ветки: сразу после удачного Start
// сессия обязана читаться обратно, и что делать, если её вдруг нет, проверить
// иначе нечем — на живом хранилище такого не бывает. Заодно это позволяет
// проверить реакцию приложения на любой отказ хранилища.
type sessions interface {
	Current(now time.Time) focus.View
	Active(now time.Time) (*config.Session, bool)
	Start(o focus.Options, now time.Time) error
	RequestCancel(now time.Time) (int, error)
	ConfirmCancel(now time.Time) error
	ForceComplete() error
}

// App — корень приложения: связывает хранилище, прокси и сессии.
type App struct {
	ctx   context.Context
	store *config.Store
	prox  *proxy.Server
	focus sessions
	log   *slog.Logger

	mu       sync.Mutex
	errText  string
	notice   string
	hint     string
	// hintArgs — аргументы подсказки. Текст подсказки хранится ключом, а не
	// готовой строкой: язык можно сменить посреди сессии, и подсказка обязана
	// сменить его вместе с остальным интерфейсом.
	hintArgs []any
	engaged  bool
	blocked  int64 // счётчик за сутки, хранится в памяти
	blockDay string
	// Сколько отсечённых запросов уже перенесено из счётчика прокси: разница
	// между ним и суточным счётчиком и есть дельта, которую нужно добавить.
	lastProxyTotal int64
	lastBlocked    string
	// proxyEngaged — браузеры направлены на прокси, значит за его здоровьем
	// нужно следить. proxyMode — режим слоя: выключен, сторожим или режем.
	proxyMode          int
	proxyBaseForwarded int64
	proxyBaseFailed    int64
	proxyStalled       int
	// proxyGuard — страховочные задачи планировщика поставлены. Держим их ровно
	// столько, сколько прописана политика прокси, и не дёргаем schtasks зря.
	proxyGuard bool
	// Положение виджета, снятое на ходу. На диск попадает при выходе из
	// режима и при закрытии приложения, а не каждый кадр перетаскивания.
	widgetX, widgetY int
	stopCh           chan struct{}
}

func newApp(store *config.Store, log *slog.Logger) *App {
	f := store.Get()
	return &App{
		store:    store,
		prox:     proxy.New(proxy.DefaultPort, log),
		focus:    focus.New(store),
		log:      log,
		blocked:  f.BlockedToday,
		blockDay: f.BlockedDate,
		stopCh:   make(chan struct{}),
	}
}

func (a *App) startup(ctx context.Context) {
	a.ctx = ctx

	// Прошлый запуск мог не дожить до выхода — тогда системный прокси остался
	// направленным на порт, которого больше никто не слушает. Возвращаем
	// настройки человека прежде, чем трогать что-либо ещё.
	restoreStoredSystemProxy(a.store, a.log)

	// Прокси — единственный слой, который закрывает браузеры. Он поднимается
	// всегда, и политика прописывается ему сразу, а не на время сессии:
	// Chromium не перечитывает прокси-настройки на ходу, поэтому включать и
	// выключать политику на каждой сессии нельзя — см. комментарий к disengage.
	a.prox.SetBlockPage(a.renderBlockPage)
	if err := a.prox.Start(); err != nil {
		a.log.Warn("прокси недоступен, остаётся политика браузеров", "err", err)
	} else if err := a.prox.SelfTest(proxyTestURL, proxySelfTestTimeout); err != nil {
		// Прокси, который слушает порт, но не выходит в интернет, браузерам
		// прописывать нельзя: это оставит человека без единой страницы.
		a.log.Warn("прокси не проходит проверку, политика не прописана", "err", err)
	} else if err := sys.SetBrowserProxy(a.prox.Addr()); err != nil {
		a.log.Warn("не удалось прописать политику прокси", "err", err)
	} else {
		a.setProxyMode(proxyWatching)
		// Системный прокси — второй способ довести браузеры до прокси, для тех
		// браузеров, которые политику не читают.
		a.installSystemProxy()
		a.log.Info("браузеры направлены на прокси на всё время работы",
			"addr", a.prox.Addr())
	}

	// Возобновляем сессию, пережившую перезапуск, либо прибираем хвосты.
	if sess, active := a.focus.Active(time.Now()); active {
		if err := a.engage(sess); err != nil {
			a.setError(err)
		}
	} else {
		a.disengage()
	}

	// Виджет — это то же окно, только маленькое и поверх остальных. Режим
	// восстанавливается вместе с сессией: если человек его включил, он нужен и
	// после перезапуска.
	if a.store.Get().Settings.Widget {
		a.applyWidget(true)
	}

	go a.loop()
	a.emit()
}

func (a *App) shutdown(_ context.Context) {
	close(a.stopCh)

	// Всегда возвращаем систему в исходное состояние: иначе пользователь
	// останется без интернета после закрытия приложения.
	a.disengage()

	// Политику браузеров снимаем именно здесь, а не в disengage.
	//
	// Сессия кончается по нескольку раз в день, и на каждом её конце политику
	// оставлять нужно — браузер замечает удаление не сразу (см. disengage). Но
	// выход из приложения случается последний раз: оставленная политика
	// указывает на порт, которого после выхода уже не будет, и человек
	// остаётся без интернета до следующего запуска Focusd. Перезагрузка не
	// помогает — политика живёт в реестре. Именно это и случилось: после
	// закрытия приложения Edge отвечал ERR_PROXY_CONNECTION_FAILED на любом
	// сайте, потому что политика просила прокси 127.0.0.1:47653, а слушать его
	// было уже некому.
	//
	// Порядок важен: сначала снимаем политики, потом закрываем прокси. Порт
	// должен успеть побыть живым, пока браузер перечитывает настройки.
	if err := sys.ClearBrowserProxy(); err != nil {
		a.log.Warn("не удалось снять политику прокси при выходе", "err", err)
	}
	a.dropSystemProxy()
	a.persistWidgetPos()
	a.persistCounters()
	_ = a.prox.Stop()
}

// renderBlockPage отдаёт страницу-заглушку тому же прокси: HTTP-сайты из
// блокировки человек видит нашим объяснением, а не пустой ошибкой.
func (a *App) renderBlockPage(w http.ResponseWriter, r *http.Request, host string) {
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(http.StatusForbidden)
	_ = blockpage.Render(w, a.blockInfo(host))
}

// ---------- Язык ----------

// lang — язык, на котором приложение говорит прямо сейчас.
func (a *App) lang() i18n.Lang {
	return EffectiveLanguage(a.store.Get().Settings.Language)
}

// text переводит строку и подставляет аргументы. Неизвестный ключ возвращается
// как есть: так через те же поля можно передать готовый текст.
func (a *App) text(key string, args ...any) string {
	if key == "" {
		return ""
	}
	return i18n.T(a.lang(), key, args...)
}

// ---------- Состояние ----------

func (a *App) GetState() State { return a.snapshot() }

func (a *App) snapshot() State {
	now := time.Now()
	view := a.focus.Current(now)
	f := a.store.Get()

	groups, custom, allow := f.Settings.DefaultGroups, f.Settings.CustomDomains, f.Settings.Allowlist
	if view.Active {
		if sess, ok := a.focus.Active(now); ok {
			groups, custom, allow = sess.Groups, sess.CustomDomains, sess.Allowlist
		}
	}

	total := a.prox.RuleCount()
	if !view.Active {
		doms, _ := buildRuleset(groups, custom, config.Strict)
		total = rules.New(doms).Len()
	}

	a.mu.Lock()
	errText, blocked := a.errText, a.blocked
	notice, hint, hintArgs, lastBlocked := a.notice, a.hint, a.hintArgs, a.lastBlocked
	a.mu.Unlock()

	// Замечание и подсказка лежат ключами: их текст зависит от языка, а язык
	// мог смениться уже после того, как они появились.
	noticeText, hintText := a.text(notice), a.text(hint, hintArgs...)

	// Политика прокси прописана всегда, пока работает Focusd, поэтому признаком
	// работающей блокировки она быть перестала: сайты режет только то, что
	// внутри прокси, а это включает сессия. Запасной слой URLBlocklist живёт
	// ровно столько же, сколько сессия.
	blocking := a.proxyModeNow() == proxyBlocking
	browserBlock := blocking || (!blocking && sys.SitesBlockedInBrowsers())

	return State{
		Elevated:      sys.IsElevated(),
		ActiveGroups:  orEmpty(groups),
		CustomDomains: orEmpty(custom),
		Allowlist:     orEmpty(allow),
		BlockedToday:  blocked,
		TotalRules:    total,
		LastBlocked:   lastBlocked,
		Session:       view,
		DataDir:       a.store.Dir(),
		Version:       Version,
		LastError:     errText,
		BrowserBlock:  browserBlock,
		ProxyBlock:    a.prox.Running() && sys.BrowserProxyAddr() != "" && blocking,
		Hint:          hintText,
		Notice:        noticeText,
		Theme:         string(f.Settings.Theme),
		ResolvedTheme: EffectiveTheme(f.Settings.Theme),
		Language:         string(f.Settings.Language),
		ResolvedLanguage: string(EffectiveLanguage(f.Settings.Language)),
		Widget:        f.Settings.Widget,
		WidgetOnTop:   f.Settings.WidgetOnTop,
		WidgetShape:   string(a.currentShape()),
	}
}

// ---------- Форма виджета ----------

// widgetGeometry возвращает размеры окна для формы.
func widgetGeometry(shape config.WidgetShape) (int, int) {
	if shape == config.WidgetRing {
		return ringWidth, ringHeight
	}
	return barWidth, barHeight
}

// currentShape — форма, которую нужно показать прямо сейчас. Компактный режим
// включается и выключается целиком, поэтому форма — это ровно то, что записано
// в настройках.
func (a *App) currentShape() config.WidgetShape {
	sh := a.store.Get().Settings.WidgetShape
	if !sh.Valid() {
		return config.WidgetBar
	}
	return sh
}

// SetWidgetShape сохраняет выбранную форму компактного таймера. Отдельным
// вызовом, как и тема: смена формы — не повод переприменять блокировку.
func (a *App) SetWidgetShape(shape string) error {
	sh := config.WidgetShape(shape)
	if !sh.Valid() {
		sh = config.WidgetBar
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.WidgetShape = sh
		return nil
	}); err != nil {
		return err
	}
	if a.store.Get().Settings.Widget {
		a.resizeWidget()
	}
	a.emit()
	return nil
}

// resizeWidget подгоняет окно под выбранную форму, не трогая положение:
// человек уже мог унести виджет в удобное место.
func (a *App) resizeWidget() {
	if a.ctx == nil || !a.store.Get().Settings.Widget {
		return
	}
	w, h := widgetGeometry(a.currentShape())
	appRuntime.WindowSetMinSize(a.ctx, w, h)
	appRuntime.WindowSetSize(a.ctx, w, h)
}

func (a *App) blockInfo(host string) blockpage.Info {
	now := time.Now()
	view := a.focus.Current(now)
	a.mu.Lock()
	blocked := a.blocked
	a.mu.Unlock()
	return blockpage.Info{
		Active:     view.Active,
		Host:       host,
		Remaining:  view.Remaining,
		Strictness: string(view.Strictness),
		Blocked:    blocked,
		Lang:       a.lang(),
	}
}

func (a *App) emit() {
	if a.ctx == nil {
		return
	}
	appRuntime.EventsEmit(a.ctx, "state", a.snapshot())
}

func (a *App) setError(err error) {
	if err == nil {
		return
	}
	a.mu.Lock()
	a.errText = err.Error()
	a.mu.Unlock()
	a.log.Warn("ошибка", "err", err)
}

func (a *App) clearError() {
	a.mu.Lock()
	a.errText = ""
	a.mu.Unlock()
}

// setNotice запоминает неполадку, которая не мешает блокировке. Показывать её
// красной ошибкой нельзя: человек решит, что ничего не работает, хотя защита
// действует другим слоем.
func (a *App) setNotice(text string) {
	a.mu.Lock()
	a.notice = text
	a.mu.Unlock()
	if text != "" {
		a.log.Info("замечание", "text", text)
	}
}

func (a *App) loop() {
	ticker := time.NewTicker(time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-a.stopCh:
			return
		case now := <-ticker.C:
			a.rollDay(now)
			a.syncCounters()
			a.trackWidgetPos()
			a.proxyWatchdog()
			a.keepSystemProxy()

			_, active := a.focus.Active(now)
			a.mu.Lock()
			engaged := a.engaged
			a.mu.Unlock()

			switch {
			case active:
				a.emit()
			case engaged:
				// Сессия истекла сама — снимаем блокировку.
				a.disengage()
				a.emit()
			}
		}
	}
}

func (a *App) rollDay(now time.Time) {
	today := now.Format("2006-01-02")
	a.mu.Lock()
	if a.blockDay != today {
		a.blockDay = today
		a.blocked = 0
	}
	a.mu.Unlock()
}

// syncCounters переносит прирост счётчика прокси в суточный счётчик.
//
// Считает только прокси, и это единственный честный источник: он стоит на пути
// трафика браузеров. Не-браузерные приложения не считаются вовсе — их закрывал
// DNS-слой, который убран.
func (a *App) syncCounters() {
	_, proxyBlocked := a.prox.Stats()

	a.mu.Lock()
	defer a.mu.Unlock()

	delta := counterDelta(proxyBlocked, &a.lastProxyTotal)
	a.blocked += delta
	if delta > 0 {
		if h := a.prox.LastBlocked(); h != "" {
			a.lastBlocked = h
		}
	}
}

// counterDelta возвращает прирост счётчика с прошлого раза. Значение меньше
// сохранённого означает, что слой перезапускался: тогда отсчёт начинается
// заново, иначе к суточному счётчику однажды прибавилось бы чужое число.
func counterDelta(total int64, prev *int64) int64 {
	if total < *prev {
		*prev = total
		return 0
	}
	delta := total - *prev
	*prev = total
	return delta
}

func (a *App) persistCounters() {
	a.mu.Lock()
	blocked, day := a.blocked, a.blockDay
	a.mu.Unlock()
	_ = a.store.Update(func(f *config.File) error {
		f.BlockedToday = blocked
		f.BlockedDate = day
		return nil
	})
}

// ---------- Управление сессией ----------

// StartSession запускает сессию фокусировки и включает блокировку.
func (a *App) StartSession(o StartOptions) error {
	a.clearError()

	if !sys.IsElevated() {
		return errors.New("нужны права администратора: перезапустите Focusd от имени администратора")
	}

	sess := config.Session{
		Strictness:    config.Strictness(o.Strictness),
		Groups:        o.Groups,
		CustomDomains: o.CustomDomains,
		Allowlist:     o.Allowlist,
		Label:         o.Label,
	}
	if err := a.focus.Start(focus.Options{
		DurationMin:   o.DurationMin,
		Strictness:    sess.Strictness,
		Groups:        sess.Groups,
		CustomDomains: sess.CustomDomains,
		Allowlist:     sess.Allowlist,
		Label:         sess.Label,
	}, time.Now()); err != nil {
		return err
	}

	active, ok := a.focus.Active(time.Now())
	if !ok {
		return errors.New("сессия не сохранилась")
	}
	if err := a.engage(active); err != nil {
		a.setError(err)
	}
	a.emit()
	return nil
}

// RequestCancel сообщает, можно ли прервать сессию и сколько ждать.
func (a *App) RequestCancel() (CancelInfo, error) {
	wait, err := a.focus.RequestCancel(time.Now())
	if err != nil {
		return CancelInfo{}, err
	}
	if wait > 0 {
		return CancelInfo{
			WaitSec: wait,
			Message: a.text("app.cancel.wait", wait),
		}, nil
	}
	return CancelInfo{Message: a.text("app.cancel.confirm")}, nil
}

// ConfirmCancel прерывает сессию, если уровень строгости это позволяет.
func (a *App) ConfirmCancel() error {
	if err := a.focus.ConfirmCancel(time.Now()); err != nil {
		return err
	}
	a.disengage()
	a.emit()
	return nil
}

// EmergencyRestore — аварийное снятие блокировки: снимает сессию независимо от
// строгости и возвращает браузеры в обычное состояние.
func (a *App) EmergencyRestore() error {
	if err := a.focus.ForceComplete(); err != nil {
		a.setError(err)
	}
	a.disengage()
	a.emit()
	return nil
}

// ---------- Блокировка ----------

// engage включает блокировку для активной сессии.
//
// Слоёв два, и они намеренно независимы. Прокси — основной: он отсекает запросы
// браузеров и считает их. URLBlocklist — запасной, включается только если прокси
// поднять или прописать не удалось.
func (a *App) engage(sess *config.Session) error {
	domains, _ := buildRuleset(sess.Groups, sess.CustomDomains, sess.Strictness)
	a.prox.SetRules(domains)
	a.prox.SetAllowlist(sess.Allowlist)
	a.prox.SetEnabled(true)

	var errs []error

	// Основной слой: браузеры ходят через локальный прокси.
	fallback, err := a.engageProxy(domains, sess.Allowlist)
	if err != nil {
		errs = append(errs, err)
	}

	// Подсказка про перезапуск браузера нужна только запасному слою: политику
	// прокси Chromium подхватывает на ходу, и советовать перезапуск там, где он
	// ничего не меняет, — значит зря гонять человека закрывать окна.
	a.mu.Lock()
	if browsers := sys.RunningBrowsers(a.ctx); fallback && len(browsers) > 0 {
		a.hint = "app.hint.browsers"
		a.hintArgs = []any{strings.Join(browsers, ", ")}
	} else {
		a.hint, a.hintArgs = "", nil
	}
	a.mu.Unlock()

	a.mu.Lock()
	a.engaged = true
	a.mu.Unlock()

	a.syncGuardTask()

	a.log.Info("блокировка включена",
		"rules", a.prox.RuleCount(), "strictness", sess.Strictness, "groups", sess.Groups)
	return errors.Join(errs...)
}

// engageProxy направляет браузеры через локальный прокси.
//
// Если прокси не поднялся, не проходит проверку на живом запросе или политику не
// удалось записать, включается запасной слой — политика URLBlocklist. Она
// обрывает запрос внутри браузера и потому надёжнее, но снаружи ненаблюдаема:
// сколько запросов отсечено, узнать уже нельзя. Поэтому она используется только
// как страховка.
//
// Проверка на живом запросе обязательна именно здесь. Прокси, который слушает
// порт, но не может выйти в интернет, после записи политики оставит человека без
// единой открывающейся страницы — и виноват в этом будет Focusd. Дешевле
// отказаться от слоя заранее, чем объяснять потом, почему браузер не работает.
//
// Возвращает признак того, что сработала именно страховка: от этого зависит,
// нужна ли подсказка про перезапуск браузера.
func (a *App) engageProxy(domains, allow []string) (fallback bool, err error) {
	addr := a.prox.Addr()
	if addr == "" {
		a.engageProxyCheck(false)
		return true, a.fallbackBrowserBlock(domains, allow)
	}

	// Проверка нужна один раз — перед тем, как политика попадёт браузерам.
	// Если прокси уже прописан и уже водил трафик, повторять её незачем, а
	// стоит она до восьми секунд (proxySelfTestTimeout): изменения правил
	// внутри сессии и «Сохранить» в настройках ждали ответа сети и выглядели
	// зависшими. За тем, что прокси жив, следит сторож — proxyWatchdog.
	if a.proxyModeNow() != proxyBlocking {
		if err := a.prox.SelfTest(proxyTestURL, proxySelfTestTimeout); err != nil {
			a.engageProxyCheck(false)
			a.log.Warn("прокси не проходит проверку — включаю политику браузеров", "err", err)
			return true, a.fallbackBrowserBlock(domains, allow)
		}
		a.engageProxyCheck(true)
	}

	if err := sys.SetBrowserProxy(addr); err != nil {
		return true, errors.Join(err, a.fallbackBrowserBlock(domains, allow))
	}
	// Системный прокси — не запасной слой, а тот же основной, только для
	// браузеров, которые политику не читают: заводим его вместе с политикой.
	a.installSystemProxy()
	// Списки URLBlocklist снимаем: с ними браузер отсекал бы запрос раньше, чем
	// его увидит прокси, и счётчик снова стал бы нулевым.
	if err := sys.UnblockSitesInBrowsers(); err != nil {
		return false, err
	}

	// Точка отсчёта для контроля здоровья: дальше следим, что через прокси
	// действительно что-то открывается, а не только отсекается.
	forwarded, failed := a.prox.Health()
	a.mu.Lock()
	a.proxyBaseForwarded, a.proxyBaseFailed = forwarded, failed
	a.proxyStalled = 0
	a.mu.Unlock()

	a.log.Info("браузеры направлены на прокси", "addr", addr)
	return false, nil
}

// Режимы прокси как слоя блокировки.
const (
	// proxyOff — политика браузерам не прописана: работаем запасным слоем.
	proxyOff = iota
	// proxyWatching — политика прописана, но сессии нет: прокси пропускает всё.
	proxyWatching
	// proxyBlocking — политика прописана, сессия идёт: прокси режет.
	proxyBlocking
)

// keepProxyPolicy оставляет политику прокси прописанной браузерам после конца
// сессии, переводя сам прокси в режим сквозного пропуска.
//
// Политика не меняется — значит, браузеру нечего перечитывать, и доступ
// возвращается в тот же миг, без перезапуска окна. Ставить её заново нужно
// только если её сняли извне: например, сторож или человек через
// «Аварийно снять блокировку».
func (a *App) keepProxyPolicy() error {
	addr := a.prox.Addr()
	if addr == "" || !a.prox.Running() {
		// Прокси не слушает: политике на него браузеры не место. Системный
		// прокси убираем по той же причине — он увёл бы в мёртвый порт не
		// только браузеры, но и остальные программы.
		if err := sys.ClearBrowserProxy(); err != nil {
			return err
		}
		a.dropSystemProxy()
		a.setProxyMode(proxyOff)
		return nil
	}

	if cur := sys.BrowserProxyAddr(); cur != addr {
		if err := sys.SetBrowserProxy(addr); err != nil {
			a.setProxyMode(proxyOff)
			return err
		}
	}
	a.setProxyMode(proxyWatching)
	return nil
}

// keepSystemProxy возвращает системный прокси на Focusd, если его увели в
// сторону.
//
// Увести его может человек — тогда блокировка тихо перестала бы действовать для
// браузеров, которые политику не читают (Яндекс Браузер), — или страховка,
// успевшая сработать в тот же миг, что и запуск приложения. Проверка дешёвая:
// одно чтение реестра, а пишем только когда настройка действительно чужая.
func (a *App) keepSystemProxy() {
	if a.proxyModeNow() == proxyOff || !a.prox.Running() {
		return
	}
	if sys.SystemProxyAddr() == a.prox.Addr() {
		return
	}
	a.installSystemProxy()
}

func (a *App) setProxyMode(mode int) {
	a.mu.Lock()
	a.proxyMode = mode
	a.mu.Unlock()
}

func (a *App) proxyModeNow() int {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.proxyMode
}

// ---------- Системный прокси ----------

// installSystemProxy направляет системный прокси на Focusd и запоминает прежние
// настройки, чтобы их можно было вернуть.
//
// Это второй способ направить браузеры в прокси, и нужен он потому, что
// политику читают не все. Яндекс Браузер кладёт её по своему
// задокументированному пути HKLM\SOFTWARE\Policies\YandexBrowser и всё равно
// ходит напрямую — проверено живой пробой tools/policyprobe.go. Системный
// прокси он читает и, что важнее, подхватывает на ходу: у уже запущенного окна
// соединения пошли через Focusd без перезапуска.
func (a *App) installSystemProxy() {
	if a.ctx == nil {
		return
	}
	addr := a.prox.Addr()
	if addr == "" {
		return
	}

	backup := a.store.Get().SystemProxy
	prev, err := sys.SetSystemProxy(addr)
	if err != nil {
		a.log.Warn("не удалось прописать системный прокси", "err", err)
		return
	}
	if backup == nil {
		// Снимок делается один раз. Со второго раза мы сохранили бы свои же
		// настройки и потеряли настройки человека — вернуть было бы нечего.
		snapshot := config.SystemProxyBackup{
			Enable:        prev.Enable,
			Server:        prev.Server,
			AutoConfigURL: prev.AutoConfigURL,
		}
		if err := a.store.Update(func(f *config.File) error {
			f.SystemProxy = &snapshot
			return nil
		}); err != nil {
			// Без сохранённого снимка вернуть настройки будет нечем, поэтому
			// подмену отменяем сразу: оставить её навсегда нельзя.
			_ = sys.RestoreSystemProxy(prev)
			a.log.Warn("прежние настройки прокси не сохранились, подмена отменена", "err", err)
			return
		}
	}
	a.log.Info("системный прокси направлен на Focusd", "addr", addr)
	a.keepProxyGuard(true)
}

// dropSystemProxy возвращает человеку его настройки системного прокси.
func (a *App) dropSystemProxy() {
	a.keepProxyGuard(false)
	restoreStoredSystemProxy(a.store, a.log)
}

// keepProxyGuard ставит и снимает страховку от неработающего прокси.
//
// Без неё аварийный конец приложения оставляет в реестре настройки, которые
// указывают на порт, которого больше нет: браузеры отвечают
// ERR_PROXY_CONNECTION_FAILED на любом сайте, и перезагрузка не помогает —
// настройки переживают её. Задачи планировщика запускают `--proxyguard`, пока
// приложение не работает (см. main.go).
func (a *App) keepProxyGuard(enable bool) {
	a.mu.Lock()
	changed := a.proxyGuard != enable
	a.proxyGuard = enable
	a.mu.Unlock()
	if !changed {
		return
	}
	exe, err := sys.ExecutablePath()
	if err != nil {
		return
	}
	if err := sys.SetProxyGuard(context.Background(), enable, exe); err != nil {
		a.log.Warn("не удалось настроить страховку прокси", "err", err)
	}
}

// restoreStoredSystemProxy возвращает настройки, снятые прошлым запуском Focusd.
//
// Отдельная функция, а не метод: она нужна ещё сторожу и режиму --restore —
// это другие процессы, и приложения в памяти у них уже нет. Снимок поэтому и
// лежит на диске.
func restoreStoredSystemProxy(store *config.Store, log *slog.Logger) {
	backup := store.Get().SystemProxy
	if backup == nil {
		// Снимка нет, а системный прокси всё же смотрит в порт Focusd: значит,
		// подмена осталась без записи о себе — файл настроек потеряли или
		// приложение убили между подменой и сохранением снимка. Возвращать
		// нечего, но включённый прокси на мёртвый порт оставил бы человека без
		// интернета вовсе, поэтому просто выключаем его.
		if sys.SystemProxyAddr() == proxy.DefaultAddr() {
			if err := sys.DisableSystemProxy(); err != nil {
				log.Warn("не удалось выключить системный прокси", "err", err)
				return
			}
			log.Info("системный прокси вёл в порт Focusd без снимка настроек — выключен")
		}
		return
	}
	err := sys.RestoreSystemProxy(winsys.SystemProxySettings{
		Enable:        backup.Enable,
		Server:        backup.Server,
		AutoConfigURL: backup.AutoConfigURL,
	})
	if err != nil {
		log.Warn("не удалось вернуть прежние настройки системного прокси", "err", err)
		return
	}
	if err := store.Update(func(f *config.File) error {
		f.SystemProxy = nil
		return nil
	}); err != nil {
		log.Warn("не удалось снять отметку о подмене прокси", "err", err)
	}
	log.Info("прежние настройки системного прокси возвращены")
}

// proxySelfTestTimeout — сколько ждать ответа при проверке прокси на живом
// запросе. Сам адрес задан в platform.go: там он подменяется в тестах.
const proxySelfTestTimeout = 8 * time.Second

// engageProxyCheck запоминает, включён ли слой прокси: от этого зависит, нужно
// ли следить за его здоровьем.
func (a *App) engageProxyCheck(on bool) {
	if !on {
		a.setProxyMode(proxyOff)
		return
	}
	a.setProxyMode(proxyBlocking)
}

// proxyWatchdog следит за тем, что через прокси действительно открываются
// страницы.
//
// Зачем это нужно. Если политика уже прописана, а прокси по любой причине
// перестал водить трафик наружу, браузер остаётся без единой открывающейся
// страницы. Снять политику в этот момент нельзя: Chromium заметит это только
// со следующим опросом, а до тех пор он будет ходить в никуда — то есть станет
// только хуже. Поэтому сначала пробуем починить сам прокси, и лишь если он не
// поднимается, снимаем политику и уходим на запасной слой.
func (a *App) proxyWatchdog() {
	if a.proxyModeNow() == proxyOff {
		return
	}
	a.mu.Lock()
	baseF, baseX := a.proxyBaseForwarded, a.proxyBaseFailed
	a.mu.Unlock()

	forwarded, failed := a.prox.Health()
	/* Если за время сессии не доехал ни один запрос, а неудачные попытки есть —
	   прокси сломан. Порог в несколько попыток нужен, чтобы не сработать на
	   единственном случайном сбое: браузер делает запросы десятками. */
	if forwarded != baseF || failed < baseX+3 {
		a.mu.Lock()
		a.proxyStalled = 0
		a.mu.Unlock()
		return
	}

	a.mu.Lock()
	a.proxyStalled++
	stalled := a.proxyStalled
	a.mu.Unlock()
	if stalled < 2 {
		return
	}

	a.log.Warn("через прокси ничего не открывается", "forwarded", forwarded, "failed", failed)

	// Первая попытка — вернуть к жизни сам прокси: если слушающий сокет потерян,
	// этого достаточно, и браузеру не придётся ничего перечитывать.
	if !a.prox.Running() {
		if err := a.prox.Start(); err == nil {
			a.log.Info("прокси поднят заново", "addr", a.prox.Addr())
			// Системный прокси мог быть снят, пока прокси лежал: возвращаем.
			a.installSystemProxy()
			a.mu.Lock()
			a.proxyStalled = 0
			a.mu.Unlock()
			return
		} else {
			a.log.Warn("не удалось поднять прокси заново", "err", err)
		}
	}

	// Прокси жив, но не водит трафик, и починить его нечем. Только тогда
	// снимаем политику: это вернёт доступ не сразу, но браузер нельзя оставить
	// и без блокировки тоже.
	if err := sys.ClearBrowserProxy(); err != nil {
		a.log.Warn("не удалось снять политику прокси", "err", err)
	}
	a.dropSystemProxy()
	a.setProxyMode(proxyOff)

	sess, active := a.focus.Active(time.Now())
	if active {
		domains, _ := buildRuleset(sess.Groups, sess.CustomDomains, sess.Strictness)
		if err := a.fallbackBrowserBlock(domains, sess.Allowlist); err != nil {
			a.setError(err)
		}
	}
	a.setNotice("app.notice.proxyStalled")
	a.emit()
}

func (a *App) fallbackBrowserBlock(domains, allow []string) error {
	if err := sys.BlockSitesInBrowsers(domains, allow); err != nil {
		return fmt.Errorf("блокировка сайтов в браузерах: %w", err)
	}
	a.log.Info("прокси недоступен, включена политика браузеров")
	return nil
}

// disengage снимает блокировку и возвращает систему в норму.
//
// Политику прокси здесь НЕ снимаем — и это главное решение всего слоя.
//
// Раньше политика жила ровно столько, сколько длилась сессия. Оказалось, что
// Chromium не перечитывает прокси на ходу: удаление политики из реестра он
// замечает только со следующим опросом, а это до десяти минут. Всё это время
// браузер продолжает ходить в порт, который уже никто не слушает, и человек
// видит ERR_TUNNEL_CONNECTION_FAILED на любом сайте — то есть выключение фокуса
// выглядит как поломка интернета. Лечится это только перезапуском браузера, а
// просить человека перезапускать браузер после каждой сессии нельзя.
//
// Поэтому прокси прописан браузерам всё время, пока работает Focusd, а режим
// переключается внутри него: во время сессии он режет, вне сессии прозрачно
// пропускает всё. Политика при этом не меняется ни на байт, и браузеру нечего
// перечитывать. Совсем снимается она только при выходе из приложения.
func (a *App) disengage() {
	// Режим запоминается до сброса: если слой прокси так и не заработал —
	// например, не прошёл проверку на старте, — политику ему прописывать
	// нельзя. Иначе конец сессии вернул бы в реестр ровно то, от чего отказались
	// намеренно, и браузеры ушли бы в прокси, который не ходит в сеть.
	proxyWasOn := a.proxyModeNow() != proxyOff

	a.prox.SetEnabled(false)
	a.engageProxyCheck(false)

	a.mu.Lock()
	wasEngaged := a.engaged
	a.engaged = false
	a.hint = ""
	a.mu.Unlock()

	// Политику прокси оставляем на месте: она переживёт конец сессии и снимет
	// её только закрытие приложения. Подробности — в комментарии к disengage.
	if proxyWasOn {
		if err := a.keepProxyPolicy(); err != nil {
			a.log.Warn("не удалось оставить политику прокси", "err", err)
		}
	}

	if wasEngaged {
		a.persistCounters()
		a.log.Info("блокировка выключена")
	}

	// Политики браузеров снимаем всегда: они запасной слой, и оставить их
	// висеть после сессии — значит блокировать сайты без всякого срока.
	if err := sys.UnblockSitesInBrowsers(); err != nil {
		a.log.Warn("не удалось снять политики браузеров", "err", err)
	}

	// Сторож снимается только тогда, когда охранять больше нечего: закрытие
	// приложения во время жёсткой сессии не должно её прерывать.
	a.syncGuardTask()
}

// syncGuardTask приводит сторожевую задачу планировщика в соответствие с
// текущим состоянием. Жёсткая сессия охраняется всегда: иначе её смысл
// теряется — достаточно было бы закрыть приложение.
func (a *App) syncGuardTask() {
	// Без прав администратора задачи планировщика недоступны — не шумим.
	if !sys.IsElevated() {
		return
	}
	exe, err := sys.ExecutablePath()
	if err != nil {
		return
	}
	sess, active := a.focus.Active(time.Now())
	need := active && sess.Strictness == config.Lock
	if err := sys.SetGuard(a.ctx, need, exe); err != nil {
		a.log.Warn("не удалось обновить сторожевую задачу", "err", err)
	}
}

// buildRuleset собирает домены для блокировки и список исключений.
func buildRuleset(groups, custom []string, strictness config.Strictness) (domains, allow []string) {
	domains = catalog.GroupsByID(groups)
	domains = append(domains, custom...)
	if strictness != config.Soft {
		domains = append(domains, catalog.DoH()...)
	}
	return domains, nil
}

// ---------- Каталог и настройки ----------

// GetCatalog возвращает доступные группы блокировки.
// GetCatalog отдаёт каталог групп на текущем языке. Переводятся только подписи:
// домены и идентификаторы групп от языка не зависят и участвуют в правилах
// блокировки, поэтому остаются как есть.
func (a *App) GetCatalog() []catalog.Group {
	lang := a.lang()
	groups := catalog.All()
	for i := range groups {
		groups[i].Title = i18n.GroupTitle(lang, groups[i].ID, groups[i].Title)
		groups[i].Note = i18n.GroupNote(lang, groups[i].ID, groups[i].Note)
	}
	return groups
}

// GetSettings возвращает текущие настройки.
//
// Форма виджета отдаётся действующая, а не та, что лежит на диске: пока
// компактный режим не включён, окно остаётся обычным, и панель настроек должна
// показывать именно то, что человек увидит.
func (a *App) GetSettings() config.Settings {
	s := a.store.Get().Settings
	s.WidgetShape = a.currentShape()
	return s
}

// SaveSettings сохраняет настройки и применяет системные изменения.
func (a *App) SaveSettings(s config.Settings) error {
	a.clearError()

	s.CustomDomains = cleanDomains(s.CustomDomains)
	s.Allowlist = cleanDomains(s.Allowlist)
	if s.DefaultDurationMin <= 0 {
		s.DefaultDurationMin = 25
	}
	if !s.DefaultStrictness.Valid() {
		s.DefaultStrictness = config.Strict
	}
	if !s.Theme.Valid() {
		s.Theme = config.ThemeSystem
	}
	if !s.WidgetShape.Valid() {
		s.WidgetShape = config.WidgetBar
	}
	// Положение и режим виджета живут вне панели настроек: туда приезжает
	// снимок, снятый до сохранения, и затирать ими уже унесённый виджет нельзя.
	cur := a.store.Get().Settings
	s.Widget = cur.Widget
	s.WidgetOnTop = cur.WidgetOnTop
	s.WidgetX = cur.WidgetX
	s.WidgetY = cur.WidgetY

	if err := a.store.Update(func(f *config.File) error {
		f.Settings = s
		return nil
	}); err != nil {
		return err
	}

	// Форма виджета меняется на месте: ждать «Сохранить» и перезапуска ради
	// квадрата вместо плашки незачем.
	if a.ctx != nil && a.store.Get().Settings.Widget {
		a.resizeWidget()
	}

	// Задача планировщика переставляется только тогда, когда переключатель и
	// правда переключили. Раньше schtasks запускался на каждое «Сохранить» —
	// человек, пришедший поменять длительность сессии, ждал возни с задачами
	// планировщика и решал, что кнопка зависла.
	if s.Autostart != cur.Autostart {
		if exe, err := sys.ExecutablePath(); err == nil {
			if err := sys.SetAutostart(a.ctx, s.Autostart, exe); err != nil {
				a.setError(fmt.Errorf("автозапуск: %w", err))
			}
		}
	}

	// Если сессия идёт, новые правила применяются сразу.
	if sess, active := a.focus.Active(time.Now()); active {
		sess.CustomDomains = mergeUnique(sess.CustomDomains, s.CustomDomains)
		sess.Allowlist = s.Allowlist
		if err := a.engage(sess); err != nil {
			a.setError(err)
		}
	}
	a.emit()
	return nil
}

// ---------- Виджет и оформление ----------

// SetWidget включает и выключает компактный таймер поверх всех окон.
//
// Компактный режим — это то же окно приложения, уменьшенное до размеров
// виджета: Wails v2 не умеет открывать второе окно, а поднимать ради таймера
// отдельный процесс было бы издевательством над пользователем. Окно без рамки,
// поэтому таскать его можно мышью за любую свободную точку — этим занимается
// разметка, а не система.
func (a *App) SetWidget(on bool) error {
	f := a.store.Get()
	if f.Settings.Widget == on {
		return nil
	}

	if on {
		// Положение берём из прошлого раза; нули означают «человек ещё не
		// двигал виджет» — тогда ставим его в правый верхний угол.
		a.mu.Lock()
		a.widgetX, a.widgetY = f.Settings.WidgetX, f.Settings.WidgetY
		a.mu.Unlock()
	} else {
		a.persistWidgetPos()
	}

	if err := a.store.Update(func(x *config.File) error {
		x.Settings.Widget = on
		return nil
	}); err != nil {
		return err
	}

	a.applyWidget(on)
	a.emit()
	return nil
}

// SetTheme сохраняет выбранное оформление. Отдельным вызовом, а не через
// SaveSettings: переключение темы не должно ничего переприменять и уж тем более
// перезапускать блокировку.
func (a *App) SetTheme(theme string) error {
	t := config.Theme(theme)
	if !t.Valid() {
		t = config.ThemeSystem
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Theme = t
		return nil
	}); err != nil {
		return err
	}
	if a.ctx != nil {
		// Подложка окна видна при изменении размера, пока WebView не
		// перерисовался, поэтому она обязана следовать за темой.
		c := windowColourLight
		if EffectiveTheme(t) == "dark" {
			c = windowColourDark
		}
		appRuntime.WindowSetBackgroundColour(a.ctx, c.R, c.G, c.B, c.A)
	}
	a.emit()
	return nil
}

// SetLanguage сохраняет выбранный язык. Отдельным вызовом, по той же причине,
// что и тема: смена языка — не повод переприменять блокировку. Каталог групп,
// подсказка и замечание приедут в интерфейс уже переведёнными вместе с новым
// состоянием.
func (a *App) SetLanguage(lang string) error {
	l := config.Language(lang)
	if !l.Valid() {
		l = config.LanguageAuto
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Language = l
		return nil
	}); err != nil {
		return err
	}
	a.emit()
	return nil
}

// SetWidgetOnTop включает и выключает «поверх всех окон» для виджета.
func (a *App) SetWidgetOnTop(on bool) error {
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.WidgetOnTop = on
		return nil
	}); err != nil {
		return err
	}
	if a.ctx != nil && a.store.Get().Settings.Widget {
		appRuntime.WindowSetAlwaysOnTop(a.ctx, on)
	}
	a.emit()
	return nil
}

// applyWidget приводит окно к выбранному режиму.
func (a *App) applyWidget(on bool) {
	if a.ctx == nil {
		return
	}
	if !on {
		appRuntime.WindowSetAlwaysOnTop(a.ctx, false)
		appRuntime.WindowSetSize(a.ctx, mainWidth, mainHeight)
		appRuntime.WindowSetMinSize(a.ctx, minWidth, minHeight)
		appRuntime.WindowCenter(a.ctx)
		return
	}

	w, h := widgetGeometry(a.currentShape())

	// Минимальный размер опускаем до размера виджета: иначе окно просто не
	// станет меньше минимума обычного режима и «виджет» окажется во весь экран.
	appRuntime.WindowSetMinSize(a.ctx, w, h)
	appRuntime.WindowSetSize(a.ctx, w, h)
	appRuntime.WindowSetAlwaysOnTop(a.ctx, a.store.Get().Settings.WidgetOnTop)

	a.mu.Lock()
	x, y := a.widgetX, a.widgetY
	a.mu.Unlock()
	if x == 0 && y == 0 {
		// Никто виджет ещё не двигал. Ставим в угол, но не сохраняем это
		// положение: иначе «угол по умолчанию» навсегда останется в конфиге
		// и перестанет следовать за разрешением экрана.
		x, y = defaultWidgetPos(a.ctx, w)
	}
	appRuntime.WindowSetPosition(a.ctx, x, y)
}

// defaultWidgetPos ставит виджет в правый верхний угол основного экрана —
// единственное место, где он не закрывает ни заголовок окна, ни панель задач.
func defaultWidgetPos(ctx context.Context, widgetW int) (int, int) {
	screens, err := appRuntime.ScreenGetAll(ctx)
	if err != nil {
		return 600, widgetMargin
	}
	for _, s := range screens {
		if !s.IsPrimary && !s.IsCurrent {
			continue
		}
		w := s.Size.Width
		if w == 0 {
			w = s.Width
		}
		if w > widgetW {
			return w - widgetW - widgetMargin, widgetMargin
		}
	}
	return 600, widgetMargin
}

// trackWidgetPos запоминает положение виджета в памяти. На диск оно попадает
// при выходе из режима и при закрытии приложения: писать конфигурацию на каждое
// движение мышью незачем.
func (a *App) trackWidgetPos() {
	if a.ctx == nil || !a.store.Get().Settings.Widget {
		return
	}
	x, y := appRuntime.WindowGetPosition(a.ctx)
	a.mu.Lock()
	a.widgetX, a.widgetY = x, y
	a.mu.Unlock()
}

func (a *App) persistWidgetPos() {
	a.mu.Lock()
	x, y := a.widgetX, a.widgetY
	a.mu.Unlock()
	if x == 0 && y == 0 {
		return
	}
	_ = a.store.Update(func(f *config.File) error {
		f.Settings.WidgetX = x
		f.Settings.WidgetY = y
		return nil
	})
}

// AddCustomDomain добавляет домен в блокировку, в том числе во время сессии.
func (a *App) AddCustomDomain(domain string) error {
	d := rules.Normalize(domain)
	if d == "" {
		return fmt.Errorf("не похоже на домен: %q", domain)
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.CustomDomains = mergeUnique(f.Settings.CustomDomains, []string{d})
		return nil
	}); err != nil {
		return err
	}
	return a.reapplyIfActive()
}

// RemoveCustomDomain убирает домен из блокировки.
func (a *App) RemoveCustomDomain(domain string) error {
	d := rules.Normalize(domain)
	return a.store.Update(func(f *config.File) error {
		f.Settings.CustomDomains = removeString(f.Settings.CustomDomains, d)
		if f.Session != nil {
			f.Session.CustomDomains = removeString(f.Session.CustomDomains, d)
		}
		return nil
	})
}

// AddAllowlistDomain добавляет домен в исключения.
func (a *App) AddAllowlistDomain(domain string) error {
	d := rules.Normalize(domain)
	if d == "" {
		return fmt.Errorf("не похоже на домен: %q", domain)
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Allowlist = mergeUnique(f.Settings.Allowlist, []string{d})
		return nil
	}); err != nil {
		return err
	}
	return a.reapplyIfActive()
}

// RemoveAllowlistDomain убирает домен из исключений.
func (a *App) RemoveAllowlistDomain(domain string) error {
	d := rules.Normalize(domain)
	return a.store.Update(func(f *config.File) error {
		f.Settings.Allowlist = removeString(f.Settings.Allowlist, d)
		return nil
	})
}

func (a *App) reapplyIfActive() error {
	sess, active := a.focus.Active(time.Now())
	if !active {
		a.emit()
		return nil
	}
	f := a.store.Get()
	sess.CustomDomains = f.Settings.CustomDomains
	sess.Allowlist = f.Settings.Allowlist
	if err := a.engage(sess); err != nil {
		a.setError(err)
	}
	a.emit()
	return nil
}

// OpenDataDir открывает папку с конфигурацией в проводнике.
func (a *App) OpenDataDir() error {
	return execCommand("explorer", a.store.Dir()).Start()
}

// TestDomain сообщает, заблокирован ли домен при текущих правилах.
func (a *App) TestDomain(domain string) bool {
	d := rules.Normalize(domain)
	if d == "" {
		return false
	}
	now := time.Now()
	groups, custom, allow := a.store.Get().Settings.DefaultGroups, a.store.Get().Settings.CustomDomains, a.store.Get().Settings.Allowlist
	strict := config.Strict
	if sess, ok := a.focus.Active(now); ok {
		groups, custom, allow, strict = sess.Groups, sess.CustomDomains, sess.Allowlist, sess.Strictness
	}
	if rules.New(allow).Match(d) {
		return false
	}
	domains, _ := buildRuleset(groups, custom, strict)
	return rules.New(domains).Match(d)
}

// ---------- Утилиты ----------

// orEmpty гарантирует, что в JSON уйдёт [], а не null: интерфейс ожидает массивы.
func orEmpty(in []string) []string {
	if in == nil {
		return []string{}
	}
	return in
}

func cleanDomains(in []string) []string {
	out := make([]string, 0, len(in))
	seen := make(map[string]bool, len(in))
	for _, d := range in {
		n := rules.Normalize(d)
		if n == "" || seen[n] {
			continue
		}
		seen[n] = true
		out = append(out, n)
	}
	return out
}

func mergeUnique(dst, src []string) []string {
	seen := make(map[string]bool, len(dst)+len(src))
	out := make([]string, 0, len(dst)+len(src))
	for _, list := range [][]string{dst, src} {
		for _, v := range list {
			if v == "" || seen[v] {
				continue
			}
			seen[v] = true
			out = append(out, v)
		}
	}
	return out
}

func removeString(in []string, v string) []string {
	out := in[:0]
	for _, x := range in {
		if x != v {
			out = append(out, x)
		}
	}
	return out
}
