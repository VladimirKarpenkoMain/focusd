// Package catalog содержит встроенный набор групп блокируемых доменов.
package catalog

// Group — именованный набор доменов, который пользователь может включить целиком.
type Group struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Icon    string   `json:"icon"`
	Note    string   `json:"note"`
	Domains []string `json:"domains"`
}

// dohProviders — домены публичных DNS-over-HTTPS резолверов.
// Блокируются в строгих режимах, иначе браузер обойдёт локальный DNS.
var dohProviders = []string{
	"dns.google",
	"dns.google.com",
	"cloudflare-dns.com",
	"mozilla.cloudflare-dns.com",
	"chrome.cloudflare-dns.com",
	"one.one.one.one",
	"dns.quad9.net",
	"doh.opendns.com",
	"doh.dns.sb",
	"dns.nextdns.io",
	"doh.cleanbrowsing.org",
	"doh.adguard.com",
	"dns.adguard-dns.com",
	"doh.mullvad.net",
	"dns.alidns.com",
	"doh.pub",
	"doh.360.cn",
}

var groups = []Group{
	{
		ID:    "social",
		Title: "Соцсети",
		Icon:  "users",
		Note:  "Ленты, сторис, бесконечный скролл",
		Domains: []string{
			"instagram.com", "cdninstagram.com", "facebook.com", "fb.com", "fb.watch",
			"fbcdn.net", "fbsbx.com", "messenger.com", "threads.net", "threads.com",
			"x.com", "twitter.com", "t.co", "twimg.com",
			"tiktok.com", "tiktokcdn.com", "tiktokv.com", "musical.ly", "byteoversea.com",
			"vk.com", "vk.ru", "vkvideo.ru", "userapi.com", "vk-cdn.net",
			"ok.ru", "mycdn.me",
			"reddit.com", "redd.it", "redditmedia.com", "redditstatic.com",
			"snapchat.com", "sc-cdn.net",
			"linkedin.com", "licdn.com",
			"pinterest.com", "pinimg.com",
			"tumblr.com",
			"likee.video", "like.video",
			"weibo.com", "xiaohongshu.com",
		},
	},
	{
		ID:    "video",
		Title: "Видео и стриминг",
		Icon:  "play",
		Note:  "YouTube, Twitch, сериалы",
		Domains: []string{
			"youtube.com", "youtu.be", "ytimg.com", "youtube-nocookie.com",
			"googlevideo.com", "yt3.ggpht.com", "ggpht.com", "youtubei.googleapis.com",
			"ytstatic.com", "youtubeeducation.com",
			"netflix.com", "nflxvideo.net", "nflximg.net", "nflxso.net",
			"twitch.tv", "ttvnw.net", "jtvnw.net", "twitchcdn.net",
			"vimeo.com", "vimeocdn.com",
			"rutube.ru", "vkvideo.ru",
			"hulu.com", "disneyplus.com", "primevideo.com", "hbomax.com", "max.com",
			"kick.com", "trovo.live", "dlive.tv",
			"kinopoisk.ru", "ivi.ru", "okko.tv", "more.tv", "wink.ru", "amediateka.ru",
			"crunchyroll.com", "dailymotion.com", "dmcdn.net",
		},
	},
	{
		ID:    "shorts",
		Title: "Короткие видео",
		Icon:  "zap",
		Note:  "Reels, Shorts, клипы",
		Domains: []string{
			"reels.instagram.com",
			"reels.cdninstagram.com",
			"kwai.com", "kwai.net", "kwaicdn.com",
			"triller.co", "clashapp.co",
			"douyin.com", "douyinvod.com",
		},
	},
	{
		ID:    "games",
		Title: "Игры",
		Icon:  "gamepad",
		Note:  "Магазины, лончеры, игровые порталы",
		Domains: []string{
			"store.steampowered.com", "steamcommunity.com", "steampowered.com",
			"steamstatic.com", "steamcontent.com", "steam-chat.com",
			"epicgames.com", "epicgames.dev", "unrealengine.com", "easyanticheat.net",
			"roblox.com", "rbxcdn.com", "robloxlabs.com",
			"minecraft.net", "mojang.com",
			"battle.net", "blizzard.com", "blzstatic.cn",
			"riotgames.com", "leagueoflegends.com", "valorant.com", "riotcdn.net",
			"gog.com", "gog-statics.com",
			"ea.com", "origin.com", "eaassets-a.akamaihd.net",
			"ubisoft.com", "ubistatic-a.akamaihd.net",
			"playstation.com", "xbox.com", "xboxlive.com",
			"wargaming.net", "warthunder.com", "gaijin.net",
			"faceit.com", "esea.net",
			"chess.com", "lichess.org",
			"igdb.com", "op.gg", "u.gg", "tracker.gg",
		},
	},
	{
		ID:    "messengers",
		Title: "Мессенджеры",
		Icon:  "message",
		Note:  "Чаты, сервера, каналы",
		Domains: []string{
			"discord.com", "discordapp.com", "discord.gg", "discordapp.net",
			"discord.media", "discordstatus.com",
			"telegram.org", "t.me", "telegram.me", "telegra.ph", "web.telegram.org",
			"whatsapp.com", "whatsapp.net", "wa.me",
			"slack.com", "slack-edge.com",
			"signal.org",
		},
	},
	{
		ID:    "news",
		Title: "Новости и форумы",
		Icon:  "newspaper",
		Note:  "Новостные ленты и обсуждения",
		Domains: []string{
			"lenta.ru", "rbc.ru", "ria.ru", "tass.ru", "gazeta.ru",
			"kommersant.ru", "vedomosti.ru", "russian.rt.com", "meduza.io",
			"bbc.com", "bbc.co.uk", "cnn.com", "nytimes.com", "theguardian.com",
			"habr.com", "vc.ru", "pikabu.ru", "dtf.ru", "tj.ru",
			"4pda.to", "4pda.ru", "driver.ru",
			"news.ycombinator.com", "lobste.rs", "slashdot.org",
			"medium.com", "substack.com", "quora.com",
		},
	},
	{
		ID:    "shopping",
		Title: "Маркетплейсы",
		Icon:  "cart",
		Note:  "Магазины и доски объявлений",
		Domains: []string{
			"ozon.ru", "ozone.ru", "wildberries.ru", "wb.ru", "wbbasket.ru",
			"avito.ru", "avito.st", "youla.ru", "yula.ru",
			"aliexpress.com", "aliexpress.ru", "alicdn.com",
			"amazon.com", "amazon.co.uk", "amazon.de", "media-amazon.com", "ssl-images-amazon.com",
			"ebay.com", "ebayimg.com",
			"market.yandex.ru", "beru.ru",
			"dns-shop.ru", "citilink.ru", "mvideo.ru", "eldorado.ru",
			"lamoda.ru", "goldapple.ru", "letu.ru",
			"temu.com", "shein.com", "sheincdn.com",
		},
	},
	{
		ID:    "adult",
		Title: "18+",
		Icon:  "shield",
		Note:  "Контент для взрослых",
		Domains: []string{
			"pornhub.com", "phncdn.com", "xvideos.com", "xnxx.com", "xhamster.com",
			"redtube.com", "youporn.com", "youporngay.com", "spankbang.com",
			"onlyfans.com", "fansly.com", "chaturbate.com", "stripchat.com",
			"bongacams.com", "livejasmin.com", "cam4.com", "myfreecams.com",
			"brazzers.com", "bangbros.com", "realitykings.com", "mofos.com",
			"eporner.com", "hqporner.com", "txxx.com", "beeg.com",
			"nhentai.net", "rule34.xxx", "e-hentai.org",
		},
	},
	{
		ID:    "doomscroll",
		Title: "Doomscroll-ловушки",
		Icon:  "infinity",
		Note:  "Сервисы бесконечного листания",
		Domains: []string{
			"9gag.com", "imgur.com", "ifunny.co", "boredpanda.com",
			"buzzfeed.com", "boredombash.com", "distractify.com",
			"memepedia.ru", "joyreactor.cc", "reactor.cc",
			"lookatme.ru", "adme.ru", "fishki.net",
			"9gag.tv", "demilked.com", "thechive.com",
		},
	},
}

// All возвращает копию всех групп каталога.
func All() []Group {
	out := make([]Group, len(groups))
	for i, g := range groups {
		out[i] = g
		out[i].Domains = append([]string(nil), g.Domains...)
	}
	return out
}

// GroupsByID возвращает домены указанных групп, объединённые и нормализованные.
func GroupsByID(ids []string) []string {
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		want[id] = true
	}
	var out []string
	for _, g := range groups {
		if want[g.ID] {
			out = append(out, g.Domains...)
		}
	}
	return out
}

// DoH возвращает домены публичных DoH-резолверов.
func DoH() []string {
	return append([]string(nil), dohProviders...)
}

// Defaults — группы, включённые по умолчанию при первом запуске.
func Defaults() []string {
	return []string{"social", "video", "shorts", "doomscroll"}
}

// IDs возвращает идентификаторы всех групп в порядке объявления.
func IDs() []string {
	out := make([]string, 0, len(groups))
	for _, g := range groups {
		out = append(out, g.ID)
	}
	return out
}
