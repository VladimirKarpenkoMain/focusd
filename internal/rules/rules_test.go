package rules

import "testing"

func TestNormalize(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"YouTube.com", "youtube.com"},
		{"  instagram.com  ", "instagram.com"},
		{"https://www.tiktok.com/@user/video/123", "www.tiktok.com"},
		{"http://x.com:443/path", "x.com"},
		{"*.reddit.com", "reddit.com"},
		{"example.com.", "example.com"},
		{"sub.domain.co.uk", "sub.domain.co.uk"},
		{"", ""},
		{"localhost", ""},
		{"не-домен", ""},
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestMatchSuffixOnLabelBoundary(t *testing.T) {
	s := New([]string{"instagram.com", "youtube.com", "googlevideo.com"})

	blocked := []string{
		"instagram.com",
		"www.instagram.com",
		"WWW.Instagram.COM",
		"youtube.com",
		"m.youtube.com",
		"r1---sn-abc.googlevideo.com",
	}
	for _, name := range blocked {
		if !s.Match(name) {
			t.Errorf("%q должен быть заблокирован", name)
		}
	}

	allowed := []string{
		"notinstagram.com",
		"instagram.com.evil.net",
		"myyoutube.com",
		"youtube.co",
		"google.com",
		"example.com",
		"scontent.cdninstagram.com", // правила cdninstagram.com в наборе нет
	}
	for _, name := range allowed {
		if s.Match(name) {
			t.Errorf("%q не должен быть заблокирован", name)
		}
	}
}

func TestCdnSubdomainOwnRule(t *testing.T) {
	s := New([]string{"cdninstagram.com"})
	if !s.Match("scontent.cdninstagram.com") {
		t.Error("поддомен добавленного CDN должен блокироваться")
	}
	if s.Match("cdninstagram.com.evil.net") {
		t.Error("посторонний домен не должен блокироваться")
	}
}

func TestTopLevelZoneIsNotBlockable(t *testing.T) {
	// Правило из одной метки не должно ронять целую зону.
	s := New([]string{"com"})
	if s.Match("youtube.com") {
		t.Error("правило «com» не должно блокировать youtube.com")
	}
}

func TestEmptyAndNilSet(t *testing.T) {
	var nilSet *Set
	if nilSet.Match("youtube.com") {
		t.Error("nil-набор не должен ничего блокировать")
	}
	if New(nil).Match("youtube.com") {
		t.Error("пустой набор не должен ничего блокировать")
	}
	if New(nil).Len() != 0 {
		t.Error("пустой набор должен иметь длину 0")
	}
}

func TestDuplicateDomainsCollapse(t *testing.T) {
	s := New([]string{"x.com", "x.com", "X.COM", "https://x.com/a"})
	if s.Len() != 1 {
		t.Errorf("ожидалось 1 правило, получено %d", s.Len())
	}
}

func TestAtomicSwap(t *testing.T) {
	var a Atomic
	if a.Match("youtube.com") {
		t.Error("пустой Atomic не должен блокировать")
	}
	a.Store(New([]string{"youtube.com"}))
	if !a.Match("www.youtube.com") {
		t.Error("после Store правило должно работать")
	}
	a.Store(New(nil))
	if a.Match("youtube.com") {
		t.Error("после очистки правило не должно работать")
	}
}

func TestNormalizeRejectsGarbage(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"exa mple.com", ""},                         // пробел внутри имени
		{"example.com/path?x=1#frag", "example.com"}, // путь и запрос отсекаются
		{"*.com", ""},                                // правило из одной метки
		{"https://:8080/x", ""},                      // пустое имя с портом
		{"example.com:", "example.com"},              // двоеточие с пустым портом
		{"a.b:80", "a.b"},                            // порт отсекается вместе с хвостом
		{"пример.рф", ""},                            // кириллица не входит в разрешённые символы
	}
	for _, c := range cases {
		if got := Normalize(c.in); got != c.want {
			t.Errorf("Normalize(%q) = %q, ожидалось %q", c.in, got, c.want)
		}
	}
}

func TestMatchRejectsUnusableNames(t *testing.T) {
	s := New([]string{"youtube.com"})
	// Пустое и нераспознаваемое имя не должно совпадать ни с одним правилом.
	for _, name := range []string{"", "   ", "localhost", "не-домен"} {
		if s.Match(name) {
			t.Errorf("Match(%q) не должен совпадать", name)
		}
	}
}

func TestNilSetLenAndList(t *testing.T) {
	var nilSet *Set
	if nilSet.Len() != 0 {
		t.Error("nil-набор должен иметь длину 0")
	}
	if nilSet.List() != nil {
		t.Error("nil-набор должен отдавать nil-список")
	}
}

func TestListReturnsEveryRule(t *testing.T) {
	s := New([]string{"a.com", "b.com", "c.com"})
	got := s.List()
	if len(got) != 3 {
		t.Fatalf("List() вернул %d правил, ожидалось 3: %v", len(got), got)
	}
	want := map[string]bool{"a.com": true, "b.com": true, "c.com": true}
	for _, d := range got {
		if !want[d] {
			t.Errorf("в списке лишнее правило %q", d)
		}
		delete(want, d)
	}
	if len(want) != 0 {
		t.Errorf("в списке нет правил %v", want)
	}
}

func TestAtomicLoadAndLen(t *testing.T) {
	var a Atomic
	if a.Load() != nil {
		t.Error("пустой Atomic должен отдавать nil-набор")
	}
	if a.Len() != 0 {
		t.Error("пустой Atomic должен иметь длину 0")
	}
	a.Store(New([]string{"youtube.com", "x.com"}))
	if a.Load() == nil {
		t.Fatal("после Store набор должен читаться")
	}
	if a.Len() != 2 {
		t.Errorf("Len() = %d, ожидалось 2", a.Len())
	}
	// Store(nil) возвращает Atomic к пустому состоянию, и Len не паникует.
	a.Store(nil)
	if a.Len() != 0 {
		t.Errorf("после Store(nil) Len() = %d, ожидалось 0", a.Len())
	}
}

// Исключение перебивает блокировку — на этом стоит и прокси, и политика
// браузеров: «music.youtube.com» должен открываться при закрытом ютубе.
func TestAllowlistOverridesBlock(t *testing.T) {
	block := New([]string{"youtube.com"})
	allow := New([]string{"music.youtube.com"})

	if !block.Match("music.youtube.com") {
		t.Fatal("поддомен должен попадать под блокировку")
	}
	if !allow.Match("music.youtube.com") {
		t.Fatal("исключение должно совпадать")
	}
	// Именно эту комбинацию проверяет обработчик DNS-запроса.
	blocked := block.Match("music.youtube.com") && !allow.Match("music.youtube.com")
	if blocked {
		t.Error("исключение должно перебивать блокировку")
	}
}
