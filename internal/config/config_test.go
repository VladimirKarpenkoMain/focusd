package config

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestOpenCreatesDefaults(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatalf("Open вернул ошибку: %v", err)
	}

	f := s.Get()
	if f.Session != nil {
		t.Error("на первом запуске сессии быть не должно")
	}
	if f.Settings.DefaultDurationMin != 25 {
		t.Errorf("ожидалось 25 минут по умолчанию, получено %d", f.Settings.DefaultDurationMin)
	}
	if !f.Settings.DefaultStrictness.Valid() {
		t.Error("строгость по умолчанию должна быть корректной")
	}
	// Умолчания логических полей тоже обязаны применяться. Однажды оказалось,
	// что они терялись: конфигурация собиралась с нуля, и «включено по
	// умолчанию» превращалось в «выключено» — тихо и у всех.
	if !f.Settings.WidgetOnTop {
		t.Error("виджет должен по умолчанию держаться поверх окон")
	}
	if f.Settings.Theme != ThemeSystem {
		t.Errorf("оформление по умолчанию: ожидалось %q, получено %q", ThemeSystem, f.Settings.Theme)
	}
	// Язык по умолчанию — «как в системе»: приложение обязано заговорить на языке
	// Windows само, а не после похода в настройки.
	if f.Settings.Language != LanguageAuto {
		t.Errorf("язык по умолчанию: ожидалось %q, получено %q", LanguageAuto, f.Settings.Language)
	}
	if f.Settings.WidgetShape != WidgetBar {
		t.Errorf("форма виджета по умолчанию: ожидалось %q, получено %q", WidgetBar, f.Settings.WidgetShape)
	}
}

// Форма виджета приходит из файла как обычная строка: опечатка или значение из
// будущей версии не должны оставлять окно без размеров.
func TestWidgetShapeFallsBackToDefault(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"settings":{"widgetShape":"квадрат"}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Get().Settings.WidgetShape; got != WidgetBar {
		t.Errorf("неизвестная форма должна заменяться на %q, получено %q", WidgetBar, got)
	}

	if !WidgetRing.Valid() {
		t.Error("круглая форма должна считаться корректной")
	}
	for _, bad := range []WidgetShape{"", "BAR", "circle", "bar "} {
		if bad.Valid() {
			t.Errorf("%q не должна считаться корректной", bad)
		}
	}
}

