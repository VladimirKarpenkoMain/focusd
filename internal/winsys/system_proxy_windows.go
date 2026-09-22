//go:build windows

package winsys

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

// Системный прокси Windows (настройки WinINET).
//
// Зачем он нужен, если браузерам уже прописана политика ProxySettings.
// Потому что политику читают не все браузеры. Яндекс Браузер, например, кладёт
// её по своему задокументированному пути HKLM\SOFTWARE\Policies\YandexBrowser и
// всё равно ходит напрямую: проверено на живом браузере — свежий экземпляр с
// нашей политикой не открыл ни одного соединения с прокси Focusd. А системный
// прокси читают все, кто вообще умеет ходить через прокси, и — главное —
// подхватывают его на ходу: у уже запущенного Яндекса после смены системного
// прокси соединения пошли через Focusd без всякого перезапуска.
//
// Плата за это — охват шире браузеров: через системный прокси пойдут и другие
// программы пользователя. Вне сессии прокси Focusd пропускает всё, поэтому
// заметно это только во время фокуса. Прежние настройки сохраняются и
// возвращаются: при выходе, аварийным восстановлением и сторожевой задачей.
// systemProxySettingsKey — путь к системному прокси в HKCU. Именно эти
// настройки читает Яндекс Браузер и любая другая программа, ходящая через
// системный прокси.
const systemProxySettingsKey = `Software\Microsoft\Windows\CurrentVersion\Internet Settings`

const (
	// Имена значений системного прокси. Совпадают с именами в политике
	// браузеров, но лежат в другом месте реестра, поэтому и константы разные:
	// спутать их — значит прописать прокси не туда.
	systemProxyEnableValue     = "ProxyEnable"
	systemProxyServerValue     = "ProxyServer"
	systemProxyAutoConfigValue = "AutoConfigURL"
)

// systemProxyKeyPath вынесен в переменную ради тестов: они прогоняют снимок и
// восстановление на своей ветке реестра, не трогая настройки пользователя.
var systemProxyKeyPath = systemProxySettingsKey

// SystemProxySettings — настройки системного прокси, снятые перед подменой.
type SystemProxySettings struct {
	Enable        bool
	Server        string
	AutoConfigURL string
}

// SetSystemProxy направляет системный прокси на Focusd и возвращает прежние
// настройки — их нужно сохранить и вернуть при выходе.
func SetSystemProxy(addr string) (SystemProxySettings, error) {
	if strings.TrimSpace(addr) == "" {
		return SystemProxySettings{}, errors.New("не задан адрес прокси")
	}

	prev, err := SystemProxySettingsNow()
	if err != nil {
		return SystemProxySettings{}, err
	}

	k, _, err := createRegKey(registry.CURRENT_USER, systemProxyKeyPath,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return SystemProxySettings{}, fmt.Errorf("системный прокси: %w", err)
	}
	defer k.Close()

	if err := k.SetDWordValue(systemProxyEnableValue, 1); err != nil {
		return SystemProxySettings{}, fmt.Errorf("системный прокси: %w", err)
	}
	if err := k.SetStringValue(systemProxyServerValue, addr); err != nil {
		return SystemProxySettings{}, fmt.Errorf("системный прокси: %w", err)
	}
	if prev.AutoConfigURL != "" {
		if err := k.DeleteValue(systemProxyAutoConfigValue); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return SystemProxySettings{}, fmt.Errorf("системный прокси: %w", err)
		}
	}

	notifyProxyChanged()
	return prev, nil
}

