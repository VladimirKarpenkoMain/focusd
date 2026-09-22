//go:build windows

package winsys

import (
	"errors"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Тесты системного прокси на реестре в памяти. Настоящие настройки WinINET
// трогать нельзя, а ветки «не удалось записать» и «не удалось вернуть» на живой
// системе и не наступают — между тем именно от них зависит, останется ли
// человек с прокси, ведущим в мёртвый порт.

const testProxyKey = `Software\Focusd\Tests\Internet Settings`

func withFakeSystemProxy(t *testing.T) *fakeReg {
	t.Helper()
	saved := systemProxyKeyPath
	systemProxyKeyPath = testProxyKey
	t.Cleanup(func() { systemProxyKeyPath = saved })
	return withFakeRegistry(t)
}

// seedProxy кладёт в фейковый реестр прежние настройки человека.
func seedProxy(reg *fakeReg, enable uint32, server, pac string) {
	values := make(map[string]string)
	values[systemProxyEnableValue] = itoa(enable)
	if server != "" {
		values[systemProxyServerValue] = server
	}
	if pac != "" {
		values[systemProxyAutoConfigValue] = pac
	}
	reg.values[testProxyKey] = values
}

func itoa(v uint32) string {
	if v == 0 {
		return "0"
	}
	return "1"
}

func TestSetSystemProxyReplacesAndKeepsBackup(t *testing.T) {
	reg := withFakeSystemProxy(t)
	seedProxy(reg, 1, "10.0.0.1:8080", "http://wpad/wpad.dat")

	prev, err := SetSystemProxy("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("SetSystemProxy: %v", err)
	}
	if !prev.Enable || prev.Server != "10.0.0.1:8080" || prev.AutoConfigURL != "http://wpad/wpad.dat" {
		t.Fatalf("снимок прежних настроек неверен: %+v", prev)
	}
	if got := reg.value(testProxyKey, systemProxyServerValue); got != "127.0.0.1:47653" {
		t.Fatalf("адрес прокси = %q", got)
	}
	if got := reg.value(testProxyKey, systemProxyEnableValue); got != "1" {
		t.Fatalf("ProxyEnable = %q, ожидалось 1", got)
	}
	// PAC главнее ProxyServer: оставшись, он увёл бы браузеры мимо Focusd.
	if _, ok := reg.values[testProxyKey][systemProxyAutoConfigValue]; ok {
		t.Fatal("PAC-файл остался и перебил бы наш прокси")
	}
	if got := SystemProxyAddr(); got != "127.0.0.1:47653" {
		t.Fatalf("SystemProxyAddr = %q", got)
	}
}

// PAC у человека не было — удалять нечего, и это не ошибка.
func TestSetSystemProxyWithoutPac(t *testing.T) {
	reg := withFakeSystemProxy(t)
	seedProxy(reg, 0, "10.0.0.1:8080", "")

	prev, err := SetSystemProxy("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("SetSystemProxy: %v", err)
	}
	if prev.Enable || prev.Server != "10.0.0.1:8080" || prev.AutoConfigURL != "" {
		t.Fatalf("снимок неверен: %+v", prev)
	}
}

func TestSetSystemProxyOnEmptyRegistry(t *testing.T) {
	reg := withFakeSystemProxy(t)

	prev, err := SetSystemProxy("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("SetSystemProxy: %v", err)
	}
	if prev.Enable || prev.Server != "" || prev.AutoConfigURL != "" {
		t.Fatalf("на пустом реестре снимок должен быть пустым: %+v", prev)
	}
	if reg.value(testProxyKey, systemProxyServerValue) != "127.0.0.1:47653" {
		t.Fatal("прокси не прописан")
	}
}

func TestSetSystemProxyErrors(t *testing.T) {
	t.Run("пустой адрес", func(t *testing.T) {
		withFakeSystemProxy(t)
		if _, err := SetSystemProxy("  "); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("настройки не читаются", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.openErr = errors.New("реестр недоступен")
		if _, err := SetSystemProxy("127.0.0.1:47653"); err == nil {
			t.Fatal("ожидалась ошибка чтения прежних настроек")
		}
	})

	t.Run("ключ не создаётся", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.createErr = errors.New("отказано в доступе")
		if _, err := SetSystemProxy("127.0.0.1:47653"); err == nil {
			t.Fatal("ожидалась ошибка создания ключа")
		}
	})

	t.Run("ProxyEnable не пишется", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.setDwordErr = errors.New("запись запрещена")
		if _, err := SetSystemProxy("127.0.0.1:47653"); err == nil {
			t.Fatal("ожидалась ошибка записи ProxyEnable")
		}
	})

	t.Run("адрес не пишется", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.setStringErr = errors.New("запись запрещена")
		if _, err := SetSystemProxy("127.0.0.1:47653"); err == nil {
			t.Fatal("ожидалась ошибка записи адреса")
		}
	})

	t.Run("PAC не удаляется", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		seedProxy(reg, 1, "10.0.0.1:8080", "http://wpad/wpad.dat")
		reg.deleteValErr = errors.New("удаление запрещено")
		if _, err := SetSystemProxy("127.0.0.1:47653"); err == nil {
			t.Fatal("ожидалась ошибка удаления PAC")
		}
	})
}

