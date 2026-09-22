//go:build ignore

// Проба отвечает на два вопроса, от которых зависит, придётся ли человеку
// перезапускать браузер.
//
// Первый: подхватывает ли Chromium политику прокси ProxySettings в уже
// запущенном окне. Второй: подхватывает ли уже запущенный браузер смену
// СИСТЕМНОГО прокси — им закрываются браузеры, которые политик не читают
// (Яндекс Браузер: на живом браузере видно, что политику он игнорирует, а
// системный прокси слушает).
//
// Запуск ОТ ИМЕНИ АДМИНИСТРАТОРА (иначе политику в реестр не записать), и
// только когда Focusd закрыт: проба переставляет политики машины.
//
//	go run tools/policyprobe.go
//
// Проба пишет и снимает политику ProxySettings для браузеров и подменяет
// системный прокси — так же, как это делает Focusd, — и всегда убирает за
// собой, даже при ошибке.
//
// Логика: Chrome запускается при политике на мёртвый порт. Дальше политика
// переставляется на живой прокси, потом снимается, и на каждом шаге браузер
// (без перезапуска!) загружает страницу. По числу запросов, доехавших до
// живого прокси, видно, подхватил ли браузер новую политику. Второй сценарий
// повторяет то же самое с системным прокси на отдельном, заведомо «чистом»
// браузере.
package main

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"os/exec"
	"time"

	"focusd/internal/proxy"
	"focusd/internal/winsys"
)

const (
	cdpPort  = 9223
	deadAddr = "127.0.0.1:47999"
	testURL  = "http://example.com/"
)

func main() {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	if !winsys.IsElevated() {
		fmt.Println("Нужны права администратора: без них политику в реестр не записать.")
		fmt.Println("Запустите в PowerShell от администратора:")
		fmt.Println("  go run tools/policyprobe.go")
		os.Exit(1)
	}

	chrome := findChrome()
	if chrome == "" {
		fatal("Chrome не найден")
	}

	live := proxy.New(0, log)
	live.SetRules(nil)
	live.SetEnabled(false)
	if err := live.Start(); err != nil {
		fatal("прокси-заглушка не поднялась: %v", err)
	}
	defer live.Stop()

	// Политику снимаем при любом выходе — оставлять её нельзя.
	defer func() {
		if err := winsys.ClearBrowserProxy(); err != nil {
			fmt.Printf("ВНИМАНИЕ: не удалось снять политику: %v\n", err)
		} else {
			fmt.Println("\nполитика снята")
		}
	}()

	browser := &chromeCDP{chrome: chrome, port: cdpPort}
	defer browser.stop()

	fmt.Println("шаг 1: поднимаю политику на заведомо мёртвый порт")
	mustPolicy(deadAddr)
	browser.start()
	browser.open(testURL)
	time.Sleep(4 * time.Second)
	fmt.Println("  Chrome запущен с политикой на мёртвый порт")

	fmt.Println("шаг 2: переставляю политику на живой прокси, браузер НЕ перезапускаю")
	live.SetEnabled(true)
	mustPolicy(live.Addr())
	browser.open(testURL)
	browser.open("http://www.wikipedia.org/")
	time.Sleep(6 * time.Second)
	reqA, _ := live.Stats()
	fmt.Printf("  через живой прокси прошло запросов: %d\n", reqA)

	fmt.Println("шаг 3: снимаю политику, браузер всё ещё работает")
	if err := winsys.ClearBrowserProxy(); err != nil {
		fatal("не удалось снять политику: %v", err)
	}
	browser.open(testURL)
	browser.open("http://neverssl.com/")
	time.Sleep(6 * time.Second)
	reqB, _ := live.Stats()
	fmt.Printf("  через живой прокси прошло запросов всего: %d\n", reqB)

	fmt.Println()
	if reqA > 0 {
		fmt.Printf("ВЫВОД: Chrome подхватил политику прокси на ходу (%d запросов через живой прокси).\n", reqA)
		fmt.Println("       Перезапуск браузера при включении фокуса не нужен.")
	} else {
		fmt.Println("ВЫВОД: Chrome НЕ подхватил политику прокси в запущенном окне.")
		fmt.Println("       Значит, для смены прокси браузер нужно перезапускать.")
	}

	if reqB > reqA {
		fmt.Printf("       После снятия политики через прокси прошло ещё %d запросов —\n", reqB-reqA)
		fmt.Println("       значит, браузер на неё не опирается и доступ не блокирует.")
	} else {
		fmt.Println("       После снятия политики живого прокси браузер больше не касался:")
		fmt.Println("       доступ он берёт откуда-то ещё — если страницы не открываются,")
		fmt.Println("       браузер держит прокси в кэше и требует перезапуска.")
	}

	systemProxyScenario(chrome, live)
}

