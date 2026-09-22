//go:build windows

package winsys

import (
	"errors"
	"fmt"
	"os"
	"strings"

	"golang.org/x/sys/windows/registry"
)

// chromiumVendors — пути политик браузеров на движке Chromium под
// HKLM\SOFTWARE\Policies. Список один на все политики Focusd — и на прокси, и на
// блокировку сайтов, — иначе они разъедутся и какой-то браузер останется
// прикрыт наполовину.
var chromiumVendors = []string{
	`Google\Chrome`,
	`Microsoft\Edge`,
	`Chromium`,
	`BraveSoftware\Brave`,
	`Vivaldi`,
	`YandexBrowser`,
	// У разных сборок Opera путь различается, поэтому пишем оба: лишний ключ
	// безвреден, а промах мимо нужного оставит браузер вообще без политик.
	`Opera Software\Opera`,
	`Opera Software\Opera Stable`,
}

// policyValue описывает одно значение политики.
type policyValue struct {
	Path  string // путь под HKLM\SOFTWARE\Policies
	Name  string // имя значения
	Kind  string // "sz" | "dword"
	Value string
}

// policyRoot — ветка политик браузеров в HKLM. Права администратора нужны
// ровно на неё, и ни на что больше.
const policyRoot = `SOFTWARE\Policies\`

// writePolicy записывает одно значение политики в раздел браузера.
func writePolicy(path, name, kind, value string) error {
	k, _, err := createRegKey(registry.LOCAL_MACHINE, policyRoot+path, registry.SET_VALUE)
	if err != nil {
		return fmt.Errorf("политика %s\\%s: %w", path, name, err)
	}
	defer k.Close()

	switch kind {
	case "dword":
		var v uint64
		_, _ = fmt.Sscanf(value, "%d", &v)
		err = k.SetDWordValue(name, uint32(v))
	default:
		err = k.SetStringValue(name, value)
	}
	if err != nil {
		return fmt.Errorf("политика %s\\%s: %w", path, name, err)
	}
	return nil
}

const (
	autostartTask = `Focusd`
	guardTask     = `FocusdGuard`
	// Задачи страховки прокси. Их две, потому что расписание у планировщика
	// задаётся одним ключом /sc за вызов: «при входе в систему» и «раз в минуту»
	// вместе не поставить. Нужны обе: перезагрузка чинится сразу при входе, а
	// приложение, убитое без перезагрузки, — в пределах минуты.
	proxyGuardTask     = `FocusdProxyGuard`
	proxyGuardMinTask  = `FocusdProxyGuardMinute`
	proxyGuardArg      = `--proxyguard`
	proxyGuardInterval = "1"
)

// SetAutostart включает или выключает запуск приложения при входе в систему.
// Используется задача планировщика с высшими правами — иначе при каждом входе
// появлялся бы запрос UAC.
func SetAutostart(ctx contextT, enable bool, exePath string) error {
	if !enable {
		return deleteTask(ctx, autostartTask)
	}
	_, err := runExternal(ctx, "schtasks", "/create",
		"/tn", autostartTask,
		"/tr", quoteTaskArg(exePath),
		"/sc", "onlogon",
		"/rl", "highest",
		"/f",
	)
	return err
}

// SetGuard включает или выключает сторожевую задачу: раз в минуту она
// проверяет, что жёсткая сессия всё ещё удерживается, и восстанавливает
// блокировку, если приложение попытались закрыть.
func SetGuard(ctx contextT, enable bool, exePath string) error {
	if !enable {
		return deleteTask(ctx, guardTask)
	}
	_, err := runExternal(ctx, "schtasks", "/create",
		"/tn", guardTask,
		"/tr", quoteTaskArg(exePath, "--guard"),
		"/sc", "minute",
		"/mo", "1",
		"/rl", "highest",
		"/f",
	)
	return err
}

// SetProxyGuard ставит или снимает страховку от неработающего прокси.
//
// Прокси живёт внутри приложения, а настройки, которые на него указывают,
// лежат в реестре и переживают и перезагрузку, и закрытие приложения. Если
// Focusd не дожил до своего выхода (перезагрузка, снятие процесса), браузеры
// уходят в порт, которого никто не слушает, и человек остаётся без интернета
// вообще. Задачи запускают `--proxyguard`, который возвращает настройки, пока
// приложение не работает.
//
// Права высшие: иначе при каждом запуске появлялся бы запрос UAC.
func SetProxyGuard(ctx contextT, enable bool, exePath string) error {
	if !enable {
		return errors.Join(
			deleteTask(ctx, proxyGuardTask),
			deleteTask(ctx, proxyGuardMinTask),
		)
	}
	_, errLogon := runExternal(ctx, "schtasks", "/create",
		"/tn", proxyGuardTask,
		"/tr", quoteTaskArg(exePath, proxyGuardArg),
		"/sc", "onlogon",
		"/rl", "highest",
		"/f",
	)
	_, errMinute := runExternal(ctx, "schtasks", "/create",
		"/tn", proxyGuardMinTask,
		"/tr", quoteTaskArg(exePath, proxyGuardArg),
		"/sc", "minute",
		"/mo", proxyGuardInterval,
		"/rl", "highest",
		"/f",
	)
	return errors.Join(errLogon, errMinute)
}

// GuardTaskInstalled сообщает, зарегистрирована ли сторожевая задача.
func GuardTaskInstalled(ctx contextT) bool {
	_, err := runExternal(ctx, "schtasks", "/query", "/tn", guardTask)
	return err == nil
}

// deleteTask удаляет задачу планировщика. Отсутствие задачи — не ошибка, а
// разбирать локализованный текст ошибки schtasks ненадёжно, поэтому сначала
// спрашиваем, существует ли она.
func deleteTask(ctx contextT, name string) error {
	if _, err := runExternal(ctx, "schtasks", "/query", "/tn", name); err != nil {
		return nil
	}
	_, err := runExternal(ctx, "schtasks", "/delete", "/tn", name, "/f")
	return err
}

// quoteTaskArg оборачивает путь в кавычки: /tr у schtasks разбирает строку
// до конца аргумента, поэтому путь с пробелами иначе будет обрезан.
func quoteTaskArg(parts ...string) string {
	quoted := make([]string, 0, len(parts))
	for i, p := range parts {
		if i == 0 {
			quoted = append(quoted, `"`+p+`"`)
			continue
		}
		quoted = append(quoted, p)
	}
	return strings.Join(quoted, " ")
}

// SystemPrefersDark сообщает, выбрано ли в Windows тёмное оформление
// приложений. Нужно, чтобы окно успело покраситься в правильный цвет до
// первого кадра: иначе тёмная тема начинается с белой вспышки.
func SystemPrefersDark() bool {
	k, err := openRegKey(registry.CURRENT_USER,
		`SOFTWARE\Microsoft\Windows\CurrentVersion\Themes\Personalize`, registry.QUERY_VALUE)
	if err != nil {
		return false
	}
	defer k.Close()
	v, _, err := k.GetIntegerValue("AppsUseLightTheme")
	if err != nil {
		return false
	}
	return v == 0
}

// SystemLanguage возвращает локаль Windows: «ru-RU», «zh-CN», «en-US».
//
// Читается реестр, а не GetUserDefaultLocaleName: разбор локали всё равно нужен
// свой (см. i18n.FromSystem), а значение LocaleName есть на любой версии
// Windows и стоит одного открытия ключа. Ошибка или пустое значение означают
// «язык неизвестен» — приложение возьмёт английский, а не откажется запускаться.
func SystemLanguage() string {
	k, err := openRegKey(registry.CURRENT_USER, `Control Panel\International`, registry.QUERY_VALUE)
	if err != nil {
		return ""
	}
	defer k.Close()
	v, _, err := k.GetStringValue("LocaleName")
	if err != nil {
		return ""
	}
	return v
}

// osExecutable — точка подмены: os.Executable в живом процессе не отказывает, а
// ветку «путь неизвестен» проверить нужно — без пути приложение не поставит ни
// одной задачи планировщика и не запустит само себя.
var osExecutable = os.Executable

// ExecutablePath возвращает полный путь к текущему исполняемому файлу.
func ExecutablePath() (string, error) {
	p, err := osExecutable()
	if err != nil {
		return "", err
	}
	return p, nil
}
