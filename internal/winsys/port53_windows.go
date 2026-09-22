//go:build windows

package winsys

import "strings"

// Port53Owner описывает, кто слушает 53-й порт на всех интерфейсах.
type Port53Owner struct {
	Process string // имя исполняемого файла, например "svchost.exe"
	PID     string
	Service string // служба Windows, которую хостит процесс, например "SharedAccess"
}

// service53Human переводит внутренние имена служб в понятные человеку.
var service53Human = map[string]string{
	"SharedAccess": "общий доступ к подключению к Интернету (ICS)",
	"hns":          "сеть Hyper-V (HNS)",
	"vmcompute":    "вычисления Hyper-V",
	"dnscache":     "DNS-клиент Windows",
	"iphlpsvc":     "вспомогательный модуль IP",
	"RemoteAccess": "маршрутизация и удалённый доступ",
}

// Describe возвращает человекочитаемое описание владельца порта.
func (o Port53Owner) Describe() string {
	if o.Process == "" {
		if o.PID == "" {
			return "неизвестный процесс"
		}
		return "неизвестный процесс (PID " + o.PID + ")"
	}
	human, known := service53Human[o.Service]
	switch {
	case known:
		return o.Process + ", " + human + " (PID " + o.PID + ")"
	case o.Service != "":
		return o.Process + ", служба " + o.Service + " (PID " + o.PID + ")"
	default:
		return o.Process + " (PID " + o.PID + ")"
	}
}

// Hint объясняет, что делать с владельцем порта. Пустая строка означает, что
// конкретный совет дать нечем.
//
// Останавливать службу приходится человеку: это заметное вмешательство в
// систему (сеть Hyper-V, мобильный хот-спот), и делать его молча приложение не
// вправе.
func (o Port53Owner) Hint() string {
	const admin = "в PowerShell от администратора: "
	switch o.Service {
	case "SharedAccess":
		return "Эту службу поднимают мобильный хот-спот и сеть Hyper-V по умолчанию — " +
			"её используют WSL2 и Docker Desktop. Остановить " + admin + "Stop-Service SharedAccess"
	case "hns", "vmcompute":
		return "Это сеть Hyper-V, её используют WSL2 и Docker Desktop. Остановить " +
			admin + "Stop-Service hns"
	case "":
		return ""
	default:
		return "Остановить " + admin + "Stop-Service " + o.Service
	}
}

// WhoOwnsPort53 ищет, кто слушает 53-й порт на всех адресах.
//
// Раньше здесь была быстрая проба «удастся ли занять 0.0.0.0:53». Она врёт ровно
// в самом частом случае: Windows разрешает вторую UDP-привязку к тому же адресу,
// поэтому на машине, где порт уже держит служба ICS, проба проходит, конфликт
// остаётся незамеченным — и человек видит невнятную ошибку сокета вместо
// объяснения. Источник истины — таблица сокетов.
//
// Focusd занимает конкретные 127.0.0.1:53 и [::1]:53, поэтому слушатель на
// 0.0.0.0 — всегда кто-то чужой.
func WhoOwnsPort53(ctx contextT) (Port53Owner, bool) {
	out, err := runExternal(ctx, "netstat", "-ano", "-p", "UDP")
	if err != nil {
		return Port53Owner{}, false
	}
	for _, line := range strings.Split(out, "\n") {
		fields := strings.Fields(line)
		// Формат строки: Proto  Local Address  Foreign Address  PID
		if len(fields) < 4 {
			continue
		}
		if fields[1] != "0.0.0.0:53" && fields[1] != "[::]:53" {
			continue
		}
		return describeOwner(ctx, fields[len(fields)-1]), true
	}
	return Port53Owner{}, false
}

func describeOwner(ctx contextT, pid string) Port53Owner {
	owner := Port53Owner{PID: pid}
	// /SVC добавляет третью колонку — имя службы, которую хостит процесс.
	list, err := runExternal(ctx, "tasklist", "/FI", "PID eq "+pid, "/FO", "CSV", "/NH", "/SVC")
	if err != nil {
		return owner
	}
	fields := csvFields(list)
	if len(fields) > 0 {
		owner.Process = fields[0]
	}
	if len(fields) > 2 {
		owner.Service = fields[2]
	}
	return owner
}

// csvFields разбирает строку вида "svchost.exe","4008","SharedAccess".
// Строки-приглашения tasklist на разных языках выглядят по-разному, поэтому
// вместо текста проверяем, что строка начинается с кавычки.
func csvFields(line string) []string {
	line = strings.TrimSpace(line)
	if !strings.HasPrefix(line, `"`) {
		return nil
	}
	parts := strings.Split(line, `","`)
	for i := range parts {
		parts[i] = strings.Trim(parts[i], `"`)
	}
	return parts
}
