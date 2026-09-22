// Package config хранит настройки приложения и состояние активной сессии на
// диске, чтобы жёсткую сессию нельзя было снять перезапуском.
package config

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Strictness — уровень строгости сессии.
type Strictness string

const (
	// Soft — сессию можно прервать в любой момент.
	Soft Strictness = "soft"
	// Strict — прервать можно только через StrictUnlockDelay после начала.
	Strict Strictness = "strict"
	// Lock — прервать нельзя до конца сессии.
	Lock Strictness = "lock"
)

// StrictUnlockDelay — сколько нужно выдержать, прежде чем отменить строгую сессию.
const StrictUnlockDelay = 60 * time.Second

// Valid сообщает, известно ли такое значение строгости.
func (s Strictness) Valid() bool {
	switch s {
	case Soft, Strict, Lock:
		return true
	}
	return false
}

// Theme — выбранное оформление интерфейса.
type Theme string

const (
	// ThemeSystem следует за оформлением Windows.
	ThemeSystem Theme = "system"
	// ThemeLight и ThemeDark задают оформление жёстко.
	ThemeLight Theme = "light"
	ThemeDark  Theme = "dark"
)

// Valid сообщает, известно ли такое оформление.
func (t Theme) Valid() bool {
	switch t {
	case ThemeSystem, ThemeLight, ThemeDark:
		return true
	}
	return false
}

// Language — выбранный язык интерфейса.
type Language string

const (
	// LanguageAuto следует за языком Windows. Значение по умолчанию: приложение
	// должно заговорить на языке системы само, а не после похода в настройки.
	LanguageAuto Language = "auto"
	// LanguageRU, LanguageEN и LanguageZH задают язык жёстко.
	LanguageRU Language = "ru"
	LanguageEN Language = "en"
	LanguageZH Language = "zh"
)

// Valid сообщает, известен ли такой выбор языка.
func (l Language) Valid() bool {
	switch l {
	case LanguageAuto, LanguageRU, LanguageEN, LanguageZH:
		return true
	}
	return false
}

// WidgetShape — форма компактного таймера.
type WidgetShape string

const (
	// WidgetBar — плашка с полосой прогресса.
	WidgetBar WidgetShape = "bar"
	// WidgetRing — круг с кольцом прогресса.
	WidgetRing WidgetShape = "ring"
)

// Valid сообщает, известна ли такая форма виджета.
func (w WidgetShape) Valid() bool {
	switch w {
	case WidgetBar, WidgetRing:
		return true
	}
	return false
}

// Settings — пользовательские настройки.
type Settings struct {
	Autostart          bool       `json:"autostart"`
	DefaultDurationMin int        `json:"defaultDurationMin"`
	DefaultStrictness  Strictness `json:"defaultStrictness"`
	DefaultGroups      []string   `json:"defaultGroups"`
	CustomDomains      []string   `json:"customDomains"`
	Allowlist          []string   `json:"allowlist"`
	Theme              Theme      `json:"theme"`
	// Language выбирает язык интерфейса. Пустое значение в конфигурации прошлых
	// версий означает «как в системе» — так же, как у оформления.
	Language Language `json:"language"`

	// Widget — компактный таймер поверх всех окон. Размер окна приложения
	// меняется, поэтому положение запоминается: иначе виджет каждый раз
	// возвращался бы в угол, откуда его только что унесли.
	Widget bool `json:"widget"`
	// WidgetOnTop — держать виджет поверх остальных окон. Отдельно от Widget:
	// таймер бывает нужен и как обычное маленькое окно, которое можно закрыть
	// другими.
	WidgetOnTop bool `json:"widgetOnTop"`
	// WidgetShape — плашка или круг. Форма меняет размер окна, поэтому она
	// хранится рядом с положением: у круга и плашки разные габариты.
	WidgetShape WidgetShape `json:"widgetShape"`
	WidgetX     int         `json:"widgetX"`
	WidgetY     int         `json:"widgetY"`
}

// Session — сохранённое состояние сессии фокуса.
type Session struct {
	Strictness     Strictness `json:"strictness"`
	StartedAt      int64      `json:"startedAt"`
	EndsAt         int64      `json:"endsAt"`
	DurationSec    int        `json:"durationSec"`
	CancelUnlockAt int64      `json:"cancelUnlockAt"`
	Groups         []string   `json:"groups"`
	CustomDomains  []string   `json:"customDomains"`
	Allowlist      []string   `json:"allowlist"`
	Label          string     `json:"label"`
}

// SystemProxyBackup — прежние настройки системного прокси пользователя.
//
// Focusd подменяет их на время своей работы, а вернуть обязан даже после
// аварийного завершения. Поэтому снимок лежит на диске, а не в памяти: сторож и
// режим --restore — отдельные процессы и о памяти приложения ничего не знают.
// Пустая строка означает, что значения у человека не было и его нужно удалить.
type SystemProxyBackup struct {
	Enable        bool   `json:"enable"`
	Server        string `json:"server,omitempty"`
	AutoConfigURL string `json:"autoConfigUrl,omitempty"`
}

// File — содержимое файла конфигурации целиком.
type File struct {
	Settings Settings `json:"settings"`

	// Session хранится, пока сессия активна. Именно этот файл позволяет
	// восстановить жёсткую сессию после перезапуска приложения.
	Session *Session `json:"session,omitempty"`

	// SystemProxy заполнен, пока системный прокси подменён нашим. Пока он
	// здесь, настройки человека считаются не возвращёнными.
	SystemProxy *SystemProxyBackup `json:"systemProxyBackup,omitempty"`

	// Счётчик заблокированных запросов за сутки.
	BlockedDate  string `json:"blockedDate,omitempty"`
	BlockedToday int64  `json:"blockedToday,omitempty"`
}

