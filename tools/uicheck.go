//go:build ignore

// Проверка интерфейса: поднимает собранный фронтенд на локальном HTTP,
// подставляет мост Wails с настоящими данными из Go-конфигурации, снимает DOM
// headless-браузером и делает скриншоты экранов.
//
// Ловит пустое окно — худший вид отказа UI, который не видят обычные тесты.
//
// Запуск: go run tools/uicheck.go
package main

import (
	"encoding/json"
	"fmt"
	"image"
	"image/draw"
	"image/png"
	"net"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"focusd/internal/catalog"
	"focusd/internal/config"
	"focusd/internal/i18n"
)

var browsers = []string{
	`C:\Program Files\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	`C:\Program Files (x86)\Microsoft\Edge\Application\msedge.exe`,
	`C:\Program Files\Microsoft\Edge\Application\msedge.exe`,
}

const (
	winWidth  = 1100
	winHeight = 740
)

// pageOpts управляет тем, что стенд делает со страницей после загрузки.
type pageOpts struct {
	// click — CSS-селектор элемента, по которому нужно щёлкнуть.
	click string
	// scroll — CSS-селектор прокручиваемого контейнера, который нужно опустить
	// в самый низ. Нужен для скриншота нижних разделов настроек.
	scroll string
	// probe — имя сценария проверки поведения: "ruler" или "overlap".
	probe string
	// w, h — размер окна для скриншота; 0 означает штатный.
	w, h int
	// scale — масштаб устройства для снимка. Нужен там, где окно маленькое
	// (виджет), и разглядеть детали на снимке 1:1 невозможно.
	scale float64
	// box — отрисовать интерфейс в рамке ровно w×h и обрезать снимок по ней.
	//
	// Без этого виджет не снять: окно Chrome не опускается ниже ~500 px в
	// ширину и режет высоту, поэтому макет 320×140 рисуется совсем в другом
	// размере — верхняя строка уезжает за край, и снимок врёт.
	box bool
}

// viewport возвращает размер окна для скриншота.
func (o pageOpts) viewport() (int, int) {
	if o.w > 0 && o.h > 0 {
		return o.w, o.h
	}
	return winWidth, winHeight
}

func main() {
	browser := findBrowser()
	if browser == "" {
		fmt.Println("ПРОПУСК: не найден Chrome или Edge")
		return
	}

	dist, err := filepath.Abs("frontend/dist")
	if err != nil {
		fatal(err)
	}
	js, css, err := assets(dist)
	if err != nil {
		fatal(err)
	}
	if err := ensureFresh(js); err != nil {
		fatal(err)
	}
	detectorSelfTest()

	// Настоящие данные бэкенда: ровно то, что отдаёт GetSettings/GetCatalog.
	//
	// Оформление задаётся явно, а не «как в системе»: иначе снимки зависели бы
	// от темы Windows на машине сборки, и светлый вариант никто бы не увидел.
	settings := withField(realSettings(), "theme", "light")
	// Тот же набор, но с явно выбранной тёмной темой: так проверяется, что
	// оформление действительно переключается, а не только объявлено в CSS.
	dark := withField(settings, "theme", "dark")

	failures := 0
	checks := []struct {
		name     string
		state    map[string]any
		settings map[string]any
		opts     pageOpts
		want     []string
		reject   []string
	}{
		{
			name:     "экран запуска, свежая установка",
			state:    stateJSON(settings, "idle"),
			settings: settings,
			want: []string{
				"Начать фокус", "Новая сессия", "Соцсети", "Видео и стриминг",
				"Мягкий", "Жёсткий", "Строгость", "Длительность", "Что блокируем",
				// Линейка выбора времени должна быть на месте и быть доступной.
				`role="slider"`, "ruler-tick", "ruler-center", "Настройки",
				// Управление своими сайтами обязано быть на виду, а не спрятано
				// в настройках: это то, ради чего человек сюда пришёл.
				"Свои сайты", "Исключения", "проверить домен",
			},
			reject: []string{"JSERROR", "JSREJECT", "banner-error"},
		},
		{
			name:     "битые данные: списки пришли как null",
			state:    nulled(stateJSON(settings, "idle")),
			settings: nulled(settings),
			// Даже на мусоре окно обязано показать хоть что-то, а не пустоту.
			want: []string{"Начать фокус"},
		},
		{
			name:     "экран идущей жёсткой сессии",
			state:    stateJSON(settings, "lock"),
			settings: settings,
			want:     []string{"Жёсткий", "Отсечено запросов", "Правил блокировки", "Последний сайт"},
			reject:   []string{"JSERROR", "JSREJECT", "Начать фокус"},
		},
		{
			// Техническое состояние слоёв обязано жить в настройках, а не на
			// главном экране. Однажды оно уже вывалилось туда простынёй про
			// svchost, Hyper-V и WSL2 — и выглядело как поломка приложения.
			//
			// Условие проверки — не слово «прокси», оно есть в пояснениях на
			// самом экране, а признак аварии: баннер с ошибкой и текст самой
			// неполадки.
			name: "замечание о слоях не показывается на главном экране",
			state: withNotice(
				stateJSON(settings, "idle"),
				"Прокси Focusd не смог водить трафик, включена политика браузеров",
			),
			settings: settings,
			want:     []string{"Начать фокус"},
			reject: []string{
				"banner-warn", "banner-error", "не смог водить трафик",
				"JSERROR", "JSREJECT",
			},
		},
		{
			// Браузер, открытый до начала сессии, политику ещё не читал:
			// подсказка о перезапуске — то, без чего блокировка не заработает.
			name:     "подсказка перезапустить уже открытый браузер",
			state:    withHint(stateJSON(settings, "soft"), "Браузер уже открыт: Chrome. Закройте его и откройте заново."),
			settings: settings,
			want:     []string{"Браузер уже открыт", "Chrome", "class=\"hint\""},
			reject:   []string{"JSERROR", "JSREJECT"},
		},
		{
			// Мягкая сессия — самый частый случай: кнопка отмены обязана быть живой.
			name:     "экран мягкой сессии с доступной отменой",
			state:    stateJSON(settings, "soft"),
			settings: settings,
			want:     []string{"Мягкий режим", "Отменить фокус", "Правил блокировки"},
			reject:   []string{"JSERROR", "JSREJECT", "Начать фокус", "Браузер уже открыт"},
		},
		{
			// Виджет рисуется в том же окне, но без сайдбара и заголовка:
			// в плашку 320×140 они не помещаются. Проверяем именно скрытость,
			// а не отсутствие в разметке.
			name:     "виджет-плашка идущей сессии",
			state:    barState(stateJSON(settings, "strict")),
			settings: withField(settings, "widget", true),
			opts:     pageOpts{probe: "widget-out"},
			want: []string{
				`class="widget"`, `data-shape="bar"`, "widget-clock", "widget-bar-fill",
				"Строгий режим",
				`class="sidebar" hidden`, `class="titlebar" hidden`,
				// Кнопки виджета: закрепить и вернуться в окно. Возврат обязан
				// быть подписан словами — по иконке он читался наоборот.
				"Не держать поверх окон", "Показать окно", "Вернуться в окно Focusd",
				"WIDGET-OK",
			},
			reject: []string{"WIDGET-FAIL", "JSERROR", "JSREJECT", "Начать фокус"},
		},
		{
			// Круглая форма — второй вид того же таймера: кольцо прогресса
			// вместо полосы. Проверяем, что кольцо действительно нарисовано и
			// что окно получило форму «круг», а не осталось плашкой.
			name:     "виджет-круг идущей сессии",
			state:    ringState(stateJSON(settings, "lock")),
			settings: withField(settings, "widget", true),
			want: []string{
				`data-shape="ring"`, `class="widget-ring`, "ring-arc", "ring-track",
				`stroke-dashoffset=`, "widget-clock", "Жёсткий режим",
			},
			reject: []string{"JSERROR", "JSREJECT", "Начать фокус"},
		},
		{
			name:     "виджет без сессии",
			state:    barState(stateJSON(settings, "idle")),
			settings: withField(settings, "widget", true),
			want: []string{
				`class="widget`, "Фокус не идёт", "Показать окно", "Вернуться в окно Focusd",
				"WIDGET-OK",
			},
			opts:   pageOpts{probe: "widget-out"},
			reject: []string{"WIDGET-FAIL", "JSERROR", "JSREJECT"},
		},
		{
			// То же для круга: форма не должна менять правила выхода.
			name:     "виджет-круг без сессии оставляет выход",
			state:    ringState(stateJSON(settings, "idle")),
			settings: withField(settings, "widget", true),
			want:     []string{`data-shape="ring"`, "Фокус не идёт", "WIDGET-OK"},
			opts:     pageOpts{probe: "widget-out"},
			reject:   []string{"WIDGET-FAIL", "JSERROR", "JSREJECT"},
		},
		{
			// Здесь техническое состояние уместно: человек пришёл сюда разбираться.
			name: "замечание о слоях показано в настройках",
			state: withNotice(
				stateJSON(settings, "idle"),
				"Прокси Focusd не смог водить трафик, включена политика браузеров",
			),
			settings: settings,
			opts:     pageOpts{click: `[aria-label="Открыть настройки"]`},
			want:     []string{"политика браузеров", "settings-panel"},
			reject:   []string{"JSERROR", "JSREJECT"},
		},
		{
			// Настройки разбиты на разделы: заголовок отвечает на «о чём эта
			// группа целиком», а карточки идут по одной в строку. Проверяем и
			// структуру, и сами разделы — иначе панель снова съедет в кашу.
			name:     "настройки разбиты на разделы",
			state:    stateJSON(settings, "idle"),
			settings: settings,
			opts:     pageOpts{click: `[aria-label="Открыть настройки"]`},
			want: []string{
				"settings-stack", "section-title",
				"Оформление", "Компактный таймер", "Новая сессия по умолчанию",
				"Защита", "Что блокируем", "Приложение",
				"Слои защиты", "Свои сайты", "Исключения",
				// Выбор формы таймера живёт рядом с тумблером виджета.
				"Вид таймера", "Плашка", "Круг",
			},
			reject: []string{"JSERROR", "JSREJECT"},
		},
		{
			// Вид таймера и «поверх всех окон» стоят рядом с тумблером виджета,
			// а настройки открываются только в обычном окне: значит, они обязаны
			// работать именно при выключенном компактном режиме — иначе никогда.
			name:     "вид таймера переключается при выключенном виджете",
			state:    stateJSON(settings, "idle"),
			settings: settings,
			opts: pageOpts{
				click: `[aria-label="Открыть настройки"]`,
				probe: "shape-pick",
			},
			want:   []string{"SHAPE-OK"},
			reject: []string{"SHAPE-FAIL", "ONTOP-FAIL", "JSERROR", "JSREJECT"},
		},
		{
			// Наложение текста в шапке карточки — то, ради чего разделы и
			// появились. Проверяем геометрию, а не наличие классов: подпись
			// карточки обязана начинаться ниже заголовка, а не поверх него.
			name:     "заголовки и подписи карточек не пересекаются",
			state:    stateJSON(settings, "idle"),
			settings: settings,
			opts: pageOpts{
				click: `[aria-label="Открыть настройки"]`,
				probe: "overlap",
			},
			want:   []string{"OVERLAP-OK"},
			reject: []string{"OVERLAP-FAIL", "JSERROR", "JSREJECT"},
		},
		{
			// Тема — настройка, а не действие окна. Однажды её кнопка оказалась
			// в одном ряду со «Свернуть» и «Закрыть», и промахнуться означало
			// закрыть приложение вместо смены оформления. Проверяем не только
			// место в разметке, но и наличие подписи: безымянная иконка снова
			// превратит её в загадку.
			name:     "смена темы живёт в сайдбаре, а не в заголовке окна",
			state:    stateJSON(settings, "idle"),
			settings: settings,
			opts:     pageOpts{probe: "theme-place"},
			want:     []string{"THEME-OK"},
			reject:   []string{"THEME-FAIL", "JSERROR", "JSREJECT"},
		},
		{
			// Наличие линейки в DOM ничего не доказывает: проверяем, что она
			// действительно меняет значение по стрелке клавиатуры.
			name:     "линейка времени реагирует на стрелки",
			state:    stateJSON(settings, "idle"),
			settings: settings,
			opts:     pageOpts{probe: "ruler"},
			want:     []string{"RULER-OK"},
			reject:   []string{"RULER-FAIL", "JSERROR", "JSREJECT"},
		},
		{
			// Тёмная тема — не «инверсия через фильтр»: она обязана приезжать
			// атрибутом на <html>, иначе переменные не подхватятся.
			name:     "тёмная тема применяется к документу",
			state:    stateJSON(withField(settings, "theme", "dark"), "idle"),
			settings: withField(settings, "theme", "dark"),
			want:     []string{`data-theme="dark"`, "Начать фокус"},
			reject:   []string{"JSERROR", "JSREJECT"},
		},
		{
			// Английский интерфейс: проверяются три вещи сразу — что строки
			// действительно переведены, что подписи групп приехали из Go уже
			// переведёнными и что документ объявлен английским. Последнее важно
			// для переносов и подбора шрифта.
			name:     "английский интерфейс",
			state:    withField(stateJSON(settings, "idle"), "resolvedLanguage", "en"),
			settings: withField(settings, "language", "en"),
			opts:     pageOpts{click: `[data-action="settings"]`},
			want: []string{
				`lang="en"`, "New session", "Start focus", "What to block",
				// Подписи групп приезжают из Go уже переведёнными, а амперсанд в
				// разметке экранирован — поэтому сравнение с экранированным.
				"Social networks", "Video &amp; streaming", "Custom sites", "Exceptions",
				"Settings", "Compact timer", "Show the timer above other windows",
				"System", "Russian", "Chinese",
			},
			reject: []string{"JSERROR", "JSREJECT", "Новая сессия", "Соцсети", "Настройки"},
		},
		{
			// Китайский: иероглифы обязаны быть на месте во всех разделах, а не
			// только в заголовке — иначе половина панели осталась бы английской.
			name:     "китайский интерфейс",
			state:    withField(stateJSON(settings, "idle"), "resolvedLanguage", "zh"),
			settings: withField(settings, "language", "zh"),
			opts:     pageOpts{click: `[data-action="settings"]`},
			want: []string{
				`lang="zh-CN"`, "新建专注", "开始专注", "屏蔽内容",
				"社交网络", "视频与流媒体", "自定义网站", "例外",
				"设置", "迷你计时器", "在其他窗口之上显示计时器",
				"跟随系统", "俄语", "中文",
			},
			reject: []string{"JSERROR", "JSREJECT", "Новая сессия", "Настройки", "Settings"},
		},
		{
			// Экран идущей сессии по-китайски: подписи строк приходят из словаря
			// интерфейса, а не из Go, и проверяются отдельно от экрана запуска.
			name:     "китайский экран идущей сессии",
			state:    withField(stateJSON(settings, "lock"), "resolvedLanguage", "zh"),
			settings: withField(settings, "language", "zh"),
			want:     []string{"专注进行中", "屏蔽规则", "已拦截请求", "锁定模式", "紧急解除屏蔽"},
			reject:   []string{"JSERROR", "JSREJECT", "Идёт фокус", "Строгий режим"},
		},
		{
			// Английский экран идущей сессии: подпись режима собирается из
			// шаблона, и в английском она обязана читаться как «Soft mode».
			name:     "английский экран идущей сессии",
			state:    withField(stateJSON(settings, "soft"), "resolvedLanguage", "en"),
			settings: withField(settings, "language", "en"),
			want:     []string{"Focusing", "Cancel focus", "Soft mode", "Blocking rules"},
			reject:   []string{"JSERROR", "JSREJECT", "Идёт фокус", "Мягкий режим"},
		},
		{
			// Переключатель языка обязан быть живым: смена языка пересобирает
			// кадр целиком, и проверить это можно только настоящим кликом.
			name:     "переключатель языка работает",
			state:    withField(stateJSON(settings, "idle"), "resolvedLanguage", "zh"),
			settings: withField(settings, "language", "zh"),
			opts: pageOpts{
				click:  `[data-action="settings"]`,
				scroll: ".settings-body",
				probe:  "lang-pick",
			},
			want:   []string{"LANG-OK"},
			reject: []string{"LANG-FAIL", "JSERROR", "JSREJECT"},
		},
	}

	for _, c := range checks {
		dom, err := dumpDOM(browser, dist, js, css, c.state, c.settings, c.opts)
		if err != nil {
			fmt.Printf("ПРОВАЛ %-40s браузер не отработал: %v\n", c.name, err)
			failures++
			continue
		}
		if problems := check(dom, c.want, c.reject); len(problems) == 0 {
			fmt.Printf("OK     %-40s (%d символов DOM)\n", c.name, len(dom))
			continue
		} else {
			fmt.Printf("ПРОВАЛ %-40s\n", c.name)
			for _, p := range problems {
				fmt.Printf("         · %s\n", p)
			}
			// Разметка упавшего экрана сохраняется: без неё разбираться
			// с провалом приходится догадками.
			_ = os.MkdirAll("preview", 0o755)
			_ = os.WriteFile("preview/failed-dom.html", []byte(dom), 0o644)
			failures++
		}
	}

	// Скриншоты — чтобы дизайн можно было посмотреть глазами.
	previews := []struct {
		file     string
		state    map[string]any
		settings map[string]any
		opts     pageOpts
	}{
		{"preview/setup.png", stateJSON(settings, "idle"), settings, pageOpts{}},
		{"preview/setup-dark.png", stateJSON(dark, "idle"), dark, pageOpts{}},
		{"preview/running.png", stateJSON(settings, "lock"), settings, pageOpts{}},
		{"preview/running-dark.png", stateJSON(dark, "lock"), dark, pageOpts{}},
		{"preview/running-soft.png", withHint(stateJSON(settings, "soft"),
			"Браузер уже открыт: Chrome, Edge. Закройте его и откройте заново — блокировка подхватывается при запуске браузера."), settings, pageOpts{}},
		// Панель настроек открывается кликом, поэтому её снимаем с тем же
		// приёмом, что и настоящий пользователь.
		{
			"preview/settings.png",
			withNotice(stateJSON(settings, "idle"), "Прокси Focusd не смог водить трафик, поэтому браузеры переведены на политику блокировки. Счётчик отсечённых запросов временно неточен."),
			settings,
			pageOpts{click: `[aria-label="Открыть настройки"]`},
		},
		{
			"preview/settings-dark.png",
			withNotice(stateJSON(dark, "idle"), "Прокси Focusd не смог водить трафик, поэтому браузеры переведены на политику блокировки. Счётчик отсечённых запросов временно неточен."),
			dark,
			pageOpts{click: `[aria-label="Открыть настройки"]`},
		},
		// Нижние разделы настроек: панель длиннее окна, поэтому прокручиваем
		// её до конца — иначе видно только первые карточки.
		{
			"preview/settings-more.png",
			stateJSON(settings, "idle"),
			settings,
			pageOpts{click: `[aria-label="Открыть настройки"]`, scroll: ".settings-body"},
		},
		// Виджет: окно размером ровно с плашку, поэтому и снимок такой же.
		// box обязателен: Chrome не отдаёт окно 320×140, и без рамки снимок
		// показывал бы обрезанный макет, а не то, что видит человек.
		{
			"preview/widget.png",
			barState(stateJSON(dark, "strict")),
			withField(dark, "widget", true),
			pageOpts{w: 320, h: 140, box: true},
		},
		{
			"preview/widget-idle.png",
			barState(stateJSON(settings, "idle")),
			withField(settings, "widget", true),
			pageOpts{w: 320, h: 140, box: true},
		},
		// Тот же виджет крупным планом: на снимке 320×140 кнопки и подписи
		// не разглядеть, а проверять их глазами нужно.
		{
			"preview/widget@2x.png",
			barState(stateJSON(dark, "strict")),
			withField(dark, "widget", true),
			pageOpts{w: 320, h: 140, scale: 3, box: true},
		},
		// Круглая форма: окно квадратное, кольцо должно занимать его целиком.
		{
			"preview/widget-ring.png",
			ringState(stateJSON(settings, "strict")),
			withField(settings, "widget", true),
			pageOpts{w: 224, h: 224, box: true},
		},
		{
			"preview/widget-ring-dark@2x.png",
			ringState(stateJSON(dark, "lock")),
			withField(dark, "widget", true),
			pageOpts{w: 224, h: 224, scale: 3, box: true},
		},
		{
			"preview/widget-ring-idle.png",
			ringState(stateJSON(settings, "idle")),
			withField(settings, "widget", true),
			pageOpts{w: 224, h: 224, box: true},
		},
		// Минимальный размер окна: проверяем, что вёрстка не разъезжается,
		// когда места меньше всего.
		{"preview/narrow.png", stateJSON(settings, "idle"), settings, pageOpts{w: 940, h: 620}},
		// Переводы снимаются глазами: длина английских и китайских подписей
		// другая, и разъехавшаяся вёрстка иначе осталась бы незамеченной.
		{
			"preview/setup-en.png",
			withField(stateJSON(settings, "idle"), "resolvedLanguage", "en"),
			withField(settings, "language", "en"),
			pageOpts{},
		},
		{
			"preview/setup-zh.png",
			withField(stateJSON(settings, "idle"), "resolvedLanguage", "zh"),
			withField(settings, "language", "zh"),
			pageOpts{},
		},
		{
			"preview/running-en.png",
			withField(stateJSON(settings, "strict"), "resolvedLanguage", "en"),
			withField(settings, "language", "en"),
			pageOpts{},
		},
		{
			"preview/settings-en.png",
			withField(stateJSON(settings, "idle"), "resolvedLanguage", "en"),
			withField(settings, "language", "en"),
			pageOpts{click: `[data-action="settings"]`},
		},
		{
			"preview/settings-zh.png",
			withField(stateJSON(settings, "idle"), "resolvedLanguage", "zh"),
			withField(settings, "language", "zh"),
			pageOpts{click: `[data-action="settings"]`},
		},
		{
			// Язык меняется на живой панели: снимок проверяет, что переключатель
			// виден и что панель от него не разъезжается.
			"preview/settings-lang-zh.png",
			withField(stateJSON(settings, "idle"), "resolvedLanguage", "zh"),
			withField(settings, "language", "zh"),
			pageOpts{click: `[data-action="settings"]`, scroll: ".settings-body", probe: "lang-pick"},
		},
	}
	for _, p := range previews {
		if err := os.MkdirAll(filepath.Dir(p.file), 0o755); err != nil {
			fatal(err)
		}
		if err := screenshot(browser, dist, js, css, p.state, p.settings, p.opts, p.file); err != nil {
			fmt.Printf("ПРЕДУПРЕЖДЕНИЕ: скриншот %s не сделан: %v\n", p.file, err)
			continue
		}
		fmt.Printf("       скриншот: %s\n", p.file)
	}

	if failures > 0 {
		fmt.Printf("\nПровалов: %d\n", failures)
		os.Exit(1)
	}
	fmt.Println("\nИнтерфейс отрисовывается корректно.")
}

