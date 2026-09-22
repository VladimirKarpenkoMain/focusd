// Package focus управляет жизненным циклом сессии фокусировки.
package focus

import (
	"errors"
	"fmt"
	"time"

	"focusd/internal/config"
)

var (
	// ErrAlreadyActive — попытка начать вторую сессию.
	ErrAlreadyActive = errors.New("сессия уже идёт")
	// ErrNotActive — операция над несуществующей сессией.
	ErrNotActive = errors.New("активной сессии нет")
	// ErrLocked — жёсткую сессию прервать нельзя.
	ErrLocked = errors.New("жёсткую сессию нельзя прервать до конца")
	// ErrTooEarly — отмена ещё не разблокирована.
	ErrTooEarly = errors.New("отмена пока недоступна")
	// ErrBadDuration — некорректная длительность.
	ErrBadDuration = errors.New("длительность должна быть от 1 минуты до 12 часов")
)

// View — представление сессии для интерфейса.
type View struct {
	Active      bool              `json:"active"`
	Strictness  config.Strictness `json:"strictness"`
	StartedAt   int64             `json:"startedAt"`
	EndsAt      int64             `json:"endsAt"`
	DurationSec int               `json:"durationSec"`
	Remaining   int               `json:"remaining"`
	Cancelable  bool              `json:"cancelable"`
	CancelAt    int64             `json:"cancelAt"`
	Label       string            `json:"label"`
}

// Options — параметры запускаемой сессии.
type Options struct {
	DurationMin   int
	Strictness    config.Strictness
	Groups        []string
	CustomDomains []string
	Allowlist     []string
	Label         string
}

// Manager читает и пишет состояние сессии через хранилище конфигурации.
type Manager struct {
	store *config.Store
}

// New создаёт менеджер сессий.
func New(store *config.Store) *Manager {
	return &Manager{store: store}
}

// Current возвращает вид сессии. Истёкшая сессия снимается с учёта.
func (m *Manager) Current(now time.Time) View {
	sess, ok := m.Active(now)
	if !ok {
		return View{}
	}
	return view(*sess, now)
}

// Active возвращает сохранённую сессию, если она ещё идёт. Если срок вышел,
// сессия удаляется из хранилища и возвращается false.
func (m *Manager) Active(now time.Time) (*config.Session, bool) {
	f := m.store.Get()
	if f.Session == nil {
		return nil, false
	}
	if now.Unix() >= f.Session.EndsAt {
		_ = m.store.Update(func(file *config.File) error {
			file.Session = nil
			return nil
		})
		return nil, false
	}
	return f.Session, true
}

// Start открывает новую сессию.
func (m *Manager) Start(o Options, now time.Time) error {
	if o.DurationMin < 1 || o.DurationMin > 12*60 {
		return ErrBadDuration
	}
	if !o.Strictness.Valid() {
		o.Strictness = config.Strict
	}

	return m.store.Update(func(f *config.File) error {
		if f.Session != nil && now.Unix() < f.Session.EndsAt {
			return ErrAlreadyActive
		}
		dur := time.Duration(o.DurationMin) * time.Minute
		sess := &config.Session{
			Strictness:    o.Strictness,
			StartedAt:     now.Unix(),
			EndsAt:        now.Add(dur).Unix(),
			DurationSec:   int(dur.Seconds()),
			Groups:        append([]string(nil), o.Groups...),
			CustomDomains: append([]string(nil), o.CustomDomains...),
			Allowlist:     append([]string(nil), o.Allowlist...),
			Label:         o.Label,
		}
		if sess.Label == "" {
			sess.Label = defaultLabel(o.DurationMin)
		}
		// Мягкая сессия снимается сразу, строгая — после выдержки.
		sess.CancelUnlockAt = sess.StartedAt
		if o.Strictness == config.Strict {
			sess.CancelUnlockAt = now.Add(config.StrictUnlockDelay).Unix()
		}
		f.Session = sess
		return nil
	})
}

// RequestCancel сообщает, сколько секунд осталось до возможности отмены.
// Для жёсткой сессии всегда возвращает ошибку.
func (m *Manager) RequestCancel(now time.Time) (int, error) {
	sess, ok := m.Active(now)
	if !ok {
		return 0, ErrNotActive
	}
	switch sess.Strictness {
	case config.Lock:
		return 0, ErrLocked
	case config.Strict:
		if wait := sess.CancelUnlockAt - now.Unix(); wait > 0 {
			return int(wait), nil
		}
	}
	return 0, nil
}

// ConfirmCancel прерывает сессию, если её уровень строгости это допускает.
func (m *Manager) ConfirmCancel(now time.Time) error {
	sess, ok := m.Active(now)
	if !ok {
		return ErrNotActive
	}
	switch sess.Strictness {
	case config.Lock:
		return ErrLocked
	case config.Strict:
		if now.Unix() < sess.CancelUnlockAt {
			return fmt.Errorf("%w: осталось %d с", ErrTooEarly, sess.CancelUnlockAt-now.Unix())
		}
	}
	return m.clear()
}

// ForceComplete снимает сессию независимо от строгости. Используется только
// аварийным восстановлением и уборкой по истечении срока.
func (m *Manager) ForceComplete() error { return m.clear() }

func (m *Manager) clear() error {
	return m.store.Update(func(f *config.File) error {
		f.Session = nil
		return nil
	})
}

func view(s config.Session, now time.Time) View {
	remaining := int(s.EndsAt - now.Unix())
	if remaining < 0 {
		remaining = 0
	}
	cancelable := s.Strictness != config.Lock && now.Unix() >= s.CancelUnlockAt
	cancelAt := s.CancelUnlockAt
	if s.Strictness == config.Lock {
		cancelAt = 0
	}
	return View{
		Active:      true,
		Strictness:  s.Strictness,
		StartedAt:   s.StartedAt,
		EndsAt:      s.EndsAt,
		DurationSec: s.DurationSec,
		Remaining:   remaining,
		Cancelable:  cancelable,
		CancelAt:    cancelAt,
		Label:       s.Label,
	}
}

func defaultLabel(minutes int) string {
	switch {
	case minutes%60 == 0:
		return fmt.Sprintf("Фокус на %d ч", minutes/60)
	case minutes > 60:
		return fmt.Sprintf("Фокус на %d ч %d мин", minutes/60, minutes%60)
	default:
		return fmt.Sprintf("Фокус на %d мин", minutes)
	}
}
