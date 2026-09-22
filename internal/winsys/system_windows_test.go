//go:build windows

package winsys

import (
	"errors"
	"strings"
	"testing"
)

const testExe = `C:\Program Files\Focusd\Focusd.exe`

// Автозапуск — задача планировщика с высшими правами: без неё при каждом входе
// появлялся бы запрос UAC. Проверяются и постановка, и снятие, и то, что путь с
// пробелами не разъезжается на два аргумента.
func TestSetAutostart(t *testing.T) {
	// Задачи нет: /query обязан ответить ошибкой, иначе удалять будет нечего.
	runs := withFakeRun(t, func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "/query" {
			return "", errors.New("задача не найдена")
		}
		return "", nil
	})

	if err := SetAutostart(nil, true, testExe); err != nil {
		t.Fatalf("SetAutostart(true): %v", err)
	}
	if !runs.called("/tn Focusd ") || !runs.called("/sc onlogon") || !runs.called("/rl highest") {
		t.Fatalf("задача автозапуска поставлена неверно: %v", runs.calls)
	}
	if !runs.called(`"C:\Program Files\Focusd\Focusd.exe"`) {
		t.Fatalf("путь с пробелами не закавычен: %v", runs.calls)
	}

	runs.calls = nil
	if err := SetAutostart(nil, false, testExe); err != nil {
		t.Fatalf("SetAutostart(false): %v", err)
	}
	// Задачи нет — удалять нечего, и ошибкой это не считается.
	if runs.called("/delete") {
		t.Fatalf("удаление вызвано для несуществующей задачи: %v", runs.calls)
	}
}

func TestSetAutostartReportsCreateError(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "", errors.New("schtasks отказал")
	})
	if err := SetAutostart(nil, true, testExe); err == nil {
		t.Fatal("ожидалась ошибка постановки задачи")
	}
}

func TestSetAutostartDeletesExistingTask(t *testing.T) {
	runs := withFakeRun(t, func(_ string, _ []string) (string, error) {
		// На /query задача находится, на /delete — тоже всё хорошо.
		return "", nil
	})
	if err := SetAutostart(nil, false, testExe); err != nil {
		t.Fatalf("SetAutostart(false): %v", err)
	}
	if !runs.called("/delete /tn Focusd /f") {
		t.Fatalf("существующая задача не удалена: %v", runs.calls)
	}
}

func TestDeleteTaskReportsDeleteError(t *testing.T) {
	withFakeRun(t, func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "/delete" {
			return "", errors.New("не удалось удалить")
		}
		return "", nil
	})
	if err := deleteTask(nil, guardTask); err == nil {
		t.Fatal("ожидалась ошибка удаления задачи")
	}
}

// Сторожевая задача — единственное, что мешает снять жёсткую сессию, закрыв
// приложение.
func TestSetGuard(t *testing.T) {
	runs := withFakeRun(t, func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "/query" {
			return "", errors.New("задача не найдена")
		}
		return "", nil
	})

	if err := SetGuard(nil, true, testExe); err != nil {
		t.Fatalf("SetGuard(true): %v", err)
	}
	if !runs.called("/tn FocusdGuard ") || !runs.called("/sc minute") || !runs.called("/mo 1") {
		t.Fatalf("задача сторожа поставлена неверно: %v", runs.calls)
	}
	if !runs.called("--guard") {
		t.Fatalf("сторож поставлен без своего режима: %v", runs.calls)
	}

	runs.calls = nil
	if err := SetGuard(nil, false, testExe); err != nil {
		t.Fatalf("SetGuard(false): %v", err)
	}
	if runs.called("/create") {
		t.Fatalf("при снятии задача не должна ставиться: %v", runs.calls)
	}
}

func TestSetGuardReportsError(t *testing.T) {
	withFakeRun(t, func(string, []string) (string, error) {
		return "", errors.New("schtasks отказал")
	})
	if err := SetGuard(nil, true, testExe); err == nil {
		t.Fatal("ожидалась ошибка постановки задачи")
	}
}

// Страховка прокси — две задачи, а не одна: расписание у планировщика задаётся
// одним ключом за вызов. Пропущенная задача означает либо непоправленную
// перезагрузку, либо приложение, убитое без перезагрузки.
func TestSetProxyGuard(t *testing.T) {
	runs := withFakeRun(t, func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "/query" {
			return "", errors.New("задача не найдена")
		}
		return "", nil
	})

	if err := SetProxyGuard(nil, true, testExe); err != nil {
		t.Fatalf("SetProxyGuard(true): %v", err)
	}
	if !runs.called("/tn FocusdProxyGuard ") || !runs.called("/tn FocusdProxyGuardMinute ") {
		t.Fatalf("поставлены не обе задачи: %v", runs.calls)
	}
	if !runs.called("/sc onlogon") || !runs.called("/sc minute") {
		t.Fatalf("расписания задач неверны: %v", runs.calls)
	}
	if !runs.called("--proxyguard") {
		t.Fatalf("задачи поставлены без режима страховки: %v", runs.calls)
	}

	runs.calls = nil
	if err := SetProxyGuard(nil, false, testExe); err != nil {
		t.Fatalf("SetProxyGuard(false): %v", err)
	}
	if runs.called("/create") {
		t.Fatalf("при снятии задачи не должны ставиться: %v", runs.calls)
	}
}

// Ошибка постановки одной задачи не должна скрывать вторую: обе части
// страховки нужно попробовать поставить.
func TestSetProxyGuardReportsBothErrors(t *testing.T) {
	withFakeRun(t, func(_ string, args []string) (string, error) {
		if len(args) > 1 && args[0] == "/create" {
			// Имя задачи стоит в аргументах сразу после /tn.
			for i, a := range args {
				if a == "/tn" && i+1 < len(args) {
					return "", errors.New("отказано: " + args[i+1])
				}
			}
			return "", errors.New("отказано: неизвестная задача")
		}
		return "", nil
	})
	err := SetProxyGuard(nil, true, testExe)
	if err == nil {
		t.Fatal("ожидалась ошибка постановки задач")
	}
	if !strings.Contains(err.Error(), proxyGuardTask) || !strings.Contains(err.Error(), proxyGuardMinTask) {
		t.Fatalf("потеряна одна из ошибок: %v", err)
	}
}

func TestGuardTaskInstalled(t *testing.T) {
	withFakeRun(t, func(_ string, args []string) (string, error) {
		if len(args) > 0 && args[0] == "/query" {
			return "", nil
		}
		return "", errors.New("нет задачи")
	})
	if !GuardTaskInstalled(nil) {
		t.Fatal("найденная задача должна считаться поставленной")
	}

	withFakeRun(t, func(string, []string) (string, error) {
		return "", errors.New("нет задачи")
	})
	if GuardTaskInstalled(nil) {
		t.Fatal("отсутствующая задача не должна считаться поставленной")
	}
}

// quoteTaskArg закавычивает только путь: остальные части команды schtasks
// разбирает как обычные аргументы.
func TestQuoteTaskArg(t *testing.T) {
	if got := quoteTaskArg(testExe); got != `"C:\Program Files\Focusd\Focusd.exe"` {
		t.Fatalf("quoteTaskArg = %q", got)
	}
	if got := quoteTaskArg(testExe, "--guard"); got != `"C:\Program Files\Focusd\Focusd.exe" --guard` {
		t.Fatalf("quoteTaskArg с аргументом = %q", got)
	}
}