// systemProxyScenario проверяет второе, что важно для «без перезапуска»:
// подхватывает ли УЖЕ ЗАПУЩЕННЫЙ браузер смену системного прокси.
//
// Это тот слой, которым Focusd закрывает браузеры, не читающие политик:
// проверено на живом Яндексе — политику Focusd он игнорирует, а системный
// прокси слушает. Вопрос здесь ровно один: нужен ли для этого перезапуск.
func systemProxyScenario(chrome string, live *proxy.Server) {
	fmt.Println()
	fmt.Println("шаг 4: системный прокси — подхватывает ли его запущенный браузер")

	prev, err := winsys.SystemProxySettingsNow()
	if err != nil {
		fmt.Printf("  ПРОПУСК: не удалось прочитать системный прокси: %v\n", err)
		return
	}
	// Настройки человека возвращаются при любом выходе: это его машина, и
	// оставить её с чужим прокси нельзя.
	defer func() {
		if err := winsys.RestoreSystemProxy(prev); err != nil {
			fmt.Printf("  ВНИМАНИЕ: не удалось вернуть настройки системного прокси: %v\n", err)
			return
		}
		fmt.Println("  настройки системного прокси возвращены как были")
	}()

	// Браузер должен стартовать без прокси — иначе проверять будет нечего.
	if err := winsys.RestoreSystemProxy(winsys.SystemProxySettings{}); err != nil {
		fmt.Printf("  ПРОПУСК: не удалось выключить системный прокси: %v\n", err)
		return
	}

	browser := &chromeCDP{chrome: chrome, port: cdpPort + 1}
	defer browser.stop()
	browser.start()
	browser.open(testURL)
	time.Sleep(4 * time.Second)
	before, _ := live.Stats()
	fmt.Printf("  браузер запущен без прокси, через живой прокси прошло: %d\n", before)

	fmt.Println("  направляю системный прокси на живой прокси, браузер НЕ перезапускаю")
	if _, err := winsys.SetSystemProxy(live.Addr()); err != nil {
		fmt.Printf("  ПРОПУСК: не удалось прописать системный прокси: %v\n", err)
		return
	}
	browser.open(testURL)
	browser.open("http://www.wikipedia.org/")
	time.Sleep(6 * time.Second)
	after, _ := live.Stats()

	fmt.Println()
	if after > before {
		fmt.Printf("ВЫВОД: запущенный браузер подхватил системный прокси на ходу (+%d запросов).\n",
			after-before)
		fmt.Println("       Значит, и этот слой работает без перезапуска браузера.")
		return
	}
	fmt.Println("ВЫВОД: запущенный браузер НЕ подхватил системный прокси —")
	fmt.Println("       для него нужен перезапуск, и об этом человека надо предупредить.")
}

/* --- Chrome через протокол DevTools -------------------------------------- */

type chromeCDP struct {
	chrome  string
	port    int
	cmd     *exec.Cmd
	profile string
}

func (c *chromeCDP) start() {
	dir, err := os.MkdirTemp("", "focusd-policy-*")
	if err != nil {
		fatal("временный профиль: %v", err)
	}
	c.profile = dir

	// Обычное окно, а не headless: headless-процесс поднимается заново на каждый
	// запрос, и «подхватил ли живой браузер» на нём не проверить.
	c.cmd = exec.Command(c.chrome,
		fmt.Sprintf("--remote-debugging-port=%d", c.port),
		"--user-data-dir="+dir,
		"--no-first-run",
		"--no-default-browser-check",
		"--disable-gpu",
		"about:blank",
	)
	if err := c.cmd.Start(); err != nil {
		fatal("не удалось запустить Chrome: %v", err)
	}

	for i := 0; i < 60; i++ {
		time.Sleep(500 * time.Millisecond)
		if resp, err := http.Get(c.debugURL() + "/json/version"); err == nil {
			resp.Body.Close()
			return
		}
	}
	fatal("Chrome не открыл порт отладки")
}

func (c *chromeCDP) debugURL() string {
	return fmt.Sprintf("http://127.0.0.1:%d", c.port)
}

func (c *chromeCDP) stop() {
	if c.cmd != nil && c.cmd.Process != nil {
		_ = c.cmd.Process.Kill()
		_, _ = c.cmd.Process.Wait()
	}
	if c.profile != "" {
		// Профиль чистим не сразу: Chrome дописывает его при выходе.
		time.Sleep(time.Second)
		_ = os.RemoveAll(c.profile)
	}
}

type cdpTarget struct {
	ID                   string `json:"id"`
	Type                 string `json:"type"`
	WebSocketDebuggerURL string `json:"webSocketDebuggerUrl"`
}

func (c *chromeCDP) targets() []cdpTarget {
	resp, err := http.Get(c.debugURL() + "/json/list")
	if err != nil {
		return nil
	}
	defer resp.Body.Close()
	var all []cdpTarget
	if err := json.NewDecoder(resp.Body).Decode(&all); err != nil {
		return nil
	}
	var pages []cdpTarget
	for _, t := range all {
		if t.Type == "page" && t.WebSocketDebuggerURL != "" {
			pages = append(pages, t)
		}
	}
	return pages
}

// open просит уже запущенный Chrome открыть вкладку. Именно HTTP-эндпоинт
// отладки, а не WebSocket: так проба обходится без клиента протокола DevTools,
// а нам важно лишь одно — чтобы живое окно сходило по адресу.
func (c *chromeCDP) open(url string) {
	req, err := http.NewRequest(http.MethodPut, c.debugURL()+"/json/new?"+url, nil)
	if err != nil {
		return
	}
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_, _ = io.Copy(io.Discard, resp.Body)
	resp.Body.Close()
}

func mustPolicy(addr string) {
	if err := winsys.SetBrowserProxy(addr); err != nil {
		fatal("не удалось записать политику: %v", err)
	}
}

func findChrome() string {
	for _, c := range []string{
		`C:\Program Files\Google\Chrome\Application\chrome.exe`,
		`C:\Program Files (x86)\Google\Chrome\Application\chrome.exe`,
	} {
		if _, err := os.Stat(c); err == nil {
			return c
		}
	}
	return ""
}

func fatal(format string, args ...any) {
	fmt.Printf("ОШИБКА: "+format+"\n", args...)
	os.Exit(1)
}
