package focus

import (
	"errors"
	"testing"
	"time"

	"focusd/internal/config"
)

func newStore(t *testing.T) *config.Store {
	t.Helper()
	s, err := config.Open(t.TempDir())
	if err != nil {
		t.Fatalf("не удалось открыть хранилище: %v", err)
	}
	return s
}

func start(t *testing.T, m *Manager, now time.Time, minutes int, st config.Strictness) {
	t.Helper()
	err := m.Start(Options{
		DurationMin: minutes,
		Strictness:  st,
		Groups:      []string{"social", "video"},
	}, now)
	if err != nil {
		t.Fatalf("не удалось начать сессию: %v", err)
	}
}

func TestSoftSessionIsCancelableImmediately(t *testing.T) {
	m := New(newStore(t))
	now := time.Now()
	start(t, m, now, 25, config.Soft)

	v := m.Current(now)
	if !v.Active {
		t.Fatal("сессия должна быть активной")
	}
	if v.Remaining != 25*60 {
		t.Errorf("ожидалось 1500 секунд, получено %d", v.Remaining)
	}
	if !v.Cancelable {
		t.Error("мягкая сессия должна отменяться сразу")
	}
	if wait, err := m.RequestCancel(now); err != nil || wait != 0 {
		t.Errorf("ожидалось ожидание 0, получено %d (%v)", wait, err)
	}
}

func TestStrictSessionRequiresDelay(t *testing.T) {
	m := New(newStore(t))
	now := time.Now()
	start(t, m, now, 30, config.Strict)

	if m.Current(now).Cancelable {
		t.Error("строгая сессия не должна отменяться в первую минуту")
	}

	wait, err := m.RequestCancel(now)
	if err != nil {
		t.Fatalf("RequestCancel вернул ошибку: %v", err)
	}
	if wait <= 0 || wait > int(config.StrictUnlockDelay.Seconds()) {
		t.Errorf("неожиданное ожидание отмены: %d", wait)
	}

	if err := m.ConfirmCancel(now); !errors.Is(err, ErrTooEarly) {
		t.Errorf("ожидалась ошибка ErrTooEarly, получено %v", err)
	}

	later := now.Add(config.StrictUnlockDelay + time.Second)
	if !m.Current(later).Cancelable {
		t.Error("после выдержки отмена должна быть доступна")
	}
	if err := m.ConfirmCancel(later); err != nil {
		t.Fatalf("отмена должна пройти: %v", err)
	}
	if m.Current(later).Active {
		t.Error("сессия должна быть снята")
	}
}

func TestLockSessionCannotBeCancelled(t *testing.T) {
	m := New(newStore(t))
	now := time.Now()
	start(t, m, now, 45, config.Lock)

	if _, err := m.RequestCancel(now); !errors.Is(err, ErrLocked) {
		t.Errorf("ожидалась ошибка ErrLocked, получено %v", err)
	}

	// Даже спустя половину сессии отмена недоступна.
	mid := now.Add(20 * time.Minute)
	if m.Current(mid).Cancelable {
		t.Error("жёсткая сессия не должна становиться отменяемой")
	}
	if err := m.ConfirmCancel(mid); !errors.Is(err, ErrLocked) {
		t.Errorf("ожидалась ошибка ErrLocked, получено %v", err)
	}
	if !m.Current(mid).Active {
		t.Error("сессия должна продолжать идти")
	}
}

func TestExpiredSessionIsCleared(t *testing.T) {
	store := newStore(t)
	m := New(store)
	now := time.Now()
	start(t, m, now, 1, config.Soft)

	after := now.Add(2 * time.Minute)
	if m.Current(after).Active {
		t.Error("истёкшая сессия не должна считаться активной")
	}
	if store.Get().Session != nil {
		t.Error("истёкшая сессия должна удаляться из хранилища")
	}
}

