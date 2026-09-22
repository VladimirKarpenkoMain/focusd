package main

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"testing"
	"time"

	"github.com/wailsapp/wails/v2/pkg/runtime"

	"focusd/internal/catalog"
	"focusd/internal/config"
	"focusd/internal/focus"
	"focusd/internal/i18n"
)

// breakStore делает запись конфигурации невозможной: временный файл занят
// каталогом. Так проверяются ветки «настройки не сохранились».
func breakStore(t *testing.T, a *App) {
	t.Helper()
	if err := breakStoreDir(a.store.Dir()); err != nil {
		t.Fatal(err)
	}
}

// breakStoreDir ломает запись в конфигурацию указанного каталога.
func breakStoreDir(dir string) error {
	return os.Mkdir(dir+string(os.PathSeparator)+"config.json.tmp", 0o755)
}

// ---- Форма виджета ----

func TestCurrentShape(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	if got := a.currentShape(); got != config.WidgetBar {
		t.Fatalf("форма по умолчанию: %q", got)
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.WidgetShape = config.WidgetRing
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := a.currentShape(); got != config.WidgetRing {
		t.Fatalf("сохранённая форма: %q", got)
	}
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.WidgetShape = "квадрат"
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if got := a.currentShape(); got != config.WidgetBar {
		t.Fatalf("неизвестная форма должна откатываться к плашке: %q", got)
	}
}

func TestSetWidgetShape(t *testing.T) {
	a, _, r, _ := newTestApp(t)

	if err := a.SetWidgetShape(string(config.WidgetRing)); err != nil {
		t.Fatalf("SetWidgetShape: %v", err)
	}
	if got := a.store.Get().Settings.WidgetShape; got != config.WidgetRing {
		t.Fatalf("форма не сохранена: %q", got)
	}
	// Компактный режим выключен — окно трогать не нужно.
	if len(r.sizes) != 0 {
		t.Fatalf("окно перестроено без виджета: %v", r.sizes)
	}

	// Неизвестная форма не ломает окно: сохраняется плашка.
	if err := a.SetWidgetShape("квадрат"); err != nil {
		t.Fatalf("SetWidgetShape: %v", err)
	}
	if got := a.store.Get().Settings.WidgetShape; got != config.WidgetBar {
		t.Fatalf("неизвестная форма сохранена как %q", got)
	}
}

func TestSetWidgetShapeResizesActiveWidget(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Widget = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.SetWidgetShape(string(config.WidgetRing)); err != nil {
		t.Fatalf("SetWidgetShape: %v", err)
	}
	if len(r.sizes) == 0 || r.sizes[len(r.sizes)-1] != [2]int{ringWidth, ringHeight} {
		t.Fatalf("окно не стало кругом: %v", r.sizes)
	}
}

func TestSetWidgetShapeReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.SetWidgetShape(string(config.WidgetRing)); err == nil {
		t.Fatal("ожидалась ошибка записи настроек")
	}
}

func TestResizeWidgetSkippedWithoutWindow(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Widget = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	a.ctx = nil
	a.resizeWidget()
	if len(r.sizes) != 0 {
		t.Fatal("без окна менять размеры нечему")
	}
}

func TestResizeWidgetSkippedWhenNotWidget(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	a.resizeWidget()
	if len(r.sizes) != 0 {
		t.Fatal("в обычном режиме окно не трогают")
	}
}

// ---- Настройки ----

func TestGetSettings(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	got := a.GetSettings()
	if got.WidgetShape != config.WidgetBar {
		t.Fatalf("форма виджета: %q", got.WidgetShape)
	}
	if got.DefaultDurationMin == 0 {
		t.Fatal("настройки вернулись пустыми")
	}
}