// realSettings читает настройки так же, как это делает приложение на старте.
func realSettings() map[string]any {
	dir, err := os.MkdirTemp("", "focusd-uicheck-*")
	if err != nil {
		fatal(err)
	}
	defer os.RemoveAll(dir)

	store, err := config.Open(dir)
	if err != nil {
		fatal(err)
	}
	raw, err := json.Marshal(store.Get().Settings)
	if err != nil {
		fatal(err)
	}
	var out map[string]any
	if err := json.Unmarshal(raw, &out); err != nil {
		fatal(err)
	}
	return out
}

// withNotice добавляет к состоянию замечание о слоях блокировки.
func withNotice(s map[string]any, text string) map[string]any {
	return withField(s, "notice", text)
}

// withHint добавляет подсказку о том, что сделать прямо сейчас.
func withHint(s map[string]any, text string) map[string]any {
	return withField(s, "hint", text)
}

func withField(s map[string]any, key string, value any) map[string]any {
	out := make(map[string]any, len(s)+1)
	for k, v := range s {
		out[k] = v
	}
	out[key] = value
	return out
}

// nulled заменяет все массивы на null — имитация старых или повреждённых данных.
func nulled(in map[string]any) map[string]any {
	out := make(map[string]any, len(in))
	for k, v := range in {
		if _, isSlice := v.([]any); isSlice {
			out[k] = nil
			continue
		}
		out[k] = v
	}
	return out
}

