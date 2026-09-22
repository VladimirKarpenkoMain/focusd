//go:build windows

package winsys

import (
	"golang.org/x/sys/windows"
)

// IsElevated сообщает, запущен ли процесс с правами администратора.
func IsElevated() bool {
	return windows.GetCurrentProcessToken().IsElevated()
}