func TestSaveSettingsCleansAndApplies(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	var autostart []bool
	s := sys
	s.SetAutostart = func(_ context.Context, enable bool, _ string) error {
		autostart = append(autostart, enable)
		return nil
	}

	if err := a.SaveSettings(config.Settings{
		Autostart:          true,
		DefaultDurationMin: 0, // недопустимо — должно стать 25
		DefaultStrictness:  "жестокая",
		Theme:              "неоновая",
		WidgetShape:        "квадрат",
		CustomDomains:      []string{" YouTube.com ", "youtube.com", "мусор"},
		Allowlist:          []string{"https://music.youtube.com/feed", ""},
		// Поля виджета приходят пустыми: их приложение берёт из хранилища, а не
		// из панели настроек.
		Widget:      true,
		WidgetOnTop: false,
		WidgetX:     777,
		WidgetY:     888,
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	got := a.store.Get().Settings
	if got.DefaultDurationMin != 25 {
		t.Fatalf("длительность по умолчанию: %d", got.DefaultDurationMin)
	}
	if got.DefaultStrictness != config.Strict {
		t.Fatalf("строгость по умолчанию: %q", got.DefaultStrictness)
	}
	if got.Theme != config.ThemeSystem {
		t.Fatalf("оформление по умолчанию: %q", got.Theme)
	}
	if got.WidgetShape != config.WidgetBar {
		t.Fatalf("форма по умолчанию: %q", got.WidgetShape)
	}
	if len(got.CustomDomains) != 1 || got.CustomDomains[0] != "youtube.com" {
		t.Fatalf("свои домены: %v", got.CustomDomains)
	}
	if len(got.Allowlist) != 1 || got.Allowlist[0] != "music.youtube.com" {
		t.Fatalf("исключения: %v", got.Allowlist)
	}
	// Режим и положение виджета панелью не перезаписываются: приезжает снимок,
	// снятый до сохранения, и затирать им уже унесённый виджет нельзя.
	if got.Widget || !got.WidgetOnTop || got.WidgetX != 0 || got.WidgetY != 0 {
		t.Fatalf("настройки виджета затёрты панелью: %+v", got)
	}
	if len(autostart) != 1 || !autostart[0] {
		t.Fatalf("автозапуск не применён: %v", autostart)
	}
}

func TestSaveSettingsKeepsWidgetPosition(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Widget = true
		f.Settings.WidgetOnTop = true
		f.Settings.WidgetX, f.Settings.WidgetY = 100, 200
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.SaveSettings(config.Settings{DefaultDurationMin: 30}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}

	got := a.store.Get().Settings
	if !got.Widget || !got.WidgetOnTop || got.WidgetX != 100 || got.WidgetY != 200 {
		t.Fatalf("положение виджета потерялось: %+v", got)
	}
}

func TestSaveSettingsReportsAutostartError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	sys.SetAutostart = func(context.Context, bool, string) error { return errors.New("schtasks отказал") }

	if err := a.SaveSettings(config.Settings{DefaultDurationMin: 30}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if a.currentError() == "" {
		t.Fatal("ошибка автозапуска не показана")
	}
}

func TestSaveSettingsWithoutExePath(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	var called int
	sys.ExecutablePath = func() (string, error) { return "", errors.New("путь неизвестен") }
	sys.SetAutostart = func(context.Context, bool, string) error { called++; return nil }

	if err := a.SaveSettings(config.Settings{DefaultDurationMin: 30}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if called != 0 {
		t.Fatal("автозапуск настроен без пути к приложению")
	}
}

func TestSaveSettingsReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.SaveSettings(config.Settings{DefaultDurationMin: 30}); err == nil {
		t.Fatal("ожидалась ошибка записи настроек")
	}
}

// Идущая сессия должна подхватить новые правила сразу, а не со следующего
// запуска.
func TestSaveSettingsReappliesDuringSession(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, f := newTestApp(t, withProxyRunning())
	f.sess, f.active = softSession(), true
	s.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }

	if err := a.SaveSettings(config.Settings{
		DefaultDurationMin: 30,
		CustomDomains:      []string{"example.com"},
	}); err != nil {
		t.Fatalf("SaveSettings: %v", err)
	}
	if a.currentError() == "" {
		t.Fatal("ошибка переприменения правил не показана")
	}
	if len(f.sess.CustomDomains) == 0 {
		t.Fatal("новые домены не попали в идущую сессию")
	}
}

// ---- Виджет ----

func TestSetWidgetIsIdempotent(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	if err := a.SetWidget(false); err != nil {
		t.Fatalf("SetWidget: %v", err)
	}
	if len(r.sizes) != 0 {
		t.Fatal("режим не менялся — окно трогать нечего")
	}
}

func TestSetWidgetOnAndOff(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.WidgetX, f.Settings.WidgetY = 300, 400
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.SetWidget(true); err != nil {
		t.Fatalf("SetWidget(true): %v", err)
	}
	if !a.store.Get().Settings.Widget {
		t.Fatal("режим не сохранён")
	}
	if x, y := a.widgetPosition(); x != 300 || y != 400 {
		t.Fatalf("положение не подхвачено: %d,%d", x, y)
	}
	if len(r.positions) == 0 || r.positions[len(r.positions)-1] != [2]int{300, 400} {
		t.Fatalf("окно не переставлено: %v", r.positions)
	}

	// Уход из режима запоминает, куда человек унёс виджет, и возвращает окно.
	r.moved = [2]int{333, 444}
	a.trackWidgetPos()
	if x, y := a.widgetPosition(); x != 333 || y != 444 {
		t.Fatalf("положение не отслежено: %d,%d", x, y)
	}
	if err := a.SetWidget(false); err != nil {
		t.Fatalf("SetWidget(false): %v", err)
	}
	got := a.store.Get().Settings
	if got.Widget || got.WidgetX != 333 || got.WidgetY != 444 {
		t.Fatalf("положение виджета не сохранено: %+v", got)
	}
	if r.centred != 1 {
		t.Fatalf("окно не вернулось в обычный режим: %d", r.centred)
	}
}

func TestSetWidgetReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.SetWidget(true); err == nil {
		t.Fatal("ожидалась ошибка записи настроек")
	}
}

