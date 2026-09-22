//go:build windows

package winsys

import (
	"errors"
	"fmt"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Политика ProxySettings направляет трафик браузеров через локальный прокси
// Focusd.
//
// Почему прокси, а не URLBlocklist. URLBlocklist надёжнее — он обрывает запрос
// до сети, — но он же делает блокировку ненаблюдаемой: снаружи нельзя узнать,
// сколько запросов отсечено и какие сайты пытались открыть. Прокси стоит на
// пути трафика и видит каждую попытку, поэтому именно он отвечает за подсчёт.
// Проверить обход он не даёт: DoH и VPN не помогают, потому что браузер
// обращается к прокси раньше, чем куда-либо ещё.
//
// Loopback-адреса Chromium обходит по умолчанию, поэтому страница-заглушка и
// локальные сервисы через прокси не идут — отдельный ProxyBypassList не нужен.
const (
	proxySettingsKey = `ProxySettings`
	proxyModeValue   = "ProxyMode"
	proxyServerValue = "ProxyServer"
	// fixedServersMode — режим «всегда один и тот же прокси».
	fixedServersMode = "fixed_servers"
)

// SetBrowserProxy прописывает браузерам локальный прокси. Изменение
// подхватывается Chromium на ходу — перезапускать браузер не нужно.
func SetBrowserProxy(addr string) error {
	if strings.TrimSpace(addr) == "" {
		return errors.New("не задан адрес прокси")
	}
	var errs []error
	for _, vendor := range chromiumVendors {
		path := vendor + `\` + proxySettingsKey
		if err := writePolicy(path, proxyModeValue, "sz", fixedServersMode); err != nil {
			errs = append(errs, err)
		}
		if err := writePolicy(path, proxyServerValue, "sz", addr); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// ClearBrowserProxy снимает политику прокси. Вызывается при завершении сессии,
// аварийном восстановлении и сторожевой задачей: прокси, который держат в
// политике, но не подняли, оставил бы браузер без интернета.
func ClearBrowserProxy() error {
	var errs []error
	for _, vendor := range chromiumVendors {
		err := deleteRegKey(registry.LOCAL_MACHINE, policyRoot+vendor+`\`+proxySettingsKey)
		if err != nil && !errors.Is(err, registry.ErrNotExist) {
			errs = append(errs, fmt.Errorf("политика прокси %s: %w", vendor, err))
		}
	}
	return errors.Join(errs...)
}

// BrowserProxyAddr возвращает адрес прокси, прописанный браузерам, если он
// есть. Состояние читается из реестра, а не из памяти приложения: так оно не
// разойдётся с действительностью, если политику снимут извне.
func BrowserProxyAddr() string {
	for _, vendor := range chromiumVendors {
		k, err := openRegKey(registry.LOCAL_MACHINE,
			policyRoot+vendor+`\`+proxySettingsKey, registry.QUERY_VALUE)
		if err != nil {
			continue
		}
		mode, _, errMode := k.GetStringValue(proxyModeValue)
		server, _, errServer := k.GetStringValue(proxyServerValue)
		k.Close()
		if errMode == nil && errServer == nil && mode == fixedServersMode && server != "" {
			return server
		}
	}
	return ""
}