// DefaultSettings возвращает настройки для первого запуска.
func DefaultSettings() Settings {
	return Settings{
		Autostart:          false,
		DefaultDurationMin: 25,
		DefaultStrictness:  Strict,
		DefaultGroups:      []string{"social", "video", "shorts", "doomscroll"},
		Theme:              ThemeSystem,
		Language:           LanguageAuto,
		WidgetOnTop:        true,
		WidgetShape:        WidgetBar,
	}
}

// Store — доступ к файлу конфигурации с атомарной записью.
type Store struct {
	path string
	mu   sync.Mutex
	file File
}

// Open читает конфигурацию из dir, создавая её при необходимости.
func Open(dir string) (*Store, error) {
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("не удалось создать каталог данных: %w", err)
	}
	s := &Store{path: filepath.Join(dir, "config.json")}
	// Начинаем с умолчаний, а не с нулевого значения: разбор JSON перезапишет
	// только те поля, которые в файле есть. Иначе любой новый параметр
	// оказывался бы выключенным у всех, кто обновился, а на чистой установке
	// молча терялись бы умолчания вроде «держать таймер поверх окон».
	s.file = File{Settings: DefaultSettings()}

	raw, err := os.ReadFile(s.path)
	switch {
	case err == nil:
		if err := json.Unmarshal(raw, &s.file); err != nil {
			// Битый файл не должен блокировать запуск: переносим его в сторону.
			_ = os.Rename(s.path, s.path+".broken")
			s.file = File{Settings: DefaultSettings()}
		}
	case errors.Is(err, os.ErrNotExist):
		s.file = File{Settings: DefaultSettings()}
	default:
		return nil, fmt.Errorf("не удалось прочитать конфигурацию: %w", err)
	}

	if s.file.Settings.DefaultDurationMin <= 0 {
		s.file.Settings.DefaultDurationMin = DefaultSettings().DefaultDurationMin
	}
	if !s.file.Settings.DefaultStrictness.Valid() {
		s.file.Settings.DefaultStrictness = DefaultSettings().DefaultStrictness
	}
	if !s.file.Settings.Theme.Valid() {
		s.file.Settings.Theme = DefaultSettings().Theme
	}
	if !s.file.Settings.WidgetShape.Valid() {
		s.file.Settings.WidgetShape = DefaultSettings().WidgetShape
	}
	// Пустые списки должны уходить в интерфейс как [], а не как null: иначе
	// фронтенд падает на первом же обращении к .length.
	s.file.Settings.fillSlices(DefaultSettings().DefaultGroups)
	return s, nil
}

// fillSlices заменяет отсутствующие (nil) списки на пустые. Явно опустошённый
// пользователем список при этом сохраняется как есть.
func (s *Settings) fillSlices(groupsDefault []string) {
	if s.DefaultGroups == nil {
		s.DefaultGroups = append([]string{}, groupsDefault...)
	}
	if s.CustomDomains == nil {
		s.CustomDomains = []string{}
	}
	if s.Allowlist == nil {
		s.Allowlist = []string{}
	}
}

// Path возвращает путь к файлу конфигурации.
func (s *Store) Path() string { return s.path }

// Dir возвращает каталог данных.
func (s *Store) Dir() string { return filepath.Dir(s.path) }

// Get возвращает копию текущего состояния.
func (s *Store) Get() File {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.file.clone()
}

// Update изменяет состояние и сохраняет его на диск.
func (s *Store) Update(fn func(*File) error) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	prev := s.file.clone()
	if err := fn(&s.file); err != nil {
		s.file = prev
		return err
	}
	if err := s.save(); err != nil {
		s.file = prev
		return err
	}
	return nil
}

func (s *Store) save() error {
	// Ни один список не должен уйти на диск как null.
	s.file.Settings.fillSlices(DefaultSettings().DefaultGroups)

	// Ошибку маршалинга проверить нечем: в File нет ни каналов, ни функций, ни
	// чисел, которые encoding/json отвергает, поэтому она невозможна по типу.
	raw, _ := json.MarshalIndent(&s.file, "", "  ")
	tmp := s.path + ".tmp"
	if err := os.WriteFile(tmp, raw, 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, s.path)
}

func (f File) clone() File {
	c := f
	c.Settings.DefaultGroups = cloneSlice(f.Settings.DefaultGroups)
	c.Settings.CustomDomains = cloneSlice(f.Settings.CustomDomains)
	c.Settings.Allowlist = cloneSlice(f.Settings.Allowlist)
	if f.Session != nil {
		sess := *f.Session
		sess.Groups = cloneSlice(f.Session.Groups)
		sess.CustomDomains = cloneSlice(f.Session.CustomDomains)
		sess.Allowlist = cloneSlice(f.Session.Allowlist)
		c.Session = &sess
	}
	if f.SystemProxy != nil {
		backup := *f.SystemProxy
		c.SystemProxy = &backup
	}
	return c
}

// cloneSlice копирует срез, сохраняя отличие «пустой» от «отсутствующего»:
// append(nil, ...) без элементов вернул бы nil и превратился бы в JSON null.
func cloneSlice(in []string) []string {
	out := make([]string, len(in))
	copy(out, in)
	return out
}