// RestoreSystemProxy возвращает настройки, снятые SetSystemProxy.
//
// Пустая строка означает, что значения в реестре не было, — тогда его нужно
// именно удалить: оставленный адрес Focusd увёл бы браузеры в мёртвый порт
// после закрытия приложения.
func RestoreSystemProxy(prev SystemProxySettings) error {
	k, _, err := createRegKey(registry.CURRENT_USER, systemProxyKeyPath,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("системный прокси: %w", err)
	}
	defer k.Close()

	enable := uint32(0)
	if prev.Enable {
		enable = 1
	}
	if err := k.SetDWordValue(systemProxyEnableValue, enable); err != nil {
		return fmt.Errorf("системный прокси: %w", err)
	}
	if err := restoreStringValue(k, systemProxyServerValue, prev.Server); err != nil {
		return err
	}
	if err := restoreStringValue(k, systemProxyAutoConfigValue, prev.AutoConfigURL); err != nil {
		return err
	}

	notifyProxyChanged()
	return nil
}

// DisableSystemProxy выключает системный прокси, не трогая записанный в реестре
// адрес.
//
// Так поступают, когда вернуть прежние настройки нечем: снимок потеряли, а
// включённый прокси ведёт в мёртвый порт. Выключенная настройка безопасна,
// включённая — оставляет человека без интернета вовсе.
func DisableSystemProxy() error {
	k, _, err := createRegKey(registry.CURRENT_USER, systemProxyKeyPath,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("системный прокси: %w", err)
	}
	defer k.Close()

	if err := k.SetDWordValue(systemProxyEnableValue, 0); err != nil {
		return fmt.Errorf("системный прокси: %w", err)
	}
	notifyProxyChanged()
	return nil
}

// SystemProxySettingsNow читает текущие настройки. Отсутствие ключа или
// значений — не ошибка: это и есть «прокси не настроен».
func SystemProxySettingsNow() (SystemProxySettings, error) {
	k, err := openRegKey(registry.CURRENT_USER, systemProxyKeyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return SystemProxySettings{}, nil
	}
	if err != nil {
		return SystemProxySettings{}, fmt.Errorf("системный прокси: %w", err)
	}
	defer k.Close()

	var out SystemProxySettings
	if v, _, err := k.GetIntegerValue(systemProxyEnableValue); err == nil {
		out.Enable = v != 0
	}
	if v, _, err := k.GetStringValue(systemProxyServerValue); err == nil {
		out.Server = v
	}
	if v, _, err := k.GetStringValue(systemProxyAutoConfigValue); err == nil {
		out.AutoConfigURL = v
	}
	return out, nil
}

// SystemProxyAddr возвращает адрес включённого системного прокси. Пустая строка
// означает, что системный прокси выключен или настроен не нами: состояние
// читается из реестра, а не из памяти приложения, и не разойдётся с
// действительностью, если настройки поменяют извне.
func SystemProxyAddr() string {
	st, err := SystemProxySettingsNow()
	if err != nil || !st.Enable {
		return ""
	}
	return st.Server
}

func restoreStringValue(k regKey, name, value string) error {
	if value == "" {
		if err := k.DeleteValue(name); err != nil && !errors.Is(err, registry.ErrNotExist) {
			return fmt.Errorf("системный прокси: %w", err)
		}
		return nil
	}
	if err := k.SetStringValue(name, value); err != nil {
		return fmt.Errorf("системный прокси: %w", err)
	}
	return nil
}

// notifyProxyChanged сообщает уже запущенным программам, что настройки прокси
// изменились. Chromium замечает смену сам, но слой заведён ради тех, кто сам не
// замечает: без этого уведомления они продолжат ходить по-старому до
// перезапуска. Ошибки здесь не важны — уведомление не единственный путь, а
// сбой wininet не повод считать, что прокси не прописан.
func notifyProxyChanged() {
	const (
		internetOptionRefresh         = 37
		internetOptionSettingsChanged = 39
	)
	wininet := windows.NewLazySystemDLL("wininet.dll")
	setOption := wininet.NewProc("InternetSetOptionW")
	_, _, _ = setOption.Call(0, internetOptionSettingsChanged, 0, 0)
	_, _, _ = setOption.Call(0, internetOptionRefresh, 0, 0)
}
