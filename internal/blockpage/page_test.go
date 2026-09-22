package blockpage

import (
	"errors"
	"strings"
	"testing"
)

// Активная сессия: страница обязана показать таймер и режим — это то, ради
// чего человек вообще её видит.
func TestRenderActiveSession(t *testing.T) {
	var b strings.Builder
	err := Render(&b, Info{
		Active:     true,
		Host:       "youtube.com",
		Remaining:  3725, // 1:02:05
		Strictness: "lock",
		Blocked:    42,
	})
	if err != nil {
		t.Fatalf("Render: %v", err)
	}
	page := b.String()

	for _, want := range []string{
		"Ты в фокусе",
		"youtube.com",
		"1:02:05",
		"Жёсткий",
		"42",
		"<meta http-equiv=\"refresh\" content=\"5\">",
	} {
		if !strings.Contains(page, want) {
			t.Errorf("в странице нет %q:\n%s", want, page)
		}
	}
	if strings.Contains(page, "Сессия завершена") {
		t.Error("активная сессия не должна показывать «сессия завершена»")
	}
}

// Сессия кончилась, а человек обновил страницу из кэша: вместо таймера должно
// быть сказано, что блокировка снята.
func TestRenderInactiveSession(t *testing.T) {
	var b strings.Builder
	if err := Render(&b, Info{Active: false, Strictness: "soft", Blocked: 7}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	page := b.String()

	if !strings.Contains(page, "Сессия завершена") || !strings.Contains(page, "Мягкий") {
		t.Errorf("страница без сессии отрисована неверно:\n%s", page)
	}
	if strings.Contains(page, "осталось") {
		t.Error("без сессии таймер показывать нечего")
	}
	// Хост пуст — значит, и плашки с адресом быть не должно.
	if strings.Contains(page, `class="host"`) {
		t.Error("пустой хост не должен попадать на страницу")
	}
}

// Неизвестный уровень строгости обязан превратиться в прочерк, а не в пустое
// место: иначе строка «Режим» выглядит сломанной.
func TestRenderUnknownStrictness(t *testing.T) {
	var b strings.Builder
	if err := Render(&b, Info{Active: true, Strictness: "что-то"}); err != nil {
		t.Fatalf("Render: %v", err)
	}
	if !strings.Contains(b.String(), "—") {
		t.Error("неизвестный режим должен показываться прочерком")
	}
}

func TestStrictnessText(t *testing.T) {
	cases := map[string]string{
		"soft":   "Мягкий",
		"strict": "Строгий",
		"lock":   "Жёсткий",
		"":       "—",
		"мусор":  "—",
	}
	for in, want := range cases {
		if got := strictnessText(in); got != want {
			t.Errorf("strictnessText(%q) = %q, ожидалось %q", in, got, want)
		}
	}
}

func TestHHMMSS(t *testing.T) {
	cases := []struct {
		sec  int
		want string
	}{
		{0, "00:00"},
		{59, "00:59"},
		{60, "01:00"},
		{3599, "59:59"},
		{3600, "1:00:00"},
		{3725, "1:02:05"},
		{86399, "23:59:59"},
		// Отрицательное время — не «минус», а ноль: у сессии не бывает
		// отрицательного остатка, но подставить его сюда могут.
		{-1, "00:00"},
	}
	for _, c := range cases {
		if got := hhmmss(c.sec); got != c.want {
			t.Errorf("hhmmss(%d) = %q, ожидалось %q", c.sec, got, c.want)
		}
	}
}

// Ошибка записи должна вернуться наружу: молча отданная пустая страница
// выглядела бы как «блокировка не работает».
func TestRenderPropagatesWriteError(t *testing.T) {
	errBoom := errors.New("обрыв")
	if err := Render(errWriter{errBoom}, Info{Active: true}); !errors.Is(err, errBoom) {
		t.Fatalf("Render вернул %v, ожидалась ошибка записи", err)
	}
}

type errWriter struct{ err error }

func (w errWriter) Write([]byte) (int, error) { return 0, w.err }