// stateJSON собирает State ровно с теми именами полей, что отдаёт app.go.
// mode: "idle" — сессии нет, иначе идёт сессия указанной строгости.
func stateJSON(settings map[string]any, mode string) map[string]any {
	groups, _ := settings["defaultGroups"].([]any)
	ids := make([]string, 0, len(groups))
	for _, g := range groups {
		if s, ok := g.(string); ok {
			ids = append(ids, s)
		}
	}

	now := time.Now().Unix()
	session := map[string]any{
		"active": false, "strictness": "soft", "startedAt": 0, "endsAt": 0,
		"durationSec": 0, "remaining": 0, "cancelable": true, "cancelAt": 0, "label": "",
	}
	if mode != "idle" {
		const total = 45 * 60
		session = map[string]any{
			"active": true, "strictness": mode,
			"startedAt": now - 12*60, "endsAt": now + (total - 12*60),
			"durationSec": total, "remaining": total - 12*60,
			"cancelable": mode == "soft",
			// У строгой сессии отмена открывается только через минуту.
			"cancelAt": now + 48,
			"label":    "Фокус на 45 мин",
		}
	}

	theme, _ := settings["theme"].(string)
	if theme != "light" && theme != "dark" {
		theme = "light"
	}

	widget, _ := settings["widget"].(bool)
	onTop := true
	if v, ok := settings["widgetOnTop"].(bool); ok {
		onTop = v
	}

	shape, _ := settings["widgetShape"].(string)
	if shape != "ring" {
		shape = "bar"
	}

	// Язык проверки берётся из настроек, а «auto» разрешается в русский: стенд
	// собран на русских ожиданиях, и настоящее «как в системе» сделало бы
	// результат зависимым от машины сборки. Английский и китайский варианты
	// задаются явно через withField.
	language, _ := settings["language"].(string)
	if language != "en" && language != "zh" {
		language = "ru"
	}

	return map[string]any{
		"elevated":      true,
		"browserBlock":  mode != "idle",
		"proxyBlock":    mode != "idle",
		"hint":          "",
		"notice":        "",
		"activeGroups":  ids,
		"customDomains": settings["customDomains"],
		"allowlist":     settings["allowlist"],
		"blockedToday":  137,
		"totalRules":    268,
		"lastBlocked":   "r1---sn-abc.googlevideo.com",
		"dataDir":       `C:\Users\you\AppData\Local\Focusd`,
		"version":       "0.1.0",
		"lastError":     "",
		"theme":         theme,
		"resolvedTheme": theme,
		"language":      language,
		// В настоящем приложении «auto» разрешает Go; здесь выбор уже сделан,
		// и разрешённый язык совпадает с выбором.
		"resolvedLanguage": language,
		"widget":           widget,
		"widgetOnTop":      onTop,
		"widgetShape":      shape,
		"session":          session,
	}
}

