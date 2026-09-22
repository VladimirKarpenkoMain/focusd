// Package blockpage рисует страницу-заглушку: то, что человек видит вместо
// закрытого сайта.
//
// Пакет существует отдельно от слоя блокировки намеренно. Заглушка нужна не
// резолверу и не прокси как таковым — она нужна интерфейсу приложения, а слой
// блокировки лишь передаёт её браузеру. Пока страница жила внутри DNS-сервера,
// удалить перехват DNS было нельзя, не утащив за собой оформление.
package blockpage

import (
	"fmt"
	"html/template"
	"io"

	"focusd/internal/i18n"
)

// Info описывает состояние фокуса для страницы-заглушки.
type Info struct {
	Active     bool
	Host       string
	Remaining  int // секунды
	Strictness string
	Blocked    int64
	// Lang — язык страницы. Пустое значение означает русский: страница —
	// единственное место, где текст рисует Go, а не интерфейс, поэтому язык
	// приходит сюда вместе с остальными данными о сессии.
	Lang i18n.Lang
}

const pageHTML = `<!doctype html>
<html lang="{{.Locale}}">
<head>
<meta charset="utf-8">
<meta name="viewport" content="width=device-width,initial-scale=1">
<meta http-equiv="refresh" content="5">
<title>{{.Texts.PageTitle}}</title>
<style>
  /* Страница живёт в том же визуальном языке, что и приложение: свои
     переменные, системный шрифт, поддержка светлой и тёмной темы. */
  :root{
    --bg:#f2f3f6; --surface:#ffffff; --border:#e1e4ea; --hairline:#edeff3;
    --text:#14161b; --text-2:#5a6170; --text-3:#8a91a0;
    --accent:#4c5ce8; --accent-2:#8b7cf6; --accent-soft:rgba(76,92,232,.10);
    --shadow:0 20px 55px rgba(16,20,32,.14);
  }
  @media (prefers-color-scheme: dark){
    :root{
      --bg:#0e0f13; --surface:#16181e; --border:#272b34; --hairline:#1f232b;
      --text:#f1f2f5; --text-2:#9ba2af; --text-3:#6c7484;
      --accent:#7c8cff; --accent-2:#a78bfa; --accent-soft:rgba(124,140,255,.14);
      --shadow:0 26px 70px rgba(0,0,0,.6);
    }
  }
  *{box-sizing:border-box}
  html,body{height:100%}
  body{
    margin:0; background:var(--bg); color:var(--text);
    font:16px/1.5 'Segoe UI Variable Text','Segoe UI',system-ui,-apple-system,Roboto,'Microsoft YaHei UI','Microsoft YaHei',sans-serif;
    display:flex; align-items:center; justify-content:center; padding:24px;
    -webkit-font-smoothing:antialiased;
  }
  .card{
    width:100%; max-width:420px; text-align:center;
    background:var(--surface); border:1px solid var(--border);
    border-radius:22px; padding:38px 30px 30px;
    box-shadow:var(--shadow);
  }
  .mark{
    width:66px; height:66px; margin:0 auto 20px; border-radius:20px;
    display:grid; place-items:center;
    background:linear-gradient(145deg,var(--accent),var(--accent-2));
    box-shadow:0 10px 28px var(--accent-soft);
  }
  h1{
    margin:0 0 6px; font-size:22px; font-weight:650; letter-spacing:-.02em;
    font-family:'Segoe UI Variable Display','Segoe UI',system-ui,'Microsoft YaHei UI','Microsoft YaHei',sans-serif;
  }
  .sub{color:var(--text-2); font-size:14px; margin:0}
  .host{
    display:inline-block; margin:16px 0 0; padding:5px 12px; border-radius:999px;
    background:var(--bg); border:1px solid var(--border);
    font:500 12.5px/1.4 'Cascadia Mono',ui-monospace,Consolas,'Microsoft YaHei UI','Microsoft YaHei',monospace; color:var(--text-2);
    word-break:break-all; max-width:100%;
  }
  .timer{
    font:300 44px/1.1 'Segoe UI Variable Display','Segoe UI',system-ui,'Microsoft YaHei UI','Microsoft YaHei',sans-serif;
    font-variant-numeric:tabular-nums; letter-spacing:-.045em; margin:18px 0 2px;
    color:var(--text);
  }
  .timer-label{color:var(--text-3); font-size:12.5px; margin:0}
  .meta{
    margin-top:24px; padding-top:16px; border-top:1px solid var(--hairline);
    display:flex; justify-content:center; gap:26px; color:var(--text-3); font-size:12px;
  }
  .meta b{display:block; margin-top:2px; color:var(--text); font-size:15px; font-weight:600; font-variant-numeric:tabular-nums}
</style>
</head>
<body>
  <div class="card">
    <div class="mark">
      <svg width="30" height="30" viewBox="0 0 24 24" fill="none" stroke="#fff" stroke-width="1.9"
           stroke-linecap="round" stroke-linejoin="round" aria-hidden="true">
        <rect x="4" y="10.5" width="16" height="10.5" rx="3"/>
        <path d="M8 10.5V7a4 4 0 0 1 8 0v3.5"/>
      </svg>
    </div>
    {{if .Active}}
      <h1>{{.Texts.ActiveTitle}}</h1>
      <p class="sub">{{.Texts.ActiveSub}}</p>
      <div class="timer">{{.RemainingText}}</div>
      <p class="timer-label">{{.Texts.Remaining}}</p>
    {{else}}
      <h1>{{.Texts.DoneTitle}}</h1>
      <p class="sub">{{.Texts.DoneSub}}</p>
    {{end}}
    {{if .Host}}<div class="host">{{.Host}}</div>{{end}}
    <div class="meta">
      <div>{{.Texts.Mode}}<b>{{.StrictnessText}}</b></div>
      <div>{{.Texts.Blocked}}<b>{{.Blocked}}</b></div>
    </div>
  </div>
</body>
</html>`