func TestSetTheme(t *testing.T) {
	a, _, r, _ := newTestApp(t)

	if err := a.SetTheme(string(config.ThemeDark)); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if got := a.store.Get().Settings.Theme; got != config.ThemeDark {
		t.Fatalf("тема не сохранена: %q", got)
	}
	if len(r.backgrounds) != 1 || r.backgrounds[0] != [4]uint8{
		windowColourDark.R, windowColourDark.G, windowColourDark.B, windowColourDark.A,
	} {
		t.Fatalf("подложка окна не сменилась: %v", r.backgrounds)
	}

	// Неизвестное значение не ломает оформление.
	if err := a.SetTheme("неоновая"); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if got := a.store.Get().Settings.Theme; got != config.ThemeSystem {
		t.Fatalf("неизвестная тема сохранена как %q", got)
	}
}

func TestSetThemeWithoutWindow(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	a.ctx = nil
	if err := a.SetTheme(string(config.ThemeLight)); err != nil {
		t.Fatalf("SetTheme: %v", err)
	}
	if len(r.backgrounds) != 0 {
		t.Fatal("без окна красить нечего")
	}
}

func TestSetThemeReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.SetTheme(string(config.ThemeDark)); err == nil {
		t.Fatal("ожидалась ошибка записи настроек")
	}
}

