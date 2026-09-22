//go:build windows

package winsys

import (
	"errors"
	"fmt"
	"strconv"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// Блокировка сайтов политиками браузера.
//
// Политика URLBlocklist действует внутри браузера и обрывает запрос до того,
// как он уйдёт в сеть. Поэтому ей не мешают ни DoH, ни VPN, ни занятый 53-й
// порт — в отличие от перехвата DNS. Плата за это — охват: политику читают
// только браузеры на движке Chromium, и вместо нашей страницы-заглушки
// браузер показывает своё сообщение о блокировке.
const (
	blocklistValue = "URLBlocklist"
	allowlistValue = "URLAllowlist"
)

// BlockSitesInBrowsers прописывает браузерам списки запрещённых и разрешённых
// адресов. Пустой список исключений означает, что исключений нет.
//
// Правило из одного имени накрывает домен целиком вместе с поддоменами:
// "youtube.com" закрывает и www., и m., и music. Это документированное
// поведение URLBlocklist, а не наше допущение, поэтому расширять имена вручную
// не нужно.
func BlockSitesInBrowsers(domains, allow []string) error {
	blocked := normalizeSitePatterns(domains)
	allowed := normalizeSitePatterns(allow)

	var errs []error
	for _, vendor := range chromiumVendors {
		if err := writePolicyList(vendor+`\`+blocklistValue, blocked); err != nil {
			errs = append(errs, err)
		}
		if err := writePolicyList(vendor+`\`+allowlistValue, allowed); err != nil {
			errs = append(errs, err)
		}
	}
	return errors.Join(errs...)
}

// UnblockSitesInBrowsers снимает списки и возвращает браузеры в обычное
// состояние. Вызывается при завершении сессии и при аварийном восстановлении.
func UnblockSitesInBrowsers() error {
	var errs []error
	for _, vendor := range chromiumVendors {
		for _, name := range []string{blocklistValue, allowlistValue} {
			if err := clearPolicyList(vendor + `\` + name); err != nil {
				errs = append(errs, err)
			}
		}
	}
	return errors.Join(errs...)
}

// BrowsersBlocked сообщает, действует ли блокировка сайтов хотя бы в одном
// браузере. Состояние читается из реестра, а не из памяти приложения: так оно
// не разойдётся с действительностью, если политику снимут извне.
//
// Учитывает и прокси, и список. Для признака «сайты сейчас закрыты» этого мало:
// политика прокси прописана браузерам всегда, пока работает Focusd, а режет
// только во время сессии. Поэтому рядом есть SitesBlockedInBrowsers.
func BrowsersBlocked() bool {
	if BrowserProxyAddr() != "" {
		return true
	}
	return SitesBlockedInBrowsers()
}

// SitesBlockedInBrowsers сообщает, действует ли именно список запрещённых
// адресов — запасной слой, который включается на время сессии.
func SitesBlockedInBrowsers() bool {
	for _, vendor := range chromiumVendors {
		if policyListLen(vendor+`\`+blocklistValue) > 0 {
			return true
		}
	}
	return false
}

// browserProcesses сопоставляет исполняемые файлы браузеров с понятными именами.
var browserProcesses = []struct{ exe, name string }{
	{"chrome.exe", "Chrome"},
	{"msedge.exe", "Edge"},
	{"browser.exe", "Яндекс Браузер"},
	{"brave.exe", "Brave"},
	{"vivaldi.exe", "Vivaldi"},
	{"opera.exe", "Opera"},
}

// RunningBrowsers возвращает имена запущенных браузеров, которые читают наши
// политики.
//
// Браузер подхватывает политику при запуске, поэтому об уже открытом окне надо
// сказать прямо. Иначе человек нажимает «Начать фокус», ничего не происходит —
// и он решает, что приложение не работает, хотя достаточно перезапустить браузер.
func RunningBrowsers(ctx contextT) []string {
	out, err := runExternal(ctx, "tasklist", "/FO", "CSV", "/NH")
	if err != nil {
		return nil
	}
	lower := strings.ToLower(out)
	var found []string
	for _, b := range browserProcesses {
		if strings.Contains(lower, `"`+b.exe+`"`) {
			found = append(found, b.name)
		}
	}
	return found
}

// normalizeSitePatterns приводит домены к виду, который понимает политика:
// без схемы, пути и завершающей точки, в нижнем регистре, без повторов.
// Порядок сохраняется, чтобы список в реестре был предсказуемым.
func normalizeSitePatterns(in []string) []string {
	seen := make(map[string]bool, len(in))
	out := make([]string, 0, len(in))
	for _, raw := range in {
		v := strings.ToLower(strings.TrimSpace(raw))
		v = strings.TrimPrefix(v, "https://")
		v = strings.TrimPrefix(v, "http://")
		if i := strings.IndexAny(v, "/?#"); i >= 0 {
			v = v[:i]
		}
		v = strings.TrimSuffix(v, ".")
		if v == "" || seen[v] {
			continue
		}
		seen[v] = true
		out = append(out, v)
	}
	return out
}

func writePolicyList(path string, values []string) error {
	if len(values) == 0 {
		return clearPolicyList(path)
	}
	k, _, err := createRegKey(registry.LOCAL_MACHINE, policyRoot+path,
		registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		return fmt.Errorf("политика %s: %w", path, err)
	}
	defer k.Close()
	return writeListValues(k, values)
}

// writeListValues заменяет содержимое ключа списком values.
//
// Старые значения обязательно удаляются: список прошлой сессии мог быть
// длиннее, и без чистки его домены остались бы заблокированными навсегда.
func writeListValues(k regKey, values []string) error {
	if names, err := k.ReadValueNames(-1); err == nil {
		for _, n := range names {
			_ = k.DeleteValue(n)
		}
	}
	for i, v := range values {
		if err := k.SetStringValue(strconv.Itoa(i+1), v); err != nil {
			return fmt.Errorf("значение %d: %w", i+1, err)
		}
	}
	return nil
}

func clearPolicyList(path string) error {
	err := deleteRegKey(registry.LOCAL_MACHINE, policyRoot+path)
	if err != nil && !errors.Is(err, registry.ErrNotExist) {
		return fmt.Errorf("политика %s: %w", path, err)
	}
	return nil
}

func policyListLen(path string) int {
	k, err := openRegKey(registry.LOCAL_MACHINE, policyRoot+path, registry.QUERY_VALUE)
	if err != nil {
		return 0
	}
	defer k.Close()
	names, err := k.ReadValueNames(-1)
	if err != nil {
		return 0
	}
	return len(names)
}
