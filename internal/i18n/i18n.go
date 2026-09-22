// Package i18n хранит переводы интерфейса и выбирает язык приложения.
//
// Строки живут здесь, а не рядом с местом показа, по одной причине: один и тот
// же текст показывается из разных мест. «Жёсткий» встречается и в интерфейсе, и
// на странице-заглушке, которую отдаёт прокси; «Что блокируем» — в трёх разных
// карточках настроек. Держать переводы в одном месте дешевле, чем сверять их
// потом по коду.
//
// Переводов на русский здесь почти нет намеренно: русский текст — это исходные
// строки в своих пакетах (каталог групп, разметка страницы-заглушки), и
// переписывать их сюда значило бы завести вторую копию, которая рано или поздно
// разойдётся с первой. Для таких строк есть Or: он возвращает исходный текст,
// если перевода нет.
package i18n

import (
	"fmt"
	"strings"
)

// Lang — язык интерфейса.
type Lang string

const (
	// RU — русский, EN — английский, ZH — китайский (упрощённый).
	RU Lang = "ru"
	EN Lang = "en"
	ZH Lang = "zh"
)

// Auto — значение настройки «как в системе». Языком не является: это выбор,
// который разрешается в один из трёх выше.
const Auto = "auto"

// Supported — языки в порядке выбора в интерфейсе.
var Supported = []Lang{RU, EN, ZH}

// Known сообщает, поддерживается ли такой язык.
func Known(v string) (Lang, bool) {
	l := Lang(strings.ToLower(strings.TrimSpace(v)))
	for _, k := range Supported {
		if l == k {
			return k, true
		}
	}
	return RU, false
}

// Resolve сводит настройку языка к конкретному языку.
//
// Неизвестное значение — включая пустое (конфигурация прошлых версий) и «auto» —
// означает «как в системе». Так же ведёт себя оформление: мусор в настройке не
// оставляет человека с неработающим экраном, а отдаёт решение системе.
func Resolve(setting, system string) Lang {
	if l, ok := Known(setting); ok {
		return l
	}
	return FromSystem(system)
}

// FromSystem выбирает язык по локали системы: «ru-RU», «zh-CN», «en-US».
//
// Понимает и трёхбуквенные коды старых версий Windows (RUS, CHS, CHT, ENU):
// значение приходит из реестра, а там форма записи менялась между версиями.
// Всё незнакомое — английский: он единственный, который читает хоть кто-то.
func FromSystem(locale string) Lang {
	v := strings.ToLower(strings.TrimSpace(locale))
	v = strings.ReplaceAll(v, "_", "-")
	switch {
	case strings.HasPrefix(v, "ru"), v == "rus":
		return RU
	case strings.HasPrefix(v, "zh"), v == "chs", v == "cht":
		return ZH
	default:
		return EN
	}
}

// T возвращает перевод ключа. Аргументы подставляются в него через Sprintf.
//
// Ключа нет в таблице — возвращается сам ключ. Это не заглушка: так вызывающий
// код может передать уже готовый текст (например, пояснение от системы) и
// получить его обратно нетронутым.
func T(l Lang, key string, args ...any) string {
	l = normalize(l)
	// Английский — последний рубеж перед ключом: пустая строка на месте подписи
	// хуже неточной.
	text := lookupIn(l, key)
	if text == "" {
		text = lookupIn(EN, key)
	}
	if text == "" {
		return key
	}
	if len(args) == 0 {
		return text
	}
	return fmt.Sprintf(text, args...)
}

// Or возвращает перевод ключа, а если перевода нет — исходный текст.
//
// Так живут строки, у которых русский вариант уже есть в своём пакете:
// переводить их сюда не нужно, а английский и китайский должны подставляться.
// Английский здесь намеренно не подставляется: у русского свои исходные строки,
// и подстановка чужого перевода заменила бы их не тем текстом.
func Or(l Lang, key, fallback string) string {
	if text := lookupIn(normalize(l), key); text != "" {
		return text
	}
	return fallback
}

// lookupIn ищет строку только в одном языке.
func lookupIn(l Lang, key string) string {
	return messages[l][key]
}

// normalize приводит язык к поддерживаемому. Пустое и незнакомое значение — это
// русский: он исходный язык приложения, и вызывающий код, которому язык не
// передали, обязан получить осмысленный текст, а не английский по случайности.
func normalize(l Lang) Lang {
	if _, ok := Known(string(l)); ok {
		return l
	}
	return RU
}

// GroupTitle и GroupNote — подписи групп каталога. Русский берётся из самого
// каталога: он остаётся исходным текстом, а здесь лежат только переводы.
func GroupTitle(l Lang, id, fallback string) string {
	return Or(l, "group."+id+".title", fallback)
}

func GroupNote(l Lang, id, fallback string) string {
	return Or(l, "group."+id+".note", fallback)
}

// Strictness называет уровень строгости. Неизвестный уровень — прочерк, а не
// пустое место: строка «Режим» с пустотой выглядит сломанной.
func Strictness(l Lang, s string) string {
	switch s {
	case "soft":
		return T(l, "strictness.soft")
	case "strict":
		return T(l, "strictness.strict")
	case "lock":
		return T(l, "strictness.lock")
	}
	return "—"
}

// Locale возвращает тег языка для атрибута lang в HTML.
func Locale(l Lang) string {
	switch l {
	case ZH:
		return "zh-CN"
	case EN:
		return "en"
	default:
		return "ru"
	}
}