// Язык сохраняется отдельным вызовом, как и тема: смена языка не должна
// переприменять блокировку. Каталог и подсказки приезжают в интерфейс переводами
// вместе со следующим состоянием.
func TestSetLanguage(t *testing.T) {
	a, s, r, _ := newTestApp(t)
	before := len(r.emitted)

	if err := a.SetLanguage(string(config.LanguageZH)); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if got := a.store.Get().Settings.Language; got != config.LanguageZH {
		t.Fatalf("язык не сохранён: %q", got)
	}
	if len(r.emitted) != before+1 || r.emitted[len(r.emitted)-1] != "state" {
		t.Fatal("после смены языка состояние не разослано")
	}

	// Неизвестное значение возвращает выбор системе, а не ломает язык вовсе.
	if err := a.SetLanguage("неоновый"); err != nil {
		t.Fatalf("SetLanguage: %v", err)
	}
	if got := a.store.Get().Settings.Language; got != config.LanguageAuto {
		t.Fatalf("неизвестный язык сохранён как %q", got)
	}

	// Выбор языка виден интерфейсу: второй раз «как в системе» разрешать негде.
	f := a.snapshot()
	if f.Language != string(config.LanguageAuto) {
		t.Fatalf("выбор языка в состоянии: %q", f.Language)
	}
	if f.ResolvedLanguage != string(i18n.RU) {
		t.Fatalf("язык системы в тестах русский, получено %q", f.ResolvedLanguage)
	}

	s.SystemLanguage = func() string { return "zh-CN" }
	if got := a.snapshot().ResolvedLanguage; got != string(i18n.ZH) {
		t.Fatalf("при пустом выборе язык берётся у системы, получено %q", got)
	}
	if err := a.SetLanguage(string(config.LanguageEN)); err != nil {
		t.Fatal(err)
	}
	if got := a.snapshot().ResolvedLanguage; got != string(i18n.EN) {
		t.Fatalf("явный выбор сильнее системного, получено %q", got)
	}
}

func TestSetLanguageReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.SetLanguage(string(config.LanguageEN)); err == nil {
		t.Fatal("ожидалась ошибка записи настроек")
	}
}

// Каталог отдаётся на выбранном языке: подписи групп — единственное, что в нём
// переводится, домены участвуют в правилах блокировки и обязаны не меняться.
func TestGetCatalogIsLocalized(t *testing.T) {
	a, _, _, _ := newTestApp(t)

	if err := a.SetLanguage(string(config.LanguageEN)); err != nil {
		t.Fatal(err)
	}
	groups := a.GetCatalog()
	if len(groups) != len(catalog.IDs()) {
		t.Fatalf("каталог вернул %d групп", len(groups))
	}
	var video catalog.Group
	for _, g := range groups {
		if g.ID == "video" {
			video = g
		}
	}
	if video.Title != "Video & streaming" {
		t.Fatalf("подпись группы не переведена: %q", video.Title)
	}
	if len(video.Domains) == 0 || video.Domains[0] != "youtube.com" {
		t.Fatalf("домены групп переводить нельзя: %v", video.Domains)
	}

	if err := a.SetLanguage(string(config.LanguageRU)); err != nil {
		t.Fatal(err)
	}
	for _, g := range a.GetCatalog() {
		if g.ID == "video" && g.Title != "Видео и стриминг" {
			t.Fatalf("русская подпись потерялась: %q", g.Title)
		}
	}
}

func TestSetWidgetOnTop(t *testing.T) {
	a, _, r, _ := newTestApp(t)

	// Виджет включён — окно обязано отреагировать сразу.
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Widget = true
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWidgetOnTop(true); err != nil {
		t.Fatalf("SetWidgetOnTop: %v", err)
	}
	if len(r.onTop) != 1 || !r.onTop[0] {
		t.Fatalf("окно не поднято поверх: %v", r.onTop)
	}

	// Виджет выключен — трогать нечего.
	r.onTop = nil
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.Widget = false
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := a.SetWidgetOnTop(true); err != nil {
		t.Fatalf("SetWidgetOnTop: %v", err)
	}
	if len(r.onTop) != 0 {
		t.Fatal("без виджета «поверх окон» ни на что не влияет")
	}
}

