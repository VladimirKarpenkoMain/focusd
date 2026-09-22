//go:build windows

package winsys

import (
	"context"
	"strings"
	"testing"
	"time"
)

// Run запускает настоящую программу: подменять здесь нечего — проверяется сам
// вызов. cmd.exe есть на любой Windows, а вывод нужен, чтобы отделить сбой с
// объяснением от сбоя молча.
func TestRunCapturesOutput(t *testing.T) {
	out, err := Run(context.Background(), "cmd", "/c", "echo", "hello-focusd")
	if err != nil {
		t.Fatalf("Run вернул ошибку: %v", err)
	}
	if !strings.Contains(out, "hello-focusd") {
		t.Fatalf("вывод не пойман: %q", out)
	}
}

// Без контекста вызов обязан работать: nil приходит оттуда, где контекста нет.
func TestRunWithoutContext(t *testing.T) {
	out, err := Run(nil, "cmd", "/c", "echo", "ok-focusd")
	if err != nil {
		t.Fatalf("Run(nil, ...) вернул ошибку: %v", err)
	}
	if !strings.Contains(out, "ok-focusd") {
		t.Fatalf("вывод не пойман: %q", out)
	}
}

// Сбой с выводом: текст команды обязан попасть в ошибку — иначе разбирать
// причину будет нечем.
func TestRunReportsFailureWithOutput(t *testing.T) {
	out, err := Run(context.Background(), "cmd", "/c", "echo detail 1>&2 & exit 3")
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if !strings.Contains(out, "detail") {
		t.Fatalf("вывод команды потерян: %q", out)
	}
	if !strings.Contains(err.Error(), "detail") {
		t.Fatalf("ошибка не содержит вывод команды: %v", err)
	}
}

// Сбой молча: ошибка всё равно должна называть команду.
func TestRunReportsFailureWithoutOutput(t *testing.T) {
	out, err := Run(context.Background(), "cmd", "/c", "exit 4")
	if err == nil {
		t.Fatal("ожидалась ошибка")
	}
	if out != "" {
		t.Fatalf("вывода быть не должно, получено %q", out)
	}
	if !strings.Contains(err.Error(), "exit 4") {
		t.Fatalf("ошибка не называет команду: %v", err)
	}
}

// Несуществующая программа — тоже ошибка, а не паника.
func TestRunReportsMissingProgram(t *testing.T) {
	if _, err := Run(context.Background(), "нет-такой-программы-focusd", "аргумент"); err == nil {
		t.Fatal("отсутствующая программа должна дать ошибку")
	}
}

func TestRunAllStopsAtFirstFailure(t *testing.T) {
	if err := RunAll(context.Background(), "cmd", [][]string{
		{"/c", "echo", "первая"},
		{"/c", "exit", "1"},
		{"/c", "echo", "третья"},
	}); err == nil {
		t.Fatal("ожидалась ошибка второй команды")
	}
}

func TestRunAllSucceeds(t *testing.T) {
	if err := RunAll(context.Background(), "cmd", [][]string{
		{"/c", "echo", "раз"},
		{"/c", "echo", "два"},
	}); err != nil {
		t.Fatalf("RunAll вернул ошибку: %v", err)
	}
	// Пустой список команд — не ошибка.
	if err := RunAll(context.Background(), "cmd", nil); err != nil {
		t.Fatalf("пустой список команд: %v", err)
	}
}

// Таймаут: команда, которая не успевает, обязана быть остановлена, а не висеть
// вечно на закрытии приложения.
func TestRunHonoursContextCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Run(ctx, "cmd", "/c", "echo", "поздно"); err == nil {
		t.Fatal("отменённый контекст должен прервать команду")
	}
	_ = time.Second
}