// Возврат настроек — последняя линия: прокси, оставшийся включённым на мёртвый
// порт, оставляет человека без интернета вовсе.
func TestRestoreSystemProxy(t *testing.T) {
	reg := withFakeSystemProxy(t)

	if err := RestoreSystemProxy(SystemProxySettings{
		Enable:        true,
		Server:        "10.0.0.1:8080",
		AutoConfigURL: "http://wpad/wpad.dat",
	}); err != nil {
		t.Fatalf("RestoreSystemProxy: %v", err)
	}
	if reg.value(testProxyKey, systemProxyServerValue) != "10.0.0.1:8080" {
		t.Fatal("адрес человека не возвращён")
	}
	if reg.value(testProxyKey, systemProxyAutoConfigValue) != "http://wpad/wpad.dat" {
		t.Fatal("PAC не возвращён")
	}
	if reg.value(testProxyKey, systemProxyEnableValue) != "1" {
		t.Fatal("ProxyEnable не возвращён")
	}
}

// Значения, которого у человека не было, должны именно исчезнуть: оставшийся
// адрес Focusd увёл бы браузеры в мёртвый порт.
func TestRestoreSystemProxyDeletesMissingValues(t *testing.T) {
	reg := withFakeSystemProxy(t)
	seedProxy(reg, 1, "127.0.0.1:47653", "")

	if err := RestoreSystemProxy(SystemProxySettings{}); err != nil {
		t.Fatalf("RestoreSystemProxy: %v", err)
	}
	if _, ok := reg.values[testProxyKey][systemProxyServerValue]; ok {
		t.Fatal("адрес Focusd остался в настройках")
	}
	if reg.value(testProxyKey, systemProxyEnableValue) != "0" {
		t.Fatal("прокси не выключен")
	}
}

