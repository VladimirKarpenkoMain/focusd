//go:build windows

package winsys

import (
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Тесты идут по своей ветке HKCU: настоящие настройки прокси пользователя
// трогать нельзя, а права администратора для HKCU не нужны.
const testInternetSettingsKey = `Software\Focusd\Tests\Internet Settings`

func withTestSystemProxy(t *testing.T) {
	t.Helper()
	saved := systemProxyKeyPath
	systemProxyKeyPath = testInternetSettingsKey
	clearTestSystemProxy(t)
	t.Cleanup(func() {
		clearTestSystemProxy(t)
		systemProxyKeyPath = saved
	})
	// Проверяем доступность реестра до самого теста: в ограниченном окружении
	// (песочница, чужой профиль) эти тесты должны пропускаться так же дружно,
	// как и соседние, а не падать поодиночке.
	probe, _, err := registry.CreateKey(registry.CURRENT_USER, testInternetSettingsKey,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("реестр недоступен: %v", err)
	}
	probe.Close()
}

func clearTestSystemProxy(t *testing.T) {
	t.Helper()
	if err := registry.DeleteKey(registry.CURRENT_USER, testInternetSettingsKey); err != nil &&
		err != registry.ErrNotExist {
		t.Skipf("реестр недоступен: %v", err)
	}
}

func seedSystemProxy(t *testing.T, enable uint32, server, pac string) {
	t.Helper()
	k, _, err := registry.CreateKey(registry.CURRENT_USER, testInternetSettingsKey,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("реестр недоступен: %v", err)
	}
	defer k.Close()
	if err := k.SetDWordValue(systemProxyEnableValue, enable); err != nil {
		t.Fatalf("подготовка ProxyEnable: %v", err)
	}
	if server != "" {
		if err := k.SetStringValue(systemProxyServerValue, server); err != nil {
			t.Fatalf("подготовка ProxyServer: %v", err)
		}
	}
	if pac != "" {
		if err := k.SetStringValue(systemProxyAutoConfigValue, pac); err != nil {
			t.Fatalf("подготовка AutoConfigURL: %v", err)
		}
	}
}

// Главное свойство: после Focusd настройки пользователя возвращаются ровно
// такими, какими были, — включая выключенный прокси и чужой адрес.
func TestSystemProxyRestoresUserSettings(t *testing.T) {
	withTestSystemProxy(t)
	seedSystemProxy(t, 0, "127.0.0.1:2080", "")

	prev, err := SetSystemProxy("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("установка системного прокси: %v", err)
	}
	if got := SystemProxyAddr(); got != "127.0.0.1:47653" {
		t.Fatalf("после установки SystemProxyAddr() = %q", got)
	}
	if prev.Enable || prev.Server != "127.0.0.1:2080" {
		t.Fatalf("снимок снят неверно: %+v", prev)
	}

	if err := RestoreSystemProxy(prev); err != nil {
		t.Fatalf("возврат настроек: %v", err)
	}
	if got := SystemProxyAddr(); got != "" {
		t.Fatalf("после возврата системный прокси всё ещё включён: %q", got)
	}
	now, err := SystemProxySettingsNow()
	if err != nil {
		t.Fatalf("чтение настроек: %v", err)
	}
	if now.Enable || now.Server != "127.0.0.1:2080" {
		t.Fatalf("настройки пользователя не восстановлены: %+v", now)
	}
}

// Значения, которых у пользователя не было, должны именно исчезнуть: оставшись,
// адрес Focusd увёл бы браузеры в мёртвый порт после закрытия приложения.
func TestSystemProxyRestoreRemovesOurValues(t *testing.T) {
	withTestSystemProxy(t)

	prev, err := SetSystemProxy("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("установка системного прокси: %v", err)
	}
	if prev.Server != "" || prev.Enable {
		t.Fatalf("на чистой настройке снимок должен быть пустым: %+v", prev)
	}
	if err := RestoreSystemProxy(prev); err != nil {
		t.Fatalf("возврат настроек: %v", err)
	}

	k, err := registry.OpenKey(registry.CURRENT_USER, testInternetSettingsKey, registry.QUERY_VALUE)
	if err != nil {
		t.Fatalf("ключ настроек пропал целиком: %v", err)
	}
	defer k.Close()
	if _, _, err := k.GetStringValue(systemProxyServerValue); err == nil {
		t.Fatal("адрес прокси Focusd остался в настройках пользователя")
	}
	if v, _, err := k.GetIntegerValue(systemProxyEnableValue); err != nil || v != 0 {
		t.Fatalf("ProxyEnable = %v (%v), ожидалось 0", v, err)
	}
}

// PAC-файл главнее ProxyServer, поэтому на время работы он убирается — и
// обязательно возвращается: это чужая настройка, а не наша.
func TestSystemProxyHidesAndReturnsPac(t *testing.T) {
	withTestSystemProxy(t)
	seedSystemProxy(t, 1, "10.0.0.1:8080", "http://wpad/wpad.dat")

	prev, err := SetSystemProxy("127.0.0.1:47653")
	if err != nil {
		t.Fatalf("установка системного прокси: %v", err)
	}
	now, err := SystemProxySettingsNow()
	if err != nil {
		t.Fatalf("чтение настроек: %v", err)
	}
	if now.AutoConfigURL != "" {
		t.Fatalf("PAC остался и перебил бы наш прокси: %q", now.AutoConfigURL)
	}

	if err := RestoreSystemProxy(prev); err != nil {
		t.Fatalf("возврат настроек: %v", err)
	}
	back, err := SystemProxySettingsNow()
	if err != nil {
		t.Fatalf("чтение настроек: %v", err)
	}
	if back.AutoConfigURL != "http://wpad/wpad.dat" || back.Server != "10.0.0.1:8080" || !back.Enable {
		t.Fatalf("настройки пользователя не восстановлены: %+v", back)
	}
}

// Пустой адрес — это не «прокси без адреса», а ошибка вызова: с ним браузеры
// ушли бы в никуда.
func TestSetSystemProxyRejectsEmptyAddr(t *testing.T) {
	withTestSystemProxy(t)
	if _, err := SetSystemProxy("  "); err == nil {
		t.Fatal("пустой адрес должен быть отвергнут")
	}
}

// Аварийное выключение не должно стирать адрес: снимка настроек уже нет, и
// вернуть значение будет неоткуда. Нужно ровно одно — чтобы прокси перестал
// вести в мёртвый порт.
func TestDisableSystemProxyKeepsAddr(t *testing.T) {
	withTestSystemProxy(t)
	seedSystemProxy(t, 1, "127.0.0.1:47653", "")

	if err := DisableSystemProxy(); err != nil {
		t.Fatalf("выключение системного прокси: %v", err)
	}
	if got := SystemProxyAddr(); got != "" {
		t.Fatalf("после выключения SystemProxyAddr() = %q", got)
	}
	now, err := SystemProxySettingsNow()
	if err != nil {
		t.Fatalf("чтение настроек: %v", err)
	}
	if now.Enable || now.Server != "127.0.0.1:47653" {
		t.Fatalf("адрес пропал вместе с выключением: %+v", now)
	}
}
