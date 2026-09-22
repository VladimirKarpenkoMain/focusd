//go:build windows

package winsys

import (
	"errors"
	"strings"
	"testing"
)

func TestWhoOwnsPort53(t *testing.T) {
	runs := withFakeRun(t, func(name string, args []string) (string, error) {
		switch name {
		case "netstat":
			return "Активные подключения\r\n" +
				"  Протокол  Локальный адрес        Внешний адрес          PID\r\n" +
				"  UDP       0.0.0.0:5353           *:*                    1234\r\n" +
				"  UDP       0.0.0.0:53             *:*                    4008\r\n" +
				"  UDP       [::]:53                *:*                    4008\r\n", nil
		case "tasklist":
			return `"svchost.exe","4008","SharedAccess"`, nil
		}
		return "", errors.New("неожиданная команда " + name)
	})

	owner, ok := WhoOwnsPort53(nil)
	if !ok {
		t.Fatal("владелец порта 53 не найден")
	}
	if owner.Process != "svchost.exe" || owner.PID != "4008" || owner.Service != "SharedAccess" {
		t.Fatalf("владелец разобран неверно: %+v", owner)
	}
	// Строка с портом 5353 не должна приниматься за 53-й: иначе Focusd снял бы
	// чужую службу.
	if !runs.called("netstat -ano -p UDP") {
		t.Fatalf("netstat не вызывался: %v", runs.calls)
	}
	if !runs.called("PID eq 4008") || !runs.called("/SVC") {
		t.Fatalf("служба процесса не запрошена: %v", runs.calls)
	}
}

func TestWhoOwnsPort53IPv6Only(t *testing.T) {
	withFakeRun(t, func(name string, _ []string) (string, error) {
		if name == "netstat" {
			return "  UDP       [::]:53                *:*                    777\r\n", nil
		}
		return `"svchost.exe","777","hns"`, nil
	})

	owner, ok := WhoOwnsPort53(nil)
	if !ok || owner.Service != "hns" {
		t.Fatalf("владелец по IPv6 разобран неверно: %+v (найден: %v)", owner, ok)
	}
}

func TestWhoOwnsPort53Free(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "  UDP       0.0.0.0:5353           *:*                    1234\r\n" +
			"короткая строка\r\n", nil
	})
	if owner, ok := WhoOwnsPort53(nil); ok {
		t.Fatalf("порт 53 свободен, но найден владелец: %+v", owner)
	}
}

func TestWhoOwnsPort53OnNetstatFailure(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "", errors.New("netstat недоступен")
	})
	if _, ok := WhoOwnsPort53(nil); ok {
		t.Fatal("при сбое netstat владельца быть не должно")
	}
}

// Процесс опознан, а служба — нет: описание всё равно должно быть.
func TestDescribeOwnerWithoutTasklist(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "", errors.New("tasklist недоступен")
	})
	owner := describeOwner(nil, "4008")
	if owner.PID != "4008" {
		t.Fatalf("PID потерян: %+v", owner)
	}
	if owner.Process != "" || owner.Service != "" {
		t.Fatalf("при сбое tasklist ничего лишнего быть не должно: %+v", owner)
	}
	if got := owner.Describe(); !strings.Contains(got, "4008") {
		t.Fatalf("описание потеряло PID: %q", got)
	}
}

// Ответ без колонки службы: имя процесса всё равно нужно.
func TestDescribeOwnerWithoutServiceColumn(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return `"svchost.exe","4008"`, nil
	})
	owner := describeOwner(nil, "4008")
	if owner.Process != "svchost.exe" || owner.Service != "" {
		t.Fatalf("владелец разобран неверно: %+v", owner)
	}
}

// Приглашение tasklist на разных языках выглядит по-разному, поэтому строку без
// кавычек в начале разбирать нельзя — иначе в имя процесса попал бы текст.
func TestDescribeOwnerIgnoresPrompt(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "ИНФОРМАЦИЯ: нет задач.", nil
	})
	if owner := describeOwner(nil, "4008"); owner.Process != "" {
		t.Fatalf("приглашение принято за имя процесса: %+v", owner)
	}
}

func TestCSVFields(t *testing.T) {
	got := csvFields(`  "svchost.exe","4008","SharedAccess"  `)
	want := []string{"svchost.exe", "4008", "SharedAccess"}
	if len(got) != len(want) {
		t.Fatalf("csvFields = %v, ожидалось %v", got, want)
	}
	for i := range want {
		if got[i] != want[i] {
			t.Fatalf("csvFields = %v, ожидалось %v", got, want)
		}
	}

	if got := csvFields(""); got != nil {
		t.Fatalf("пустая строка должна давать nil, получено %v", got)
	}
	if got := csvFields(`без кавычек`); got != nil {
		t.Fatalf("строка без кавычек должна давать nil, получено %v", got)
	}
}

func TestPort53OwnerDescribe(t *testing.T) {
	cases := []struct {
		owner Port53Owner
		want  string
	}{
		{Port53Owner{}, "неизвестный процесс"},
		{Port53Owner{PID: "4008"}, "неизвестный процесс (PID 4008)"},
		{Port53Owner{Process: "svchost.exe", PID: "4008", Service: "SharedAccess"},
			"svchost.exe, общий доступ к подключению к Интернету (ICS) (PID 4008)"},
		{Port53Owner{Process: "svchost.exe", PID: "4008", Service: "чужая"},
			"svchost.exe, служба чужая (PID 4008)"},
		{Port53Owner{Process: "dns.exe", PID: "999"}, "dns.exe (PID 999)"},
	}
	for _, c := range cases {
		if got := c.owner.Describe(); got != c.want {
			t.Errorf("Describe(%+v) = %q, ожидалось %q", c.owner, got, c.want)
		}
	}
}

func TestPort53OwnerHint(t *testing.T) {
	if got := (Port53Owner{}).Hint(); got != "" {
		t.Errorf("без службы совета быть не должно, получено %q", got)
	}
	shared := Port53Owner{Service: "SharedAccess"}.Hint()
	if !strings.Contains(shared, "Stop-Service SharedAccess") || !strings.Contains(shared, "хот-спот") {
		t.Errorf("совет про ICS непонятен: %q", shared)
	}
	for _, svc := range []string{"hns", "vmcompute"} {
		got := Port53Owner{Service: svc}.Hint()
		if !strings.Contains(got, "Stop-Service hns") || !strings.Contains(got, "Hyper-V") {
			t.Errorf("совет про %s непонятен: %q", svc, got)
		}
	}
	other := Port53Owner{Service: "чужая-служба"}.Hint()
	if !strings.Contains(other, "Stop-Service чужая-служба") {
		t.Errorf("совет про незнакомую службу непонятен: %q", other)
	}
}