func TestSetWidgetOnTopWithoutWindow(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	a.ctx = nil
	if err := a.SetWidgetOnTop(true); err != nil {
		t.Fatalf("SetWidgetOnTop: %v", err)
	}
	if len(r.onTop) != 0 {
		t.Fatal("без окна настраивать нечего")
	}
}

func TestSetWidgetOnTopReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.SetWidgetOnTop(true); err == nil {
		t.Fatal("ожидалась ошибка записи настроек")
	}
}

func TestApplyWidgetWithoutWindow(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	a.ctx = nil
	a.applyWidget(true)
	a.applyWidget(false)
	if len(r.sizes) != 0 {
		t.Fatal("без окна менять нечего")
	}
}

func TestApplyWidgetReturnsToNormalSize(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	a.applyWidget(false)

	if len(r.sizes) != 1 || r.sizes[0] != [2]int{mainWidth, mainHeight} {
		t.Fatalf("окно не вернулось к обычному размеру: %v", r.sizes)
	}
	if len(r.minSizes) != 1 || r.minSizes[0] != [2]int{minWidth, minHeight} {
		t.Fatalf("минимальный размер не восстановлен: %v", r.minSizes)
	}
	if r.centred != 1 {
		t.Fatalf("окно не отцентрировано: %d", r.centred)
	}
	if len(r.onTop) != 1 || r.onTop[0] {
		t.Fatalf("«поверх окон» не снят: %v", r.onTop)
	}
}

func TestApplyWidgetUsesScreenCorner(t *testing.T) {
	a, _, r, _ := newTestApp(t)
	r.screens = []runtime.Screen{screen(true, false, 1920, 0)}

	a.applyWidget(true)

	want := [2]int{1920 - barWidth - widgetMargin, widgetMargin}
	if len(r.positions) != 1 || r.positions[0] != want {
		t.Fatalf("виджет поставлен в %v, ожидалось %v", r.positions, want)
	}
}

func TestDefaultWidgetPos(t *testing.T) {
	a, _, r, _ := newTestApp(t)

	// Ошибка перечисления экранов: привычный угол.
	r.screenErr = errors.New("нет экранов")
	if x, y := defaultWidgetPos(a.ctx, barWidth); x != 600 || y != widgetMargin {
		t.Fatalf("без экранов получилось %d,%d", x, y)
	}

	// Неосновной и не текущий экран пропускается.
	r.screenErr = nil
	r.screens = []runtime.Screen{screen(false, false, 3840, 0), screen(true, false, 1920, 0)}
	if x, _ := defaultWidgetPos(a.ctx, barWidth); x != 1920-barWidth-widgetMargin {
		t.Fatalf("выбран не тот экран: x=%d", x)
	}

	// Ширина только в старом поле — тоже годится.
	r.screens = []runtime.Screen{screen(false, true, 0, 1600)}
	if x, _ := defaultWidgetPos(a.ctx, barWidth); x != 1600-barWidth-widgetMargin {
		t.Fatalf("старое поле ширины не учтено: x=%d", x)
	}

	// Экран уже виджета: ставить некуда, остаётся привычный угол.
	r.screens = []runtime.Screen{screen(true, false, 100, 0)}
	if x, y := defaultWidgetPos(a.ctx, barWidth); x != 600 || y != widgetMargin {
		t.Fatalf("узкий экран дал %d,%d", x, y)
	}
}

func TestTrackWidgetPosSkipped(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	a.ctx = nil
	a.trackWidgetPos()
	a.ctx = context.Background()
	// Компактный режим выключен — положение не отслеживается.
	a.trackWidgetPos()
	if x, y := a.widgetPosition(); x != 0 || y != 0 {
		t.Fatalf("положение отслежено зря: %d,%d", x, y)
	}
}

func TestPersistWidgetPosSkipsUntouched(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	a.persistWidgetPos()
	if got := a.store.Get().Settings; got.WidgetX != 0 || got.WidgetY != 0 {
		t.Fatalf("в настройки записался угол по умолчанию: %+v", got)
	}
}