func TestOpenKeepsExplicitFalse(t *testing.T) {
	dir := t.TempDir()
	// Пользователь осознанно выключил блокировку DoH — умолчание не должно
	// включать её обратно при следующем запуске.
	raw := []byte(`{"settings":{"blockDoh":false,"widgetOnTop":false,"theme":"dark"}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := s.Get()
	if f.Settings.WidgetOnTop {
		t.Error("явно выключенный «поверх окон» включился обратно")
	}
	if f.Settings.Theme != ThemeDark {
		t.Errorf("выбранная тема потерялась: %q", f.Settings.Theme)
	}
	// Поля, которых в файле нет, обязаны остаться на умолчаниях.
	if f.Settings.DefaultDurationMin != 25 {
		t.Errorf("длительность по умолчанию потерялась: %d", f.Settings.DefaultDurationMin)
	}
}

// Файл от старой версии, где ещё были DNS-слой, его снимок и настройки DoH,
// обязан читаться без ошибок: неизвестные ключи просто игнорируются, а
// настройки, которые остались, должны сохраниться.
func TestOldConfigWithDNSFieldsLoads(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{
	  "settings": {
	    "upstream": ["1.1.1.1"],
	    "autostart": true,
	    "blockDoh": true,
	    "blockDot": true,
	    "theme": "dark",
	    "customDomains": ["example.com"]
	  },
	  "session": null,
	  "dnsSnapshot": [{"name":"Ethernet","index":7,"dhcp":true}],
	  "dnsHijacked": true
	}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("файл старой версии не должен мешать запуску: %v", err)
	}
	f := s.Get()
	if !f.Settings.Autostart {
		t.Error("настройка, которая осталась, потерялась")
	}
	if f.Settings.Theme != ThemeDark {
		t.Errorf("тема потерялась: %q", f.Settings.Theme)
	}
	if len(f.Settings.CustomDomains) != 1 || f.Settings.CustomDomains[0] != "example.com" {
		t.Errorf("свои домены потерялись: %v", f.Settings.CustomDomains)
	}
}

func TestUpdatePersistsAndReloads(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}

	err = s.Update(func(f *File) error {
		f.Settings.CustomDomains = []string{"example.com"}
		f.Session = &Session{Strictness: Lock, EndsAt: 4102444800}
		return nil
	})
	if err != nil {
		t.Fatalf("Update вернул ошибку: %v", err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := reopened.Get()

	if len(f.Settings.CustomDomains) != 1 || f.Settings.CustomDomains[0] != "example.com" {
		t.Errorf("свои домены не сохранились: %v", f.Settings.CustomDomains)
	}
	if f.Session == nil || f.Session.Strictness != Lock {
		t.Error("сессия не сохранилась")
	}
}

func TestUpdateRollsBackOnError(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}

	want := s.Get().Settings.DefaultDurationMin
	err = s.Update(func(f *File) error {
		f.Settings.DefaultDurationMin = 999
		return os.ErrPermission
	})
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if got := s.Get().Settings.DefaultDurationMin; got != want {
		t.Errorf("изменение должно было откатиться: %d != %d", got, want)
	}
}

func TestBrokenFileIsQuarantined(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "config.json"), []byte("{ это не json"), 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatalf("битый файл не должен мешать запуску: %v", err)
	}
	if s.Get().Session != nil {
		t.Error("после сброса сессии быть не должно")
	}
	if _, err := os.Stat(filepath.Join(dir, "config.json.broken")); err != nil {
		t.Errorf("битый файл должен быть отложен в сторону: %v", err)
	}
}

func TestGetReturnsIndependentCopy(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(f *File) error {
		f.Settings.CustomDomains = []string{"a.com"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	f := s.Get()
	f.Settings.CustomDomains[0] = "подмена.com"

	if got := s.Get().Settings.CustomDomains[0]; got != "a.com" {
		t.Errorf("Get должен возвращать копию, состояние изменилось на %q", got)
	}
}

// Регрессия: на чистой установке списки уходили в интерфейс как null, из-за чего
// первый же экран падал с исключением и окно оставалось пустым.
func TestFreshInstallHasNoNullLists(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatalf("Open вернул ошибку: %v", err)
	}

	f := s.Get()
	if f.Settings.DefaultGroups == nil {
		t.Error("DefaultGroups не должен быть nil")
	}
	if f.Settings.CustomDomains == nil {
		t.Error("CustomDomains не должен быть nil")
	}
	if f.Settings.Allowlist == nil {
		t.Error("Allowlist не должен быть nil")
	}
	if len(f.Settings.DefaultGroups) == 0 {
		t.Error("на первом запуске группы по умолчанию должны быть выбраны")
	}

	raw, err := json.Marshal(f.Settings)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("null")) {
		t.Errorf("в JSON настроек не должно быть null: %s", raw)
	}
}

func TestGetStateListsSurviveClone(t *testing.T) {
	s, err := Open(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(f *File) error {
		f.Settings.CustomDomains = []string{}
		f.Settings.Allowlist = []string{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	raw, err := json.Marshal(s.Get().Settings)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(raw, []byte("null")) {
		t.Errorf("копирование не должно превращать пустой список в null: %s", raw)
	}
}

func TestExplicitlyEmptyGroupsStayEmpty(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if err := s.Update(func(f *File) error {
		f.Settings.DefaultGroups = []string{}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	got := reopened.Get().Settings.DefaultGroups
	if got == nil {
		t.Fatal("явно пустой список не должен становиться nil")
	}
	if len(got) != 0 {
		t.Errorf("явно опустошённый список не должен наполняться заново: %v", got)
	}
}

func TestStrictnessValid(t *testing.T) {
	valid := []Strictness{Soft, Strict, Lock}
	for _, s := range valid {
		if !s.Valid() {
			t.Errorf("%q должна считаться корректной", s)
		}
	}
	for _, s := range []Strictness{"", "hard", "SOFT"} {
		if s.Valid() {
			t.Errorf("%q не должна считаться корректной", s)
		}
	}
}

// Снимок системного прокси — единственное, чем можно вернуть человеку его
// настройки после подмены. Терять его при перезапуске нельзя: сторож и --restore
// работают отдельными процессами и читают именно этот файл.
func TestSystemProxyBackupSurvivesRestart(t *testing.T) {
	dir := t.TempDir()
	st, err := Open(dir)
	if err != nil {
		t.Fatalf("Open вернул ошибку: %v", err)
	}
	if st.Get().SystemProxy != nil {
		t.Fatal("на чистой установке подмены прокси быть не должно")
	}

	if err := st.Update(func(f *File) error {
		f.SystemProxy = &SystemProxyBackup{Enable: false, Server: "127.0.0.1:2080"}
		return nil
	}); err != nil {
		t.Fatal(err)
	}

	reopened, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	backup := reopened.Get().SystemProxy
	if backup == nil {
		t.Fatal("снимок настроек прокси не пережил перезапуск")
	}
	if backup.Server != "127.0.0.1:2080" || backup.Enable {
		t.Fatalf("снимок восстановлен неверно: %+v", backup)
	}

	// Get отдаёт копию: правка возвращённого снимка не должна менять состояние
	// хранилища, иначе откат неудачного Update затрёт нужные значения.
	backup.Server = "подмена"
	if again := reopened.Get().SystemProxy; again.Server != "127.0.0.1:2080" {
		t.Fatalf("Get вернул ссылку на внутреннее состояние: %+v", again)
	}
}

func TestThemeValid(t *testing.T) {
	for _, th := range []Theme{ThemeSystem, ThemeLight, ThemeDark} {
		if !th.Valid() {
			t.Errorf("%q должно считаться корректным", th)
		}
	}
	for _, th := range []Theme{"", "DARK", "тёмная", "system "} {
		if th.Valid() {
			t.Errorf("%q не должно считаться корректным", th)
		}
	}
}

func TestLanguageValid(t *testing.T) {
	for _, l := range []Language{LanguageAuto, LanguageRU, LanguageEN, LanguageZH} {
		if !l.Valid() {
			t.Errorf("%q должно считаться корректным", l)
		}
	}
	// Пустое значение корректным не считается намеренно: его нет в списке
	// переключателя, а разбирается оно как «как в системе» на стороне приложения.
	for _, l := range []Language{"", "RU", "zh-CN", "de", "русский"} {
		if l.Valid() {
			t.Errorf("%q не должно считаться корректным", l)
		}
	}
}

func TestPathAndDir(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if got := s.Dir(); got != dir {
		t.Errorf("Dir() = %q, ожидалось %q", got, dir)
	}
	if got := s.Path(); got != filepath.Join(dir, "config.json") {
		t.Errorf("Path() = %q, ожидалось %q", got, filepath.Join(dir, "config.json"))
	}
}

// Каталог данных может оказаться недоступным — например, на его месте лежит
// файл. Молча стартовать с настройками в никуда нельзя: об этом нужно сказать.
func TestOpenFailsWhenDirIsNotCreatable(t *testing.T) {
	base := t.TempDir()
	fileInstead := filepath.Join(base, "не-каталог")
	if err := os.WriteFile(fileInstead, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}

	if _, err := Open(filepath.Join(fileInstead, "Focusd")); err == nil {
		t.Fatal("ожидалась ошибка создания каталога данных")
	}
}

// Каталог на месте config.json — не «файла нет», а настоящая ошибка чтения:
// подменять её умолчаниями значило бы потерять настройки человека.
func TestOpenFailsOnUnreadableConfig(t *testing.T) {
	dir := t.TempDir()
	if err := os.Mkdir(filepath.Join(dir, "config.json"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := Open(dir); err == nil {
		t.Fatal("ожидалась ошибка чтения конфигурации")
	}
}

// Опечатки и значения из будущих версий в файле не должны оставлять приложение
// без рабочих настроек: каждое поле проверяется и заменяется на умолчание.
func TestOpenRepairsInvalidValues(t *testing.T) {
	dir := t.TempDir()
	raw := []byte(`{"settings":{
	  "defaultDurationMin":-5,
	  "defaultStrictness":"жестокая",
	  "theme":"неоновая",
	  "defaultGroups":null,
	  "customDomains":null,
	  "allowlist":null
	}}`)
	if err := os.WriteFile(filepath.Join(dir, "config.json"), raw, 0o644); err != nil {
		t.Fatal(err)
	}

	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	f := s.Get()
	def := DefaultSettings()
	if f.Settings.DefaultDurationMin != def.DefaultDurationMin {
		t.Errorf("длительность не исправлена: %d", f.Settings.DefaultDurationMin)
	}
	if f.Settings.DefaultStrictness != def.DefaultStrictness {
		t.Errorf("строгость не исправлена: %q", f.Settings.DefaultStrictness)
	}
	if f.Settings.Theme != def.Theme {
		t.Errorf("тема не исправлена: %q", f.Settings.Theme)
	}
	// Списки были null: их обязательно нужно заменить, иначе интерфейс упадёт на
	// первом же обращении к .length.
	if f.Settings.DefaultGroups == nil || len(f.Settings.DefaultGroups) != len(def.DefaultGroups) {
		t.Errorf("группы по умолчанию не восстановлены: %v", f.Settings.DefaultGroups)
	}
	if f.Settings.CustomDomains == nil || f.Settings.Allowlist == nil {
		t.Errorf("пустые списки остались nil: %+v / %+v",
			f.Settings.CustomDomains, f.Settings.Allowlist)
	}
}

// Записать конфигурацию может и не получиться (диск, права). Тогда изменение
// обязано откатиться в памяти: иначе приложение будет показывать настройки,
// которых на диске нет.
func TestUpdateRollsBackWhenSaveFails(t *testing.T) {
	dir := t.TempDir()
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	// Временный файл занят каталогом: запись в него не пройдёт.
	if err := os.Mkdir(filepath.Join(dir, "config.json.tmp"), 0o755); err != nil {
		t.Fatal(err)
	}

	want := s.Get().Settings.DefaultDurationMin
	err = s.Update(func(f *File) error {
		f.Settings.DefaultDurationMin = 999
		return nil
	})
	if err == nil {
		t.Fatal("ожидалась ошибка записи")
	}
	if got := s.Get().Settings.DefaultDurationMin; got != want {
		t.Errorf("после неудачной записи изменение осталось в памяти: %d != %d", got, want)
	}
}
