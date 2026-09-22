//go:build windows

package winsys

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// ---- Списки сайтов ----

// Главное свойство запасного слоя: список доходит до каждого браузера из
// chromiumVendors, а исключения — до отдельного значения. Пропущенный браузер
// остался бы вообще без блокировки.
func TestBlockSitesInBrowsersCoversEveryVendor(t *testing.T) {
	reg := withFakeRegistry(t)

	if err := BlockSitesInBrowsers(
		[]string{" YouTube.com ", "vk.com", "youtube.com", ""},
		[]string{"music.youtube.com", "music.youtube.com"},
	); err != nil {
		t.Fatalf("BlockSitesInBrowsers вернул ошибку: %v", err)
	}

	for _, vendor := range chromiumVendors {
		blockPath := policyRoot + vendor + `\` + blocklistValue
		if got := reg.value(blockPath, "1"); got != "youtube.com" {
			t.Errorf("%s: значение 1 = %q, ожидалось youtube.com", vendor, got)
		}
		if got := reg.value(blockPath, "2"); got != "vk.com" {
			t.Errorf("%s: значение 2 = %q, ожидалось vk.com", vendor, got)
		}
		if n := len(reg.values[blockPath]); n != 2 {
			t.Errorf("%s: записей блокировки %d, ожидалось 2 (дубликаты и пустые отброшены)", vendor, n)
		}
		allowPath := policyRoot + vendor + `\` + allowlistValue
		if got := reg.value(allowPath, "1"); got != "music.youtube.com" {
			t.Errorf("%s: исключение = %q", vendor, got)
		}
		if n := len(reg.values[allowPath]); n != 1 {
			t.Errorf("%s: исключений %d, ожидалось 1", vendor, n)
		}
	}

	if !SitesBlockedInBrowsers() {
		t.Error("после записи список должен считаться действующим")
	}
}

// Пустой список исключений — это не «ничего не делать», а «снять прежние»:
// иначе исключение прошлой сессии осталось бы в силе навсегда.
func TestBlockSitesInBrowsersClearsEmptyAllowlist(t *testing.T) {
	reg := withFakeRegistry(t)

	if err := BlockSitesInBrowsers([]string{"vk.com"}, []string{"music.youtube.com"}); err != nil {
		t.Fatal(err)
	}
	if err := BlockSitesInBrowsers([]string{"vk.com"}, nil); err != nil {
		t.Fatal(err)
	}

	for _, vendor := range chromiumVendors {
		path := policyRoot + vendor + `\` + allowlistValue
		if _, ok := reg.values[path]; ok {
			t.Errorf("%s: пустой список исключений не снял ключ", vendor)
		}
	}
	if !SitesBlockedInBrowsers() {
		t.Error("список блокировки при этом должен остаться")
	}
}

func TestUnblockSitesInBrowsers(t *testing.T) {
	reg := withFakeRegistry(t)

	if err := BlockSitesInBrowsers([]string{"vk.com"}, []string{"music.youtube.com"}); err != nil {
		t.Fatal(err)
	}
	if err := UnblockSitesInBrowsers(); err != nil {
		t.Fatalf("UnblockSitesInBrowsers вернул ошибку: %v", err)
	}
	if len(reg.values) != 0 {
		t.Fatalf("остались ключи политик: %v", reg.values)
	}
	if SitesBlockedInBrowsers() {
		t.Error("после снятия список не должен считаться действующим")
	}
}

// Отсутствие ключа — не ошибка: снимать нечего.
func TestClearPolicyListIgnoresMissingKey(t *testing.T) {
	withFakeRegistry(t)
	if err := clearPolicyList(`Google\Chrome\URLBlocklist`); err != nil {
		t.Fatalf("отсутствующий ключ не должен считаться ошибкой: %v", err)
	}
}

// А вот настоящая ошибка удаления обязана вернуться: иначе политика осталась бы
// в браузере, а приложение считало бы её снятой.
func TestClearPolicyListReportsRealError(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.deleteErr = errors.New("отказано в доступе")

	err := clearPolicyList(`Google\Chrome\URLBlocklist`)
	if err == nil {
		t.Fatal("ожидалась ошибка удаления")
	}
	if !strings.Contains(err.Error(), "отказано") {
		t.Fatalf("ошибка потеряла причину: %v", err)
	}
}

func TestWritePolicyListReportsCreateError(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.createErr = errors.New("отказано в доступе")

	if err := writePolicyList(`Google\Chrome\URLBlocklist`, []string{"vk.com"}); err == nil {
		t.Fatal("ожидалась ошибка создания ключа")
	}
}

func TestWriteListValuesReportsWriteError(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.writeErr = errors.New("диск переполнен")

	k, _, err := createRegKey(registry.LOCAL_MACHINE, `Тест\Список`, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("не удалось создать ключ: %v", err)
	}
	if err := writeListValues(k, []string{"a.com"}); err == nil {
		t.Fatal("ожидалась ошибка записи значения")
	} else if !strings.Contains(err.Error(), "значение 1") {
		t.Fatalf("ошибка не указывает на значение: %v", err)
	}
}

// Список значений должен быть прочитан, даже если записать ничего не нужно:
// на этом стоит очистка хвоста прошлой сессии.
func TestPolicyListLenReadsNames(t *testing.T) {
	reg := withFakeRegistry(t)
	path := policyRoot + `Google\Chrome\URLBlocklist`
	reg.values[path] = map[string]string{"1": "a.com", "2": "b.com"}

	if got := policyListLen(`Google\Chrome\URLBlocklist`); got != 2 {
		t.Fatalf("policyListLen = %d, ожидалось 2", got)
	}
}

func TestPolicyListLenIsZeroOnErrors(t *testing.T) {
	reg := withFakeRegistry(t)

	// Ключа нет.
	if got := policyListLen(`Google\Chrome\URLBlocklist`); got != 0 {
		t.Fatalf("отсутствующий ключ дал %d", got)
	}

	// Ключ есть, но список значений не читается.
	path := policyRoot + `Google\Chrome\URLBlocklist`
	reg.values[path] = map[string]string{"1": "a.com"}
	reg.readErr = errors.New("сломанный ключ")
	if got := policyListLen(`Google\Chrome\URLBlocklist`); got != 0 {
		t.Fatalf("нечитаемый ключ дал %d", got)
	}
	reg.readErr = nil

	// Реестр недоступен вовсе.
	reg.openErr = errors.New("отказано в доступе")
	if got := policyListLen(`Google\Chrome\URLBlocklist`); got != 0 {
		t.Fatalf("недоступный реестр дал %d", got)
	}
}

// Политика прокси действует, даже если списки сайтов пусты: именно она —
// признак «защита работает прямо сейчас».
func TestBrowsersBlockedSeesProxyPolicy(t *testing.T) {
	reg := withFakeRegistry(t)
	if BrowsersBlocked() {
		t.Fatal("на пустом реестре блокировки быть не должно")
	}

	if err := SetBrowserProxy("127.0.0.1:47653"); err != nil {
		t.Fatalf("SetBrowserProxy: %v", err)
	}
	if !BrowsersBlocked() {
		t.Error("прописанная политика прокси должна считаться блокировкой")
	}
	if SitesBlockedInBrowsers() {
		t.Error("списков сайтов мы не писали: SitesBlockedInBrowsers обязан молчать")
	}

	// Реестр читается, но значения чужие: политика прокси недействительна.
	reg.values = map[string]map[string]string{
		policyRoot + `Google\Chrome\` + proxySettingsKey: {
			proxyModeValue:   "direct",
			proxyServerValue: "127.0.0.1:47653",
		},
	}
	if got := BrowserProxyAddr(); got != "" {
		t.Fatalf("чужой режим прокси принят за наш: %q", got)
	}
}

// ---- Политика прокси ----

func TestSetBrowserProxy(t *testing.T) {
	reg := withFakeRegistry(t)

	if err := SetBrowserProxy("   "); err == nil {
		t.Fatal("пустой адрес должен быть отвергнут")
	}

	if err := SetBrowserProxy("127.0.0.1:47653"); err != nil {
		t.Fatalf("SetBrowserProxy: %v", err)
	}
	for _, vendor := range chromiumVendors {
		path := policyRoot + vendor + `\` + proxySettingsKey
		if got := reg.value(path, proxyModeValue); got != fixedServersMode {
			t.Errorf("%s: режим = %q, ожидался %q", vendor, got, fixedServersMode)
		}
		if got := reg.value(path, proxyServerValue); got != "127.0.0.1:47653" {
			t.Errorf("%s: адрес = %q", vendor, got)
		}
	}
	if got := BrowserProxyAddr(); got != "127.0.0.1:47653" {
		t.Fatalf("BrowserProxyAddr = %q", got)
	}
}

// Если адрес пуст, политика недействительна: браузер ушёл бы в никуда.
func TestBrowserProxyAddrIgnoresEmptyServer(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.values[policyRoot+`Google\Chrome\`+proxySettingsKey] = map[string]string{
		proxyModeValue:   fixedServersMode,
		proxyServerValue: "",
	}
	if got := BrowserProxyAddr(); got != "" {
		t.Fatalf("пустой адрес принят за политику: %q", got)
	}
}

// Ключ есть, но не все значения читаются — политику считать своей нельзя.
func TestBrowserProxyAddrIgnoresIncompleteKey(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.values[policyRoot+`Google\Chrome\`+proxySettingsKey] = map[string]string{
		proxyServerValue: "127.0.0.1:47653",
	}
	if got := BrowserProxyAddr(); got != "" {
		t.Fatalf("неполная политика принята за нашу: %q", got)
	}
}

func TestSetBrowserProxyReportsErrors(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.createErr = errors.New("отказано в доступе")

	err := SetBrowserProxy("127.0.0.1:47653")
	if err == nil {
		t.Fatal("ожидалась ошибка записи политики")
	}
	// Ошибка на каждом браузере: молча пропустить хоть один нельзя.
	for _, vendor := range chromiumVendors {
		if !strings.Contains(err.Error(), vendor) {
			t.Errorf("в ошибке нет %q: %v", vendor, err)
		}
	}
}

func TestClearBrowserProxyReportsErrors(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.deleteErr = errors.New("отказано в доступе")

	err := ClearBrowserProxy()
	if err == nil {
		t.Fatal("ожидалась ошибка снятия политики")
	}
	if !strings.Contains(err.Error(), "политика прокси") {
		t.Fatalf("ошибка потеряла причину: %v", err)
	}

	// Отсутствие ключа ошибкой не считается: снимать нечего.
	reg.deleteErr = nil
	if err := ClearBrowserProxy(); err != nil {
		t.Fatalf("снятие отсутствующей политики: %v", err)
	}
}

// Ни один браузер не должен остаться прикрыт наполовину: сбой на любом из них
// обязан быть виден наверху, а не проглочен.
func TestBlockSitesInBrowsersReportsErrors(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.createErr = errors.New("отказано в доступе")

	err := BlockSitesInBrowsers([]string{"vk.com"}, []string{"music.youtube.com"})
	if err == nil {
		t.Fatal("ожидалась ошибка записи списка")
	}
	// По ошибке на браузер: и блокировка, и исключения.
	for _, vendor := range chromiumVendors {
		if !strings.Contains(err.Error(), vendor) {
			t.Errorf("в ошибке нет %q: %v", vendor, err)
		}
	}
}

func TestUnblockSitesInBrowsersReportsErrors(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.deleteErr = errors.New("отказано в доступе")

	err := UnblockSitesInBrowsers()
	if err == nil {
		t.Fatal("ожидалась ошибка снятия списка")
	}
	for _, vendor := range chromiumVendors {
		if !strings.Contains(err.Error(), vendor) {
			t.Errorf("в ошибке нет %q: %v", vendor, err)
		}
	}
}

// ---- Политики браузеров как значения ----

func TestWritePolicyKinds(t *testing.T) {
	reg := withFakeRegistry(t)

	if err := writePolicy(`Тест\Политика`, "Строка", "sz", "значение"); err != nil {
		t.Fatalf("запись строки: %v", err)
	}
	if got := reg.value(policyRoot+`Тест\Политика`, "Строка"); got != "значение" {
		t.Fatalf("строка записана как %q", got)
	}
	if err := writePolicy(`Тест\Политика`, "Число", "dword", "5"); err != nil {
		t.Fatalf("запись числа: %v", err)
	}
	if got := reg.value(policyRoot+`Тест\Политика`, "Число"); got != "5" {
		t.Fatalf("число записано как %q", got)
	}
}

func TestWritePolicyReportsCreateError(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.createErr = errors.New("отказано в доступе")

	err := writePolicy(`Тест\Политика`, "Имя", "sz", "значение")
	if err == nil {
		t.Fatal("ожидалась ошибка создания ключа")
	}
	if !strings.Contains(err.Error(), "Имя") {
		t.Fatalf("ошибка не называет значение: %v", err)
	}
}

func TestWritePolicyReportsValueError(t *testing.T) {
	reg := withFakeRegistry(t)
	reg.writeErr = errors.New("запись запрещена")

	if err := writePolicy(`Тест\Политика`, "Имя", "sz", "значение"); err == nil {
		t.Fatal("ожидалась ошибка записи значения")
	}
}

// ---- Запущенные браузеры ----

func TestRunningBrowsers(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return `"chrome.exe","1234","Console","1","100 000 КБ"` + "\n" +
			`"browser.exe","4321","Console","1","200 000 КБ"` + "\n" +
			`"notepad.exe","1","Console","1","1 КБ"`, nil
	})

	got := RunningBrowsers(nil)
	want := map[string]bool{"Chrome": true, "Яндекс Браузер": true}
	if len(got) != len(want) {
		t.Fatalf("найдены браузеры %v, ожидались %v", got, want)
	}
	for _, name := range got {
		if !want[name] {
			t.Errorf("лишний браузер в списке: %q", name)
		}
	}
}

func TestRunningBrowsersIgnoresForeignProcesses(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		// "chrome.exe.bak" не должен считаться запущенным Chrome: сравнение
		// идёт по имени в кавычках.
		return `"chrome.exe.bak","1","Console","1","1 КБ"`, nil
	})
	if got := RunningBrowsers(nil); len(got) != 0 {
		t.Fatalf("найдены лишние браузеры: %v", got)
	}
}

func TestRunningBrowsersIsSilentOnError(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "", errors.New("tasklist недоступен")
	})
	if got := RunningBrowsers(nil); got != nil {
		t.Fatalf("при сбое список должен быть пустым, получено %v", got)
	}
}
