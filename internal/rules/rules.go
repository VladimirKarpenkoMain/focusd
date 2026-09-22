// Package rules хранит набор доменов для блокировки и умеет сопоставлять с ними
// имена из DNS-запросов.
package rules

import (
	"strings"
	"sync/atomic"
)

// Set — неизменяемый после создания набор доменных правил.
// Сопоставление идёт по суффиксу на границе метки: правило «instagram.com»
// ловит и «instagram.com», и «www.instagram.com», но не «notinstagram.com».
type Set struct {
	domains map[string]struct{}
}

// New собирает набор из списка доменов, отбрасывая мусор и дубликаты.
func New(domains []string) *Set {
	s := &Set{domains: make(map[string]struct{}, len(domains))}
	for _, d := range domains {
		if n := Normalize(d); n != "" {
			s.domains[n] = struct{}{}
		}
	}
	return s
}

// Normalize приводит домен к каноническому виду: нижний регистр, без точки на
// конце, без схемы, пути и порта. Возвращает "" для заведомо непригодных значений.
func Normalize(d string) string {
	d = strings.TrimSpace(strings.ToLower(d))
	if d == "" {
		return ""
	}
	if i := strings.Index(d, "://"); i >= 0 {
		d = d[i+3:]
	}
	if i := strings.IndexAny(d, "/?#"); i >= 0 {
		d = d[:i]
	}
	if i := strings.LastIndex(d, ":"); i >= 0 && !strings.Contains(d[i:], "]") {
		d = d[:i]
	}
	d = strings.Trim(d, ".")
	if d == "" || !strings.Contains(d, ".") && !strings.HasPrefix(d, "*.") {
		return ""
	}
	d = strings.TrimPrefix(d, "*.")
	// Отсекаем заведомо неверные символы, чтобы не засорять набор.
	for _, r := range d {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '.' || r == '-' || r == '_' {
			continue
		}
		return ""
	}
	if strings.Count(d, ".") < 1 {
		return ""
	}
	return d
}

// Match сообщает, заблокировано ли имя. Ожидается уже нормализованное имя.
func (s *Set) Match(name string) bool {
	if s == nil || len(s.domains) == 0 {
		return false
	}
	name = Normalize(name)
	if name == "" {
		return false
	}
	// Проверяем имя и каждый его родительский домен: a.b.c -> a.b.c, b.c, c.
	// Cut вместо IndexByte: он честно сообщает, что точки в имени не осталось,
	// и срез не может выйти за границы даже если Normalize когда-нибудь
	// перестанет требовать точку.
	for {
		if _, ok := s.domains[name]; ok {
			return true
		}
		_, parent, hasDot := strings.Cut(name, ".")
		if !hasDot || !strings.Contains(parent, ".") {
			// Не даём правилу из одной метки заблокировать целую зону верхнего уровня.
			return false
		}
		name = parent
	}
}

// Len возвращает количество правил.
func (s *Set) Len() int {
	if s == nil {
		return 0
	}
	return len(s.domains)
}

// List возвращает правила в виде среза (порядок не определён).
func (s *Set) List() []string {
	if s == nil {
		return nil
	}
	out := make([]string, 0, len(s.domains))
	for d := range s.domains {
		out = append(out, d)
	}
	return out
}

// Atomic — потокобезопасная ссылка на Set, пригодная для горячей замены правил
// без блокировок на пути обработки DNS-запроса.
type Atomic struct {
	p atomic.Pointer[Set]
}

// Store заменяет текущий набор.
func (a *Atomic) Store(s *Set) { a.p.Store(s) }

// Load возвращает текущий набор (может быть nil).
func (a *Atomic) Load() *Set { return a.p.Load() }

// Match проверяет имя по текущему набору.
func (a *Atomic) Match(name string) bool {
	s := a.p.Load()
	if s == nil {
		return false
	}
	return s.Match(name)
}

// Len возвращает число правил в текущем наборе.
func (a *Atomic) Len() int { return a.p.Load().Len() }