// catalogFor переводит подписи групп так же, как это делает приложение:
// домены и идентификаторы остаются, подписи приходят из словаря.
func catalogFor(state map[string]any) []catalog.Group {
	code, _ := state["resolvedLanguage"].(string)
	lang, _ := i18n.Known(code)
	groups := catalog.All()
	for i := range groups {
		groups[i].Title = i18n.GroupTitle(lang, groups[i].ID, groups[i].Title)
		groups[i].Note = i18n.GroupNote(lang, groups[i].ID, groups[i].Note)
	}
	return groups
}

// barState и ringState задают форму компактного таймера вместе с режимом
// виджета: форма приезжает в состоянии окна, а не в настройках.
func barState(s map[string]any) map[string]any {
	return withField(withField(s, "widget", true), "widgetShape", "bar")
}

func ringState(s map[string]any) map[string]any {
	return withField(withField(s, "widget", true), "widgetShape", "ring")
}

// ensureFresh не даёт проверить устаревший бандл: если сборка фронтенда не
// прошла, dist остаётся от прошлого раза и проверка молча даёт ложное «ОК».
func ensureFresh(js string) error {
	bundle, err := os.Stat(filepath.Join("frontend", "dist", "assets", js))
	if err != nil {
		return err
	}
	newest := time.Time{}
	note := func(_ string, info os.FileInfo, err error) error {
		if err == nil && info != nil && !info.IsDir() && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
		return nil
	}
	_ = filepath.Walk(filepath.Join("frontend", "src"), note)
	_ = filepath.Walk(filepath.Join("frontend", "index.html"), note)

	if newest.After(bundle.ModTime()) {
		return fmt.Errorf("бандл %s устарел (исходники новее) — пересоберите фронтенд: npm run build", js)
	}
	return nil
}