func TestRestoreSystemProxyErrors(t *testing.T) {
	t.Run("ключ не создаётся", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.createErr = errors.New("отказано в доступе")
		if err := RestoreSystemProxy(SystemProxySettings{}); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("ProxyEnable не пишется", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.setDwordErr = errors.New("запись запрещена")
		if err := RestoreSystemProxy(SystemProxySettings{}); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("адрес не возвращается", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.setStringErr = errors.New("запись запрещена")
		if err := RestoreSystemProxy(SystemProxySettings{Server: "10.0.0.1:8080"}); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("PAC не удаляется", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		seedProxy(reg, 0, "", "http://wpad/wpad.dat")
		reg.deleteValErr = errors.New("удаление запрещено")
		if err := RestoreSystemProxy(SystemProxySettings{}); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	// Адрес возвращается, а PAC удалить не дают: ошибка обязана дойти наверх,
	// а не спрятаться за успешным возвратом адреса.
	t.Run("PAC не удаляется после адреса", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		seedProxy(reg, 1, "127.0.0.1:47653", "http://wpad/wpad.dat")
		reg.deleteValErr = errors.New("удаление запрещено")
		err := RestoreSystemProxy(SystemProxySettings{Enable: true, Server: "10.0.0.1:8080"})
		if err == nil {
			t.Fatal("ожидалась ошибка удаления PAC")
		}
		if reg.value(testProxyKey, systemProxyServerValue) != "10.0.0.1:8080" {
			t.Fatal("адрес должен был вернуться до сбоя на PAC")
		}
	})

	// Значения не было — удаление отсутствующего не считается ошибкой.
	t.Run("PAC и так отсутствует", func(t *testing.T) {
		withFakeSystemProxy(t)
		if err := restoreStringValue(mustKey(t), systemProxyAutoConfigValue, ""); err != nil {
			t.Fatalf("отсутствующее значение не должно считаться ошибкой: %v", err)
		}
	})
}

func mustKey(t *testing.T) regKey {
	t.Helper()
	k, _, err := createRegKey(registry.CURRENT_USER, testProxyKey, registry.SET_VALUE)
	if err != nil {
		t.Fatalf("не удалось открыть ключ: %v", err)
	}
	return k
}

func TestDisableSystemProxy(t *testing.T) {
	reg := withFakeSystemProxy(t)
	seedProxy(reg, 1, "127.0.0.1:47653", "")

	if err := DisableSystemProxy(); err != nil {
		t.Fatalf("DisableSystemProxy: %v", err)
	}
	if got := SystemProxyAddr(); got != "" {
		t.Fatalf("прокси остался включённым: %q", got)
	}
	// Адрес сохраняется: снимка настроек уже нет, и вернуть значение будет
	// неоткуда — важно лишь, чтобы прокси не вёл в мёртвый порт.
	if reg.value(testProxyKey, systemProxyServerValue) != "127.0.0.1:47653" {
		t.Fatal("адрес пропал вместе с выключением")
	}
}

func TestDisableSystemProxyErrors(t *testing.T) {
	t.Run("ключ не создаётся", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.createErr = errors.New("отказано в доступе")
		if err := DisableSystemProxy(); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})

	t.Run("ProxyEnable не пишется", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.setDwordErr = errors.New("запись запрещена")
		if err := DisableSystemProxy(); err == nil {
			t.Fatal("ожидалась ошибка")
		}
	})
}

func TestSystemProxySettingsNow(t *testing.T) {
	t.Run("ключа нет", func(t *testing.T) {
		withFakeSystemProxy(t)
		got, err := SystemProxySettingsNow()
		if err != nil {
			t.Fatalf("отсутствие ключа — не ошибка: %v", err)
		}
		if got.Enable || got.Server != "" || got.AutoConfigURL != "" {
			t.Fatalf("настройки ненулевые: %+v", got)
		}
	})

	t.Run("реестр недоступен", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.openErr = errors.New("отказано в доступе")
		if _, err := SystemProxySettingsNow(); err == nil {
			t.Fatal("ожидалась ошибка чтения")
		}
	})

	t.Run("значения разобраны", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		values := map[string]string{
			systemProxyEnableValue:     "1",
			systemProxyServerValue:     "10.0.0.1:8080",
			systemProxyAutoConfigValue: "http://wpad/wpad.dat",
		}
		reg.values[testProxyKey] = values
		got, err := SystemProxySettingsNow()
		if err != nil {
			t.Fatalf("SystemProxySettingsNow: %v", err)
		}
		if !got.Enable || got.Server != "10.0.0.1:8080" || got.AutoConfigURL != "http://wpad/wpad.dat" {
			t.Fatalf("настройки разобраны неверно: %+v", got)
		}
	})

	t.Run("чужие типы значений", func(t *testing.T) {
		// ProxyEnable не число: поле остаётся выключенным, а не ломает чтение.
		reg := withFakeSystemProxy(t)
		reg.values[testProxyKey] = map[string]string{systemProxyEnableValue: "да"}
		got, err := SystemProxySettingsNow()
		if err != nil {
			t.Fatalf("SystemProxySettingsNow: %v", err)
		}
		if got.Enable {
			t.Fatalf("нечисловое значение включило прокси: %+v", got)
		}
	})
}