// messages — переводы. Русских подписей групп здесь нет намеренно: они лежат в
// каталоге и приходят сюда аргументом fallback (см. GroupTitle).
var messages = map[Lang]map[string]string{
	RU: {
		"strictness.soft":   "Мягкий",
		"strictness.strict": "Строгий",
		"strictness.lock":   "Жёсткий",

		"blockpage.title":        "Сфокусируйся",
		"blockpage.active.title": "Ты в фокусе",
		"blockpage.active.sub":   "Этот сайт заблокирован до конца сессии",
		"blockpage.remaining":    "осталось",
		"blockpage.done.title":   "Сессия завершена",
		"blockpage.done.sub":     "Блокировка снята — обнови страницу.",
		"blockpage.mode":         "Режим",
		"blockpage.blocked":      "Отсечено запросов",

		"app.hint.browsers": "Браузер уже открыт: %s. Закройте его и откройте заново — " +
			"список блокировки подхватывается при запуске браузера.",
		"app.notice.proxyStalled": "Прокси Focusd перестал водить трафик, поэтому браузеры " +
			"переведены на политику блокировки. Если страницы не открываются — закройте и " +
			"откройте браузер: он перечитает настройки прокси только при запуске. " +
			"Счётчик отсечённых запросов временно неточен.",
		"app.cancel.wait":    "Отмена станет доступна через %d с",
		"app.cancel.confirm": "Сессию будет прервано, блокировка снимется",
	},

	EN: {
		"strictness.soft":   "Soft",
		"strictness.strict": "Strict",
		"strictness.lock":   "Locked",

		"group.social.title":     "Social networks",
		"group.social.note":      "Feeds, stories, endless scroll",
		"group.video.title":      "Video & streaming",
		"group.video.note":       "YouTube, Twitch, series",
		"group.shorts.title":     "Short videos",
		"group.shorts.note":      "Reels, Shorts, clips",
		"group.games.title":      "Games",
		"group.games.note":       "Stores, launchers, gaming portals",
		"group.messengers.title": "Messengers",
		"group.messengers.note":  "Chats, servers, channels",
		"group.news.title":       "News & forums",
		"group.news.note":        "News feeds and discussions",
		"group.shopping.title":   "Marketplaces",
		"group.shopping.note":    "Shops and classifieds",
		"group.adult.title":      "18+",
		"group.adult.note":       "Adult content",
		"group.doomscroll.title": "Doomscroll traps",
		"group.doomscroll.note":  "Endless feed services",

		"blockpage.title":         "Stay focused",
		"blockpage.active.title":  "You are focused",
		"blockpage.active.sub":    "This site is blocked until the session ends",
		"blockpage.remaining":     "remaining",
		"blockpage.done.title":    "Session finished",
		"blockpage.done.sub":      "Blocking is off — refresh the page.",
		"blockpage.mode":          "Mode",
		"blockpage.blocked":       "Requests blocked",

		"app.hint.browsers": "Browser already running: %s. Close it and open it again — " +
			"the blocklist is read when the browser starts.",
		"app.notice.proxyStalled": "The Focusd proxy stopped carrying traffic, so browsers were " +
			"switched to the blocklist policy. If pages do not open, close and reopen the browser: " +
			"it re-reads proxy settings only at startup. The blocked-request counter is temporarily " +
			"inaccurate.",
		"app.cancel.wait":    "Cancelling will be available in %d s",
		"app.cancel.confirm": "The session will be interrupted and blocking removed",
	},

	ZH: {
		"strictness.soft":   "宽松",
		"strictness.strict": "严格",
		"strictness.lock":   "锁定",

		"group.social.title":     "社交网络",
		"group.social.note":      "信息流、动态、无限滚动",
		"group.video.title":      "视频与流媒体",
		"group.video.note":       "YouTube、Twitch、剧集",
		"group.shorts.title":     "短视频",
		"group.shorts.note":      "Reels、Shorts、短片",
		"group.games.title":      "游戏",
		"group.games.note":       "商店、启动器、游戏门户",
		"group.messengers.title": "即时通讯",
		"group.messengers.note":  "聊天、服务器、频道",
		"group.news.title":       "新闻与论坛",
		"group.news.note":        "新闻资讯与讨论区",
		"group.shopping.title":   "电商平台",
		"group.shopping.note":    "商店与分类广告",
		"group.adult.title":      "18+",
		"group.adult.note":       "成人内容",
		"group.doomscroll.title": "无尽刷屏陷阱",
		"group.doomscroll.note":  "无限浏览类服务",

		"blockpage.title":        "保持专注",
		"blockpage.active.title": "你正在专注",
		"blockpage.active.sub":   "本网站在本次专注结束前已被屏蔽",
		"blockpage.remaining":    "剩余",
		"blockpage.done.title":   "会话已结束",
		"blockpage.done.sub":     "屏蔽已解除 —— 请刷新页面。",
		"blockpage.mode":         "模式",
		"blockpage.blocked":      "已拦截请求",

		"app.hint.browsers": "浏览器已在运行：%s。请关闭后重新打开 —— " +
			"屏蔽列表只在浏览器启动时读取。",
		"app.notice.proxyStalled": "Focusd 代理已无法转发流量，因此浏览器已切换到屏蔽策略。" +
			"如果页面打不开，请关闭并重新打开浏览器：它只在启动时读取代理设置。" +
			"已拦截请求计数暂时不准确。",
		"app.cancel.wait":    "还有 %d 秒才能取消",
		"app.cancel.confirm": "会话将被中断，屏蔽会被解除",
	},
}