// detectorSelfTest убеждается, что проверка умеет отличать пустой экран от
// рабочего: иначе «ОК» ничего не значит.
func detectorSelfTest() {
	blank := `<html><body><div id="app"></div></body></html>`
	if len(check(blank, []string{"Начать фокус"}, nil)) == 0 {
		fmt.Println("ОШИБКА: детектор считает пустой экран нормальным")
		os.Exit(1)
	}
	if len(check(blank, nil, nil)) != 1 {
		fmt.Println("ОШИБКА: детектор не распознаёт отсутствие .setup")
		os.Exit(1)
	}
}

func check(dom string, want, reject []string) []string {
	var problems []string
	if !strings.Contains(dom, `class="setup"`) &&
		!strings.Contains(dom, `class="running"`) &&
		!strings.Contains(dom, `class="widget`) {
		problems = append(problems, `в DOM нет ни .setup, ни .running, ни .widget — экран не отрисован`)
	}
	for _, w := range want {
		if !strings.Contains(dom, w) {
			problems = append(problems, fmt.Sprintf("не найдено %q", w))
		}
	}
	for _, r := range reject {
		if strings.Contains(dom, r) {
			problems = append(problems, fmt.Sprintf("найдено недопустимое %q", r))
		}
	}
	return problems
}

func assets(dist string) (js, css string, err error) {
	entries, err := os.ReadDir(filepath.Join(dist, "assets"))
	if err != nil {
		return "", "", fmt.Errorf("сначала соберите фронтенд (npm run build): %w", err)
	}
	for _, e := range entries {
		switch filepath.Ext(e.Name()) {
		case ".js":
			js = e.Name()
		case ".css":
			css = e.Name()
		}
	}
	if js == "" {
		return "", "", fmt.Errorf("в %s не найден js-бандл", dist)
	}
	return js, css, nil
}