// ---- Домены ----

func TestAddAndRemoveCustomDomain(t *testing.T) {
	a, _, r, _ := newTestApp(t)

	if err := a.AddCustomDomain("мусор"); err == nil {
		t.Fatal("ожидалась ошибка разбора домена")
	}
	if err := a.AddCustomDomain("https://YouTube.com/shorts"); err != nil {
		t.Fatalf("AddCustomDomain: %v", err)
	}
	if err := a.AddCustomDomain("youtube.com"); err != nil {
		t.Fatalf("AddCustomDomain: %v", err)
	}
	if got := a.store.Get().Settings.CustomDomains; len(got) != 1 || got[0] != "youtube.com" {
		t.Fatalf("домены: %v", got)
	}
	if len(r.emitted) == 0 {
		t.Fatal("состояние не отправлено в интерфейс")
	}

	if err := a.RemoveCustomDomain("youtube.com"); err != nil {
		t.Fatalf("RemoveCustomDomain: %v", err)
	}
	if got := a.store.Get().Settings.CustomDomains; len(got) != 0 {
		t.Fatalf("домен не убран: %v", got)
	}
}

func TestAddAndRemoveAllowlistDomain(t *testing.T) {
	a, _, _, _ := newTestApp(t)

	if err := a.AddAllowlistDomain("мусор"); err == nil {
		t.Fatal("ожидалась ошибка разбора домена")
	}
	if err := a.AddAllowlistDomain("music.youtube.com"); err != nil {
		t.Fatalf("AddAllowlistDomain: %v", err)
	}
	if err := a.AddAllowlistDomain("music.youtube.com"); err != nil {
		t.Fatalf("AddAllowlistDomain: %v", err)
	}
	if got := a.store.Get().Settings.Allowlist; len(got) != 1 {
		t.Fatalf("исключения: %v", got)
	}

	if err := a.RemoveAllowlistDomain("music.youtube.com"); err != nil {
		t.Fatalf("RemoveAllowlistDomain: %v", err)
	}
	if got := a.store.Get().Settings.Allowlist; len(got) != 0 {
		t.Fatalf("исключение не убрано: %v", got)
	}
}