func TestSessionSurvivesRestart(t *testing.T) {
	store := newStore(t)
	now := time.Now()
	start(t, New(store), now, 60, config.Lock)

	// Новый менеджер поверх того же файла — имитация перезапуска приложения.
	restarted := New(store)
	v := restarted.Current(now.Add(5 * time.Minute))
	if !v.Active {
		t.Fatal("сессия должна пережить перезапуск")
	}
	if v.Strictness != config.Lock {
		t.Errorf("строгость должна сохраниться, получено %q", v.Strictness)
	}
	if v.Remaining != 55*60 {
		t.Errorf("ожидалось 3300 секунд, получено %d", v.Remaining)
	}
}

func TestSecondSessionIsRejected(t *testing.T) {
	m := New(newStore(t))
	now := time.Now()
	start(t, m, now, 10, config.Soft)

	if err := m.Start(Options{DurationMin: 5, Strictness: config.Soft}, now); !errors.Is(err, ErrAlreadyActive) {
		t.Errorf("ожидалась ошибка ErrAlreadyActive, получено %v", err)
	}
}

func TestDurationValidation(t *testing.T) {
	m := New(newStore(t))
	now := time.Now()

	for _, minutes := range []int{0, -5, 13 * 60} {
		if err := m.Start(Options{DurationMin: minutes, Strictness: config.Soft}, now); !errors.Is(err, ErrBadDuration) {
			t.Errorf("длительность %d минут должна быть отклонена, получено %v", minutes, err)
		}
	}
}

func TestRequestCancelWithoutSession(t *testing.T) {
	m := New(newStore(t))
	if _, err := m.RequestCancel(time.Now()); !errors.Is(err, ErrNotActive) {
		t.Errorf("ожидалась ошибка ErrNotActive, получено %v", err)
	}
}

func TestForceCompleteIgnoresStrictness(t *testing.T) {
	store := newStore(t)
	m := New(store)
	now := time.Now()
	start(t, m, now, 60, config.Lock)

	if err := m.ForceComplete(); err != nil {
		t.Fatalf("аварийное снятие вернуло ошибку: %v", err)
	}
	if m.Current(now).Active {
		t.Error("после аварийного снятия сессии быть не должно")
	}
}

func TestLabels(t *testing.T) {
	cases := map[int]string{
		25:  "Фокус на 25 мин",
		60:  "Фокус на 1 ч",
		90:  "Фокус на 1 ч 30 мин",
		600: "Фокус на 10 ч",
	}
	for in, want := range cases {
		if got := defaultLabel(in); got != want {
			t.Errorf("defaultLabel(%d) = %q, ожидалось %q", in, got, want)
		}
	}
}

// Неизвестный уровень строгости из интерфейса не должен оставить сессию без
// уровня вовсе: умолчание — строгий, а не «отменяется сразу».
func TestUnknownStrictnessFallsBackToStrict(t *testing.T) {
	m := New(newStore(t))
	now := time.Now()

	if err := m.Start(Options{DurationMin: 10, Strictness: "какая-то"}, now); err != nil {
		t.Fatalf("сессия не началась: %v", err)
	}
	v := m.Current(now)
	if v.Strictness != config.Strict {
		t.Errorf("строгость = %q, ожидалась %q", v.Strictness, config.Strict)
	}
	if v.Cancelable {
		t.Error("строгая сессия не должна отменяться в первую минуту")
	}
}

func TestConfirmCancelWithoutSession(t *testing.T) {
	m := New(newStore(t))
	if err := m.ConfirmCancel(time.Now()); !errors.Is(err, ErrNotActive) {
		t.Errorf("ожидалась ошибка ErrNotActive, получено %v", err)
	}
}

// view — чистая функция, и остаток она сама не даёт увести в минус. Проверяется
// напрямую: через Active такое состояние не получить, истёкшая сессия до неё не
// доходит.
func TestViewClampsNegativeRemaining(t *testing.T) {
	now := time.Unix(1_000_000, 0)
	sess := config.Session{
		Strictness:     config.Soft,
		EndsAt:         now.Add(-time.Minute).Unix(),
		DurationSec:    600,
		CancelUnlockAt: now.Add(-time.Hour).Unix(),
	}
	v := view(sess, now)
	if v.Remaining != 0 {
		t.Errorf("остаток = %d, ожидался 0", v.Remaining)
	}
	if !v.Active || !v.Cancelable {
		t.Errorf("вид сессии собран неверно: %+v", v)
	}
}