// serve поднимает dist со подставленным мостом Wails и возвращает адрес.
func serve(dist, js, css string, state, settings map[string]any, o pageOpts) (string, func(), error) {
	payload, err := json.Marshal(map[string]any{
		"state": state, "settings": settings, "catalog": catalogFor(state),
		"click": o.click, "scroll": o.scroll, "probe": o.probe,
	})
	if err != nil {
		return "", nil, err
	}

	mux := http.NewServeMux()
	mux.Handle("/assets/", http.FileServer(http.Dir(dist)))
	mux.HandleFunc("/__stub.js", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/javascript; charset=utf-8")
		fmt.Fprintf(w, "window.__DATA = %s;\n%s", payload, stubJS)
	})
	mux.HandleFunc("/", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		box := ""
		if o.box {
			bw, bh := o.viewport()
			// Рамка задаёт настоящий размер макета, а большое окно браузера
			// лишь даёт место, чтобы всё это поместилось и попало в снимок.
			box = fmt.Sprintf(
				`<style>html,body{width:%dpx;height:%dpx;overflow:hidden}</style>`, bw, bh)
		}
		fmt.Fprintf(w, `<!doctype html><html lang="ru"><head><meta charset="UTF-8">
<title>Focusd</title><script src="./__stub.js"></script>
<script type="module" crossorigin src="./assets/%s"></script>
<link rel="stylesheet" href="./assets/%s">%s</head><body><div id="app"></div></body></html>`, js, css, box)
	})

	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		return "", nil, err
	}
	srv := &http.Server{Handler: mux}
	go func() { _ = srv.Serve(ln) }()

	return fmt.Sprintf("http://%s/", ln.Addr().String()), func() { _ = srv.Close() }, nil
}

func dumpDOM(
	browser, dist, js, css string,
	state, settings map[string]any,
	o pageOpts,
) (string, error) {
	url, stop, err := serve(dist, js, css, state, settings, o)
	if err != nil {
		return "", err
	}
	defer stop()

	args := []string{"--dump-dom"}
	if o.w > 0 && o.h > 0 {
		// Размер окна важен и здесь: иначе проба измеряет геометрию одного
		// макета, а снимок делается для другого, и выводы расходятся.
		args = append(args, fmt.Sprintf("--window-size=%d,%d", o.w, o.h))
	}
	out, err := runBrowser(browser, url, args...)
	return string(out), err
}

func screenshot(
	browser, dist, js, css string,
	state, settings map[string]any,
	o pageOpts,
	path string,
) error {
	abs, err := filepath.Abs(path)
	if err != nil {
		return err
	}
	url, stop, err := serve(dist, js, css, state, settings, o)
	if err != nil {
		return err
	}
	defer stop()

	w, h := o.viewport()
	scale := o.scale
	if scale <= 0 {
		scale = 1
	}
	winW, winH := w, h
	if o.box {
		// Окно заведомо больше макета: Chrome не отдаёт окна уже ~500 px.
		winW, winH = 720, 560
	}
	args := []string{
		"--screenshot=" + abs,
		fmt.Sprintf("--window-size=%d,%d", winW, winH),
	}
	if o.scale > 0 {
		args = append(args, fmt.Sprintf("--force-device-scale-factor=%g", o.scale))
	}
	if _, err := runBrowser(browser, url, args...); err != nil {
		return err
	}
	if o.box {
		return cropPNG(abs, int(float64(w)*scale), int(float64(h)*scale))
	}
	return nil
}

// cropPNG оставляет от снимка только левый верхний прямоугольник w×h.
func cropPNG(path string, w, h int) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	src, err := png.Decode(f)
	_ = f.Close()
	if err != nil {
		return err
	}

	b := src.Bounds()
	if b.Dx() < w || b.Dy() < h {
		return fmt.Errorf("снимок %dx%d меньше нужных %dx%d", b.Dx(), b.Dy(), w, h)
	}
	dst := image.NewRGBA(image.Rect(0, 0, w, h))
	draw.Draw(dst, dst.Bounds(), src, b.Min, draw.Src)

	out, err := os.Create(path)
	if err != nil {
		return err
	}
	defer out.Close()
	return png.Encode(out, dst)
}

func runBrowser(browser, url string, extra ...string) ([]byte, error) {
	profile, err := os.MkdirTemp("", "focusd-chrome-*")
	if err != nil {
		return nil, err
	}
	defer os.RemoveAll(profile)

	common := append([]string{
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--hide-scrollbars",
		"--user-data-dir=" + profile,
		"--virtual-time-budget=6000",
	}, extra...)
	common = append(common, url)

	out, err := exec.Command(browser, append([]string{"--headless=new"}, common...)...).CombinedOutput()
	if err != nil && len(out) == 0 {
		// Старые сборки Chromium не понимают --headless=new.
		out, err = exec.Command(browser, append([]string{"--headless"}, common...)...).CombinedOutput()
	}
	if err != nil {
		return out, fmt.Errorf("--dump-dom: %w (%s)", err, truncate(string(out), 300))
	}
	return out, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "…"
}

func findBrowser() string {
	for _, b := range browsers {
		if _, err := os.Stat(b); err == nil {
			return b
		}
	}
	return ""
}

func fatal(err error) {
	fmt.Println("ОШИБКА:", err)
	os.Exit(1)
}

