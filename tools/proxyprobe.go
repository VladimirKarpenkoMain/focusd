//go:build ignore

// Проверка прокси Focusd на живой сети: пропускает ли он сайты и режет ли
// заблокированные.
//
// Запуск: go run tools/proxyprobe.go
//
// Реестр не трогается: каждый браузер запускается в отдельном профиле и
// направляется на прокси флагом --proxy-server, поэтому ваши открытые окна и
// настройки системы остаются нетронутыми.
package main

import (
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"strings"

	"focusd/internal/catalog"
	"focusd/internal/proxy"
)

func main() {
	log := slog.New(slog.NewTextHandler(io.Discard, nil))

	// Набор правил как в строгой сессии по умолчанию: четыре группы плюс DoH.
	domains := catalog.GroupsByID([]string{"social", "video", "shorts", "doomscroll"})
	domains = append(domains, catalog.DoH()...)

	p := proxy.New(0, log)
	p.SetRules(domains)
	p.SetEnabled(true)
	if err := p.Start(); err != nil {
		fatal("прокси не поднялся: %v", err)
	}
	defer p.Stop()
	addr := p.Addr()
	fmt.Printf("правил %d, прокси на %s\n\n", len(domains), addr)

	sites := []struct {
		url    string
		expect string
	}{
		{"https://example.com", "открыться"},
		{"https://www.wikipedia.org", "открыться"},
		{"https://github.com", "открыться"},
		{"https://mail.google.com", "открыться"},
		{"https://youtube.com", "быть закрыт"},
		{"https://vk.com", "быть закрыт"},
		{"https://www.instagram.com", "быть закрыт"},
	}

	chrome := findChrome()
	if chrome == "" {
		fmt.Println("Chrome не найден — проверяю только через curl")
	}

	for _, s := range sites {
		line := fmt.Sprintf("%-26s curl %-4s", strings.TrimPrefix(s.url, "https://"), curlVia(addr, s.url))
		if chrome != "" {
			line += "  браузер: " + chromeLoad(chrome, addr, s.url)
		}
		fmt.Printf("%s   (должен %s)\n", line, s.expect)
	}

	requests, blocked := p.Stats()
	forwarded, failed := p.Health()
	fmt.Printf("\nзапросов через прокси %d, отсечено %d, доехало %d, не доехало %d\n",
		requests, blocked, forwarded, failed)
	fmt.Printf("последний закрытый сайт: %q\n", p.LastBlocked())

	// Ключевая проверка: выключение блокировки не должно ломать доступ.
	// Именно на этом ломался браузер: политика снималась, а прокси пропадал.
	fmt.Println("\nвыключаю блокировку, прокси продолжает работать:")
	p.SetEnabled(false)
	for _, u := range []string{"https://youtube.com", "https://vk.com", "https://example.com"} {
		line := fmt.Sprintf("%-26s curl %-4s", strings.TrimPrefix(u, "https://"), curlVia(addr, u))
		if chrome != "" {
			line += "  браузер: " + chromeLoad(chrome, addr, u)
		}
		fmt.Printf("%s   (должен открыться)\n", line)
	}
	reqAfter, blockedAfter := p.Stats()
	fmt.Printf("счётчик замер: было %d/%d, стало %d/%d\n", requests, blocked, reqAfter, blockedAfter)
	if reqAfter != requests || blockedAfter != blocked {
		fmt.Println("ОШИБКА: вне сессии прокси не должен ни считать, ни резать")
	} else {
		fmt.Println("вне сессии прокси ничего не считает и не режет — как задумано")
	}

	if forwarded == 0 {
		fmt.Println("\nИТОГ: прокси не водит трафик — браузерам его прописывать нельзя.")
		return
	}
	if blocked == 0 {
		fmt.Println("\nИТОГ: прокси не режет заблокированные сайты.")
		return
	}
	fmt.Println("\nИТОГ: прокси и пропускает, и режет, и отпускает — как задумано.")
}

// chromeLoad открывает сайт в отдельном профиле Chrome через прокси.
// Признаки ошибок сети Chromium печатает прямо в текст страницы.
func chromeLoad(chrome, addr, url string) string {
	profile, err := os.MkdirTemp("", "focusd-site-*")
	if err != nil {
		return "нет профиля"
	}
	defer os.RemoveAll(profile)

	out, err := exec.Command(chrome,
		"--headless=new",
		"--disable-gpu",
		"--no-first-run",
		"--no-default-browser-check",
		"--user-data-dir="+profile,
		"--proxy-server=http://"+addr,
		"--virtual-time-budget=9000",
		"--dump-dom",
		url,
	).CombinedOutput()
	text := string(out)

	switch {
	case strings.Contains(text, "ERR_PROXY_CONNECTION_FAILED"):
		return "прокси недоступен"
	case strings.Contains(text, "ERR_TUNNEL_CONNECTION_FAILED"):
		return "туннель не открылся"
	case strings.Contains(text, "ERR_CONNECTION_RESET"):
		return "соединение сброшено"
	case strings.Contains(text, "ERR_"):
		return "ошибка сети"
	case len(text) > 1500:
		return fmt.Sprintf("открылся (%d байт)", len(text))
	case err != nil:
		return fmt.Sprintf("пусто (%v)", err)
	default:
		return fmt.Sprintf("пусто (%d байт)", len(text))
	}
}

func curlVia(addr, url string) string {
	out, err := exec.Command("curl.exe",
		"-s", "-o", os.DevNull, "-w", "%{http_code}",
		"--max-time", "20", "--proxy", "http://"+addr, url).CombinedOutput()
	text := strings.TrimSpace(string(out))
	if err != nil && text == "" {
		return "ошибка"
	}
	return text
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
