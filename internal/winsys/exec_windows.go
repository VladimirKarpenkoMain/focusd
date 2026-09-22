//go:build windows

// Package winsys — тонкий слой над системными возможностями Windows:
// переключение DNS адаптеров, отключение DoH в браузерах, автозапуск.
package winsys

import (
	"bytes"
	"context"
	"fmt"
	"os/exec"
	"strings"
	"syscall"
	"time"
)

// contextT — короткий алиас, чтобы не повторять context.Context в сигнатурах.
type contextT = context.Context

// createNoWindow — флаг CREATE_NO_WINDOW: не мигать консолью при вызове netsh.
const createNoWindow = 0x08000000

// Run выполняет внешнюю команду, не показывая окно, и возвращает stdout+stderr.
func Run(ctx context.Context, name string, args ...string) (string, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()

	cmd := exec.CommandContext(ctx, name, args...)
	cmd.SysProcAttr = &syscall.SysProcAttr{HideWindow: true, CreationFlags: createNoWindow}

	var buf bytes.Buffer
	cmd.Stdout = &buf
	cmd.Stderr = &buf

	err := cmd.Run()
	out := strings.TrimSpace(buf.String())
	if err != nil {
		if out != "" {
			return out, fmt.Errorf("%s %s: %w (%s)", name, strings.Join(args, " "), err, out)
		}
		return out, fmt.Errorf("%s %s: %w", name, strings.Join(args, " "), err)
	}
	return out, nil
}

// runExternal — точка подмены для тестов: netstat, tasklist и schtasks на
// машине сборки отвечают непредсказуемо, а разбор их вывода и решения,
// принимаемые по нему, проверить нужно. В бою здесь всегда Run.
var runExternal = Run

// RunAll выполняет команды по очереди, останавливаясь на первой ошибке.
func RunAll(ctx context.Context, name string, argSets [][]string) error {
	for _, args := range argSets {
		if _, err := runExternal(ctx, name, args...); err != nil {
			return err
		}
	}
	return nil
}