const stubJS = `
window.addEventListener('error', function (e) { document.title = 'JSERROR: ' + e.message; });
window.addEventListener('unhandledrejection', function (e) {
  document.title = 'JSREJECT: ' + ((e.reason && e.reason.message) || e.reason);
});
var D = window.__DATA;
var noop = function () { return Promise.resolve(); };
window.go = { main: { App: {
  GetState: function () { return Promise.resolve(D.state); },
  GetCatalog: function () { return Promise.resolve(D.catalog); },
  GetSettings: function () { return Promise.resolve(D.settings); },
  SaveSettings: noop, StartSession: noop, ConfirmCancel: noop, EmergencyRestore: noop,
  AddCustomDomain: noop, RemoveCustomDomain: noop,
  AddAllowlistDomain: noop, RemoveAllowlistDomain: noop, OpenDataDir: noop,
  SetTheme: noop,
  // Смена языка — единственное действие, на которое приложение отвечает
  // пересборкой всего кадра. Поэтому подмена не просто записывает вызов, а
  // рассылает новое состояние, как это делает Go: иначе проверка не увидела бы
  // главного — что интерфейс действительно переводится.
  SetLanguage: function (code) {
    window.__setLanguage = code;
    var next = JSON.parse(JSON.stringify(D.state));
    next.resolvedLanguage = code;
    next.language = code;
    D.state = next;
    if (window.__emit) window.__emit(next);
    return Promise.resolve();
  },
  // Последние вызовы запоминаются: так проверка видит, что кнопка действительно
  // что-то сделала, а не просто оказалась на месте.
  SetWidget: function (on) { window.__setWidget = on; return Promise.resolve(); },
  SetWidgetOnTop: function (on) { window.__setWidgetOnTop = on; return Promise.resolve(); },
  SetWidgetShape: function (shape) { window.__setWidgetShape = shape; return Promise.resolve(); },
  TestDomain: function (d) {
    var v = String(d).toLowerCase();
    var match = function (list) {
      return (list || []).some(function (x) {
        return v === x || v.slice(-(x.length + 1)) === '.' + x;
      });
    };
    if (match(D.state.allowlist)) return Promise.resolve(false);
    if (match(D.state.customDomains)) return Promise.resolve(true);
    var hit = D.catalog.some(function (g) {
      return (D.state.activeGroups || []).indexOf(g.id) >= 0 && match(g.domains);
    });
    return Promise.resolve(hit);
  },
  RequestCancel: function () { return Promise.resolve({ waitSec: 0, message: '' }); }
} } };
window.runtime = {
  EventsOn: function (name, cb) { window.__emit = cb; },
  WindowMinimise: noop, WindowToggleMaximise: noop, Quit: noop
};
// Панель настроек открывается кликом — воспроизводим это, если задан селектор.
if (D.click) {
  setTimeout(function () {
    var el = document.querySelector(D.click);
    if (el) el.click();
  }, 150);
}
// Прокрутка нужна, чтобы снять нижние разделы панели настроек: она длиннее окна.
if (D.scroll) {
  setTimeout(function () {
    var el = document.querySelector(D.scroll);
    if (el) el.scrollTop = el.scrollHeight;
  }, 260);
}
// Тема — настройка, а не действие окна: её кнопка обязана стоять рядом с
// «Настройками», а в заголовке должны остаться только кнопки самого окна.
if (D.probe === 'theme-place') {
  setTimeout(function () {
    var btn = document.querySelector('[aria-label^="Переключить оформление"]');
    if (!btn) { document.title = 'THEME-FAIL нет кнопки темы'; return; }
    if (btn.closest('.titlebar')) { document.title = 'THEME-FAIL кнопка темы в заголовке'; return; }
    if (!btn.closest('.side-foot')) { document.title = 'THEME-FAIL кнопка темы вне сайдбара'; return; }
    if (!btn.textContent.trim()) { document.title = 'THEME-FAIL у кнопки темы нет подписи'; return; }
    // Подпись без иконки уже была: span иконки находился тем же querySelector,
    // что и подпись, и текст затирал картинку.
    if (!btn.querySelector('svg')) { document.title = 'THEME-FAIL у кнопки темы нет иконки'; return; }
    var wins = document.querySelectorAll('.titlebar .win-btn');
    if (wins.length !== 3) { document.title = 'THEME-FAIL в заголовке ' + wins.length + ' кнопок окна'; return; }
    document.title = 'THEME-OK';
  }, 320);
}

// Переключатель языка обязан быть живым: смена языка пересобирает интерфейс
// целиком, и проверить это можно только настоящим кликом по пункту списка.
if (D.probe === 'lang-pick') {
  setTimeout(function () {
    var group = document.querySelector('[data-action="language"]');
    if (!group) { document.title = 'LANG-FAIL нет выбора языка'; return; }
    var opts = group.querySelectorAll('.theme-opt');
    if (opts.length !== 4) { document.title = 'LANG-FAIL пунктов ' + opts.length; return; }
    // Второй пункт — «Русский»: порядок списка одинаков на всех языках, а
    // подпись переводится, поэтому проверяется место, а не текст.
    var ru = opts[1];
    // До пункта нужно ещё добраться: панель настроек длиннее окна.
    ru.scrollIntoView({ block: 'center' });
    setTimeout(function () {
      var r = ru.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) { document.title = 'LANG-FAIL пункт не отрисован'; return; }
      var hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      if (!hit || !ru.contains(hit)) {
        document.title = 'LANG-FAIL пункт не получает клики: ' +
          (hit ? (hit.className || hit.tagName) : 'null');
        return;
      }
      ru.click();
      if (ru.getAttribute('aria-checked') !== 'true') {
        document.title = 'LANG-FAIL выбор не переключился';
        return;
      }
      if (window.__setLanguage !== 'ru') {
        document.title = 'LANG-FAIL язык не сохранён: ' + window.__setLanguage;
        return;
      }
      // Главное: кадр обязан пересобраться на новом языке. Заодно проверяется
      // каркас окна — его подписи собраны ещё при загрузке модуля, когда язык
      // не прочитан, и уже один раз так и остались русскими.
      setTimeout(function () {
        var side = document.querySelector('[data-i18n="side.settings"]');
        if (!side || side.textContent !== 'Настройки') {
          document.title = 'LANG-FAIL подпись в сайдбаре: ' + (side ? side.textContent : 'нет');
          return;
        }
        var title = document.querySelector('.settings-head h2');
        if (!title || title.textContent !== 'Настройки') {
          document.title = 'LANG-FAIL заголовок панели: ' + (title ? title.textContent : 'нет');
          return;
        }
        if (document.documentElement.lang !== 'ru') {
          document.title = 'LANG-FAIL атрибут lang: ' + document.documentElement.lang;
          return;
        }
        if (document.querySelectorAll('[data-action="language"] .theme-opt').length !== 4) {
          document.title = 'LANG-FAIL панель не пересобралась';
          return;
        }
        document.title = 'LANG-OK';
      }, 200);
    }, 80);
  }, 320);
}

// Выбор формы компактного таймера обязан быть живым. Проверяем не наличие
// кнопки, а то, что она получает клики: click() срабатывает и по элементу с
// pointer-events:none, поэтому бить нужно hit-тестом в центр кнопки.
if (D.probe === 'shape-pick') {
  setTimeout(function () {
    var opts = document.querySelectorAll('.seg-shape .seg-opt');
    if (opts.length !== 2) { document.title = 'SHAPE-FAIL кнопок формы ' + opts.length; return; }
    var ring = null;
    for (var i = 0; i < opts.length; i++) {
      if (opts[i].textContent.indexOf('Круг') >= 0) ring = opts[i];
    }
    if (!ring) { document.title = 'SHAPE-FAIL нет кнопки «Круг»'; return; }
    // Панель настроек длиннее окна, а попасть мышью можно только туда, что
    // видно: сначала доводим кнопку до экрана, как это сделал бы человек.
    ring.scrollIntoView({ block: 'center' });
    setTimeout(function () {
      var r = ring.getBoundingClientRect();
      if (r.width < 1 || r.height < 1) { document.title = 'SHAPE-FAIL кнопка не отрисована'; return; }
      var hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
      if (!hit || !ring.contains(hit)) {
        document.title = 'SHAPE-FAIL кнопка не получает клики: ' +
          [Math.round(r.left), Math.round(r.top), Math.round(r.width), Math.round(r.height)].join(',') +
          ' -> ' + (hit ? (hit.className || hit.tagName) : 'null');
        return;
      }
      ring.click();
      if (ring.getAttribute('aria-checked') !== 'true') {
        document.title = 'SHAPE-FAIL выбор формы не переключился';
        return;
      }
      if (window.__setWidgetShape !== 'ring') {
        document.title = 'SHAPE-FAIL форма не сохранена: ' + window.__setWidgetShape;
        return;
      }
      // Второй тумблер карточки — «держать поверх всех окон». Он тоже относится
      // к компактному режиму и настраивается заранее, поэтому не имеет права
      // быть выключенным: в плашке панель настроек недоступна.
      var onTop = document.querySelector('input[aria-label="Держать компактный таймер поверх всех окон"]');
      if (!onTop) { document.title = 'ONTOP-FAIL нет тумблера «поверх всех окон»'; return; }
      if (onTop.disabled) { document.title = 'ONTOP-FAIL тумблер выключен'; return; }
      var was = onTop.checked;
      onTop.click();
      if (onTop.checked === was || window.__setWidgetOnTop !== !was) {
        document.title = 'ONTOP-FAIL тумблер не сработал: ' + window.__setWidgetOnTop;
        return;
      }
      document.title = 'SHAPE-OK';
    }, 200);
  }, 320);
}

// Виджет без сессии обязан оставлять выход в обычное окно: иначе компактный
// режим — ловушка, потому что сайдбара с тумблером в нём нет. Проверяем
// видимость кнопки и то, что клик по ней действительно снимает режим.
if (D.probe === 'widget-out') {
  setTimeout(function () {
    var btn = document.querySelector('.widget-back');
    if (!btn) { document.title = 'WIDGET-FAIL нет кнопки выхода'; return; }
    var r = btn.getBoundingClientRect();
    if (r.width < 1 || r.height < 1) { document.title = 'WIDGET-FAIL кнопка выхода скрыта'; return; }
    var hit = document.elementFromPoint(r.left + r.width / 2, r.top + r.height / 2);
    if (!hit || !btn.contains(hit)) { document.title = 'WIDGET-FAIL кнопка выхода перекрыта'; return; }
    btn.click();
    if (window.__setWidget !== false) {
      document.title = 'WIDGET-FAIL выход не сработал: ' + window.__setWidget;
      return;
    }
    document.title = 'WIDGET-OK';
  }, 320);
}

// Наложение текста нельзя поймать проверкой классов: подпись карточки может
// лежать поверх заголовка, оставаясь при этом корректной разметкой. Поэтому
// меряем прямоугольники — так же, как это увидел бы человек.
if (D.probe === 'overlap') {
  setTimeout(function () {
    var cards = document.querySelectorAll('.card');
    if (cards.length === 0) { document.title = 'OVERLAP-FAIL нет карточек'; return; }
    var bad = [];
    for (var i = 0; i < cards.length; i++) {
      var title = cards[i].querySelector('.card-title');
      var hint = cards[i].querySelector('.card-hint');
      if (!title || !hint) continue;
      if (!hint.textContent.trim()) continue;
      var a = title.getBoundingClientRect();
      var b = hint.getBoundingClientRect();
      var shifted = a.right <= b.left + 0.5 || b.right <= a.left + 0.5;
      var clear = b.top >= a.bottom - 1 || a.top >= b.bottom - 1;
      if (!shifted && !clear) {
        bad.push(title.textContent + ' / ' + hint.textContent);
      }
    }
    document.title = bad.length === 0
      ? 'OVERLAP-OK ' + cards.length
      : 'OVERLAP-FAIL ' + bad.join(' | ');
  }, 320);
}
// Проверка поведения, а не наличия: линейка обязана менять значение со стрелки
// и обязана НЕ менять его на клавише, которую не обрабатывает. Второе условие
// важно: без него проверка проходила бы даже на полностью мёртвом обработчике.
if (D.probe === 'ruler') {
  setTimeout(function () {
    var r = document.querySelector('.ruler');
    if (!r) { document.title = 'RULER-FAIL нет .ruler'; return; }
    var max = Number(r.getAttribute('aria-valuemax'));
    var v0 = Number(r.getAttribute('aria-valuenow'));
    r.dispatchEvent(new KeyboardEvent('keydown', { key: 'ArrowRight', bubbles: true }));
    var v1 = Number(r.getAttribute('aria-valuenow'));
    r.dispatchEvent(new KeyboardEvent('keydown', { key: 'End', bubbles: true }));
    var v2 = Number(r.getAttribute('aria-valuenow'));
    r.dispatchEvent(new KeyboardEvent('keydown', { key: 'F5', bubbles: true }));
    var v3 = Number(r.getAttribute('aria-valuenow'));
    var ok = v1 === v0 + 5 && v2 === max && v3 === v2;
    document.title = (ok ? 'RULER-OK ' : 'RULER-FAIL ') + [v0, v1, v2, v3].join('->');
  }, 150);
}
`
