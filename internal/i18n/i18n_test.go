package i18n

import (
	"strings"
	"testing"
)

// Выбор языка — единственное место, где ошибка тихая: неверный язык не падает и
// не пишет в журнал, он просто показывает человеку чужой текст. Поэтому ветки
// разбора проверяются все, включая мусор в настройке.

func TestKnown(t *testing.T) {
	cases := map[string]bool{
		"ru": true, "en": true, "zh": true,
		"RU": true, " ru ": true,
		"de": false, "auto": false, "": false, "мусор": false,
	}
	for in, want := range cases {
		if _, ok := Known(in); ok != want {
			t.Errorf("Known(%q) = %v, ожидалось %v", in, ok, want)
		}
	}
	// Успешный разбор обязан вернуть нормализованный код, а не исходную строку.
	if got, _ := Known(" ZH "); got != ZH {
		t.Errorf("Known вернул %q, ожидалось %q", got, ZH)
	}
}

func TestResolve(t *testing.T) {
	// Явный выбор сильнее системного: иначе переключатель в настройках не работал
	// бы на машине с другим языком.
	for _, l := range Supported {
		if got := Resolve(string(l), "en-US"); got != l {
			t.Errorf("Resolve(%q, en-US) = %q, ожидалось %q", l, got, l)
		}
	}
	// «auto», пустая настройка (конфигурация прошлых версий) и мусор — всё это
	// решение системы, а не ошибка.
	for _, v := range []string{Auto, "", "мусор"} {
		if got := Resolve(v, "zh-CN"); got != ZH {
			t.Errorf("Resolve(%q, zh-CN) = %q, ожидалось %q", v, got, ZH)
		}
	}
}