var tmpl = template.Must(template.New("block").Parse(pageHTML))

// texts — подписи страницы на выбранном языке. Собираются в структуру, потому
// что их читает разметка: разложить это по отдельным полям Info значило бы
// повторить в Info всю разметку страницы.
type texts struct {
	PageTitle   string
	ActiveTitle string
	ActiveSub   string
	Remaining   string
	DoneTitle   string
	DoneSub     string
	Mode        string
	Blocked     string
}

func textsFor(l i18n.Lang) texts {
	return texts{
		PageTitle:   i18n.T(l, "blockpage.title"),
		ActiveTitle: i18n.T(l, "blockpage.active.title"),
		ActiveSub:   i18n.T(l, "blockpage.active.sub"),
		Remaining:   i18n.T(l, "blockpage.remaining"),
		DoneTitle:   i18n.T(l, "blockpage.done.title"),
		DoneSub:     i18n.T(l, "blockpage.done.sub"),
		Mode:        i18n.T(l, "blockpage.mode"),
		Blocked:     i18n.T(l, "blockpage.blocked"),
	}
}

// Render рисует страницу-заглушку.
func Render(w io.Writer, info Info) error {
	data := struct {
		Info
		RemainingText  string
		StrictnessText string
		Locale         string
		Texts          texts
	}{Info: info}
	if data.Active {
		data.RemainingText = hhmmss(data.Remaining)
	}
	data.StrictnessText = i18n.Strictness(info.Lang, data.Strictness)
	data.Locale = i18n.Locale(info.Lang)
	data.Texts = textsFor(info.Lang)
	return tmpl.Execute(w, data)
}

// strictnessText называет уровень строгости по-русски. Оставлено ради читаемости
// вызовов без языка: русский — исходный текст приложения.
func strictnessText(s string) string {
	return i18n.Strictness(i18n.RU, s)
}

func hhmmss(sec int) string {
	if sec < 0 {
		sec = 0
	}
	h, m, s := sec/3600, (sec%3600)/60, sec%60
	if h > 0 {
		return fmt.Sprintf("%d:%02d:%02d", h, m, s)
	}
	return fmt.Sprintf("%02d:%02d", m, s)
}