func TestSystemProxyAddr(t *testing.T) {
	t.Run("выключен", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		seedProxy(reg, 0, "127.0.0.1:47653", "")
		if got := SystemProxyAddr(); got != "" {
			t.Fatalf("выключенный прокси не должен давать адрес: %q", got)
		}
	})

	t.Run("включён", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		seedProxy(reg, 1, "127.0.0.1:47653", "")
		if got := SystemProxyAddr(); got != "127.0.0.1:47653" {
			t.Fatalf("SystemProxyAddr = %q", got)
		}
	})

	t.Run("реестр недоступен", func(t *testing.T) {
		reg := withFakeSystemProxy(t)
		reg.openErr = errors.New("отказано в доступе")
		if got := SystemProxyAddr(); got != "" {
			t.Fatalf("при сбое чтения адреса быть не должно: %q", got)
		}
	})
}

func TestRestoreStringValueErrors(t *testing.T) {
	reg := withFakeSystemProxy(t)

	if err := restoreStringValue(mustKey(t), "Имя", "значение"); err != nil {
		t.Fatalf("restoreStringValue: %v", err)
	}

	reg.setStringErr = errors.New("запись запрещена")
	if err := restoreStringValue(mustKey(t), "Имя", "значение"); err == nil {
		t.Fatal("ожидалась ошибка записи")
	}

	reg.setStringErr = nil
	reg.deleteValErr = errors.New("удаление запрещено")
	if err := restoreStringValue(mustKey(t), "Имя", ""); err == nil {
		t.Fatal("ожидалась ошибка удаления")
	}
}

// notifyProxyChanged обязана пережить любой ответ wininet: уведомление — не
// единственный путь, и сбой в нём не повод считать прокси не прописанным.
func TestNotifyProxyChangedIsSilent(t *testing.T) {
	notifyProxyChanged()
	notifyProxyChanged()
}

// Оформление системы читается из реестра; при недоступном ключе или значении
// остаётся светлая тема, а не поломка запуска.
func TestSystemPrefersDarkReadsRegistry(t *testing.T) {
	t.Run("нет ключа", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.openErr = errors.New("отказано в доступе")
		if SystemPrefersDark() {
			t.Fatal("при недоступном ключе тёмной темы быть не должно")
		}
	})

	t.Run("нет значения", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.values[`SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`] = map[string]string{}
		if SystemPrefersDark() {
			t.Fatal("без значения тёмной темы быть не должно")
		}
	})

	t.Run("значение read", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.values[`SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`] = map[string]string{
			"AppsUseLightTheme": "0",
		}
		if !SystemPrefersDark() {
			t.Fatal("AppsUseLightTheme=0 означает тёмное оформление")
		}
	})

	t.Run("значение light", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.values[`SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`] = map[string]string{
			"AppsUseLightTheme": "1",
		}
		if SystemPrefersDark() {
			t.Fatal("AppsUseLightTheme=1 означает светлое оформление")
		}
	})
}

// Язык системы читается оттуда же, откуда и оформление: при недоступном ключе
// или значении приложение обязано остаться работоспособным, а не упасть.
func TestSystemLanguageReadsRegistry(t *testing.T) {
	t.Run("нет ключа", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.openErr = errors.New("отказано в доступе")
		if got := SystemLanguage(); got != "" {
			t.Fatalf("при недоступном ключе язык неизвестен, получено %q", got)
		}
	})

	t.Run("нет значения", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.values[`Control Panel\International`] = map[string]string{}
		if got := SystemLanguage(); got != "" {
			t.Fatalf("без значения язык неизвестен, получено %q", got)
		}
	})

	t.Run("значение read", func(t *testing.T) {
		reg := withFakeRegistry(t)
		reg.values[`Control Panel\International`] = map[string]string{"LocaleName": "zh-CN"}
		if got := SystemLanguage(); got != "zh-CN" {
			t.Fatalf("SystemLanguage() = %q, ожидалось %q", got, "zh-CN")
		}
	})
}

// Если путь к исполняемому файлу не выяснить, задачи планировщика ставить нечем,
// и об этом нужно честно сказать.
func TestExecutablePathError(t *testing.T) {
	saved := osExecutable
	osExecutable = func() (string, error) { return "", errors.New("путь неизвестен") }
	t.Cleanup(func() { osExecutable = saved })

	got, err := ExecutablePath()
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if got != "" {
		t.Fatalf("путь не должен возвращаться при ошибке: %q", got)
	}
	if !strings.Contains(err.Error(), "путь неизвестен") {
		t.Fatalf("ошибка потеряла причину: %v", err)
	}
}