func TestFromSystem(t *testing.T) {
	cases := map[string]Lang{
		"ru-RU": RU, "ru": RU, "RUS": RU,
		"zh-CN": ZH, "zh-TW": ZH, "zh_Hans": ZH, "CHS": ZH, "cht": ZH,
		"en-US": EN, "ENU": EN,
		// Всё остальное — английский: его читает хоть кто-то.
		"de-DE": EN, "": EN, "мусор": EN,
	}
	for in, want := range cases {
		if got := FromSystem(in); got != want {
			t.Errorf("FromSystem(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestT(t *testing.T) {
	if got := T(EN, "strictness.lock"); got != "Locked" {
		t.Errorf("T(EN, lock) = %q", got)
	}
	if got := T(ZH, "strictness.lock"); got != "锁定" {
		t.Errorf("T(ZH, lock) = %q", got)
	}
	// Подстановка аргументов: строка с %d обязана получить число.
	got := T(EN, "app.cancel.wait", 45)
	if !strings.Contains(got, "45") || strings.Contains(got, "%!") {
		t.Errorf("T с аргументом = %q", got)
	}
	// Ключ, которого нет ни в одном языке, возвращается как есть: так вызывающий
	// код может передать готовый текст и получить его обратно.
	if got := T(RU, "нет-такого-ключа"); got != "нет-такого-ключа" {
		t.Errorf("T для неизвестного ключа = %q", got)
	}
	// Пустой и незнакомый язык — это русский: он исходный, и код без языка
	// обязан получить осмысленный текст, а не английский по случайности.
	for _, l := range []Lang{"", "fr"} {
		if got := T(l, "blockpage.mode"); got != "Режим" {
			t.Errorf("T(%q, mode) = %q, ожидалось %q", l, got, "Режим")
		}
	}
	// Перевод есть только в английском, а просят китайский: пустая подпись хуже
	// английской, поэтому она берётся из английского.
	messages[EN]["test.only-english"] = "fallback"
	t.Cleanup(func() { delete(messages[EN], "test.only-english") })
	if got := T(ZH, "test.only-english"); got != "fallback" {
		t.Errorf("T без перевода в языке = %q, ожидалось %q", got, "fallback")
	}
}

func TestOr(t *testing.T) {
	// Перевода нет — возвращается исходная строка, а не английская подстановка:
	// русский текст живёт в своём пакете и подменять его нельзя.
	if got := Or(RU, "group.social.title", "Соцсети"); got != "Соцсети" {
		t.Errorf("Or(RU) = %q, ожидалось %q", got, "Соцсети")
	}
	if got := Or(EN, "strictness.soft", "Мягкий"); got != "Soft" {
		t.Errorf("Or(EN) = %q, ожидалось %q", got, "Soft")
	}
	// Язык не передан: подставляется исходный текст, а не английский перевод.
	if got := Or("", "group.social.title", "Соцсети"); got != "Соцсети" {
		t.Errorf("Or без языка = %q, ожидалось %q", got, "Соцсети")
	}
}

func TestGroupTitles(t *testing.T) {
	if got := GroupTitle(EN, "social", "Соцсети"); got != "Social networks" {
		t.Errorf("GroupTitle(EN, social) = %q", got)
	}
	if got := GroupTitle(RU, "social", "Соцсети"); got != "Соцсети" {
		t.Errorf("GroupTitle(RU, social) = %q", got)
	}
	if got := GroupNote(ZH, "social", "Ленты"); got != "信息流、动态、无限滚动" {
		t.Errorf("GroupNote(ZH, social) = %q", got)
	}
	// Группа без перевода обязана остаться со своим исходным текстом.
	if got := GroupNote(EN, "не-такая-группа", "исходный"); got != "исходный" {
		t.Errorf("GroupNote для неизвестной группы = %q", got)
	}
}

func TestStrictness(t *testing.T) {
	for in, want := range map[string]string{
		"soft":   "Мягкий",
		"strict": "Строгий",
		"lock":   "Жёсткий",
		"мусор":  "—",
		"":       "—",
	} {
		if got := Strictness(RU, in); got != want {
			t.Errorf("Strictness(RU, %q) = %q, ожидалось %q", in, got, want)
		}
	}
	// Уровень, неизвестный самому приложению, обязан дать прочерк в любом языке:
	// пустое место в строке «Режим» выглядит поломкой.
	if got := Strictness(ZH, "мусор"); got != "—" {
		t.Errorf("Strictness(ZH, мусор) = %q", got)
	}
}

func TestLocale(t *testing.T) {
	for in, want := range map[Lang]string{RU: "ru", EN: "en", ZH: "zh-CN"} {
		if got := Locale(in); got != want {
			t.Errorf("Locale(%q) = %q, ожидалось %q", in, got, want)
		}
	}
	// Незнакомый язык не должен оставить страницу без lang вовсе.
	if got := Locale(Lang("fr")); got != "ru" {
		t.Errorf("Locale(fr) = %q", got)
	}
}

// Таблицы обязаны совпадать по набору ключей: пропущенный перевод виден только
// глазами, а нашёлся бы он именно здесь.
func TestTablesHaveSameKeys(t *testing.T) {
	if len(messages[EN]) != len(messages[ZH]) {
		t.Fatalf("в таблицах разное число строк: en %d, zh %d", len(messages[EN]), len(messages[ZH]))
	}
	for key := range messages[EN] {
		if messages[ZH][key] == "" {
			t.Errorf("в китайской таблице нет ключа %q", key)
		}
	}
	// Каталог — единственное место, где ключи групп задаются списком: здесь он и
	// сверяется, чтобы новая группа не осталась без перевода молча.
	for _, id := range []string{
		"social", "video", "shorts", "games", "messengers",
		"news", "shopping", "adult", "doomscroll",
	} {
		for _, l := range []Lang{EN, ZH} {
			if messages[l]["group."+id+".title"] == "" || messages[l]["group."+id+".note"] == "" {
				t.Errorf("группа %q не переведена на %q", id, l)
			}
		}
	}
}

// Поддерживаемые языки обязаны быть непустым списком без дублей: по нему
// строится переключатель в интерфейсе.
func TestSupported(t *testing.T) {
	if len(Supported) != 3 {
		t.Fatalf("ожидалось три языка, получено %d", len(Supported))
	}
	seen := map[Lang]bool{}
	for _, l := range Supported {
		if l == "" || seen[l] {
			t.Errorf("язык %q пуст или повторяется", l)
		}
		seen[l] = true
	}
}