func TestRemoveCustomDomainFromSession(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	if err := a.store.Update(func(f *config.File) error {
		f.Settings.CustomDomains = []string{"example.com"}
		f.Session = &config.Session{CustomDomains: []string{"example.com"}}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if err := a.RemoveCustomDomain("example.com"); err != nil {
		t.Fatalf("RemoveCustomDomain: %v", err)
	}
	f := a.store.Get()
	if len(f.Settings.CustomDomains) != 0 || len(f.Session.CustomDomains) != 0 {
		t.Fatalf("домен остался: %+v / %+v", f.Settings.CustomDomains, f.Session.CustomDomains)
	}
}

func TestAddCustomDomainReportsStoreError(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	breakStore(t, a)

	if err := a.AddCustomDomain("youtube.com"); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
	if err := a.AddAllowlistDomain("youtube.com"); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
	if err := a.RemoveCustomDomain("youtube.com"); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
	if err := a.RemoveAllowlistDomain("youtube.com"); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
}

func TestReapplyIfActive(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, _, f := newTestApp(t, withProxyRunning())
	if err := a.reapplyIfActive(); err != nil {
		t.Fatalf("reapplyIfActive без сессии: %v", err)
	}

	f.sess, f.active = softSession(), true
	s.UnblockSitesInBrowsers = func() error { return errors.New("реестр недоступен") }
	if err := a.reapplyIfActive(); err != nil {
		t.Fatalf("reapplyIfActive с сессией: %v", err)
	}
	if a.currentError() == "" {
		t.Fatal("ошибка переприменения правил не показана")
	}
	if !a.isEngaged() {
		t.Fatal("правила идущей сессии не переприменены")
	}
}

// ---- Проводник ----

func TestOpenDataDir(t *testing.T) {
	a, _, _, _ := newTestApp(t)
	var called []string
	saved := execCommand
	execCommand = func(name string, arg ...string) *exec.Cmd {
		called = append(called, name+" "+arg[0])
		// Настоящая команда не запускается: проверяется только то, что
		// приложение позвало проводник и не упало.
		return exec.Command("cmd", "/c", "exit", "0")
	}
	t.Cleanup(func() { execCommand = saved })

	if err := a.OpenDataDir(); err != nil {
		t.Fatalf("OpenDataDir: %v", err)
	}
	if len(called) != 1 {
		t.Fatalf("проводник вызван %d раз", len(called))
	}
}

// ---- Проверка домена ----

func TestTestDomain(t *testing.T) {
	a, _, _, f := newTestApp(t)
	if err := a.store.Update(func(file *config.File) error {
		file.Settings.DefaultGroups = []string{"video"}
		file.Settings.CustomDomains = []string{"example.com"}
		file.Settings.Allowlist = []string{"music.youtube.com"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	if a.TestDomain("мусор") {
		t.Fatal("нераспознанное имя не может быть заблокировано")
	}
	if !a.TestDomain("youtube.com") {
		t.Fatal("домен из группы должен быть закрыт")
	}
	if !a.TestDomain("example.com") {
		t.Fatal("свой домен должен быть закрыт")
	}
	if a.TestDomain("music.youtube.com") {
		t.Fatal("исключение должно перебивать блокировку")
	}
	if a.TestDomain("example.org") {
		t.Fatal("посторонний домен не должен быть закрыт")
	}

	// Идущая сессия диктует свои правила.
	f.sess, f.active = softSession(), true
	f.sess.Groups = nil
	f.sess.CustomDomains = []string{"only-here.com"}
	if a.TestDomain("youtube.com") {
		t.Fatal("правила идущей сессии не учтены")
	}
	if !a.TestDomain("only-here.com") {
		t.Fatal("домены идущей сессии не учтены")
	}
}

// screen собирает описание экрана. Тип размера объявлен внутри Wails и снаружи
// не называется, поэтому поля заполняются по одному.
func screen(isPrimary, isCurrent bool, logicalWidth, oldWidth int) runtime.Screen {
	var s runtime.Screen
	s.IsPrimary, s.IsCurrent = isPrimary, isCurrent
	s.Size.Width = logicalWidth
	s.Width = oldWidth
	return s
}

// ---- Петля ----

// Петля раз в секунду приводит состояние в порядок и рассылает его интерфейсу.
// Проверяется живьём: подделывать тикер нечем, а поведение важно — именно на
// нём ломалась блокировка после конца сессии.
func TestLoopEmitsAndEndsSession(t *testing.T) {
	useLocalProxyTarget(t)

	a, s, r, f := newTestApp(t, withProxyRunning())
	s.BrowserProxyAddr = func() string { return a.prox.Addr() }
	f.sess, f.active = softSession(), true
	f.view = focus.View{Active: true, Strictness: config.Soft, Remaining: 60}
	a.setEngaged(true)

	go a.loop()
	defer close(a.stopCh)

	// Первый тик: сессия идёт — состояние уходит в интерфейс.
	deadline := time.Now().Add(3 * time.Second)
	for len(r.emitted) == 0 && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if len(r.emitted) == 0 {
		t.Fatal("идущая сессия не отправила состояние в интерфейс")
	}

	// Сессия кончилась сама: блокировка обязана сняться без участия человека.
	f.active = false
	deadline = time.Now().Add(3 * time.Second)
	for a.isEngaged() && time.Now().Before(deadline) {
		time.Sleep(20 * time.Millisecond)
	}
	if a.isEngaged() {
		t.Fatal("истёкшая сессия оставила блокировку включённой")
	}
}
