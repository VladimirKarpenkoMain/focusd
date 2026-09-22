//go:build windows

package winsys

import (
	"reflect"
	"testing"

	"golang.org/x/sys/windows/registry"
)

func TestNormalizeSitePatterns(t *testing.T) {
	in := []string{
		" YouTube.com ",
		"https://youtube.com/shorts",
		"http://www.TikTok.com",
		"youtube.com",
		"example.com.",
		"",
		"   ",
	}
	want := []string{"youtube.com", "www.tiktok.com", "example.com"}

	got := normalizeSitePatterns(in)
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("normalizeSitePatterns() = %v, ожидалось %v", got, want)
	}
}

// Реестр пишется под HKCU: проверяем логику списка, не трогая настоящие
// политики машины и не требуя прав администратора.
func TestWriteListValuesReplacesOldList(t *testing.T) {
	const path = `Software\Focusd\Tests\URLBlocklist`

	k, _, err := registry.CreateKey(registry.CURRENT_USER, path, registry.SET_VALUE|registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("реестр недоступен: %v", err)
	}
	defer func() {
		k.Close()
		_ = registry.DeleteKey(registry.CURRENT_USER, path)
	}()

	long := []string{"a.com", "b.com", "c.com", "d.com"}
	if err := writeListValues(k, long); err != nil {
		t.Fatalf("запись списка: %v", err)
	}
	if names, _ := k.ReadValueNames(-1); len(names) != len(long) {
		t.Fatalf("записалось %d значений вместо %d", len(names), len(long))
	}

	// Главное свойство: короткий список не должен оставлять хвост от прошлого
	// раза, иначе домены старой сессии останутся заблокированными навсегда.
	if err := writeListValues(k, long[:2]); err != nil {
		t.Fatalf("перезапись списка: %v", err)
	}
	names, _ := k.ReadValueNames(-1)
	if len(names) != 2 {
		t.Fatalf("после сокращения осталось %d значений: %v", len(names), names)
	}
	if v, _, err := k.GetStringValue("1"); err != nil || v != "a.com" {
		t.Fatalf("значение 1 = %q (%v), ожидалось a.com", v, err)
	}
	if _, _, err := k.GetStringValue("3"); err == nil {
		t.Fatal("значение 3 должно было исчезнуть вместе с хвостом списка")
	}

	if err := writeListValues(k, nil); err != nil {
		t.Fatalf("очистка списка: %v", err)
	}
	if names, _ := k.ReadValueNames(-1); len(names) != 0 {
		t.Fatalf("после пустого списка осталось %v", names)
	}
}

// Список браузеров — источник истины сразу для двух политик, и его молчаливое
// сокращение оставило бы часть браузеров неприкрытыми.
func TestChromiumVendorsCoverMainBrowsers(t *testing.T) {
	want := []string{
		`Google\Chrome`,
		`Microsoft\Edge`,
		`BraveSoftware\Brave`,
		`Vivaldi`,
		`YandexBrowser`,
	}
	for _, w := range want {
		found := false
		for _, v := range chromiumVendors {
			if v == w {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("в chromiumVendors нет %q", w)
		}
	}
}
