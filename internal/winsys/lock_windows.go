//go:build windows

package winsys

import (
	"golang.org/x/sys/windows"
)

// LockFile — эксклюзивная блокировка файла, живущая пока процесс не завершится.
// Операционная система снимает её автоматически даже при аварийном падении.
type LockFile struct {
	handle windows.Handle
}

// AcquireLock пытается захватить файл. Если его уже держит другой процесс,
// возвращается ошибка разделения доступа.
func AcquireLock(path string) (*LockFile, error) {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return nil, err
	}
	// Нулевой режим совместного доступа: никто другой файл не откроет.
	h, err := windows.CreateFile(name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return nil, err
	}
	return &LockFile{handle: h}, nil
}

// Release снимает блокировку.
func (l *LockFile) Release() {
	if l == nil || l.handle == 0 {
		return
	}
	_ = windows.CloseHandle(l.handle)
	l.handle = 0
}

// IsLocked сообщает, удерживает ли файл другой процесс.
func IsLocked(path string) bool {
	name, err := windows.UTF16PtrFromString(path)
	if err != nil {
		return false
	}
	h, err := windows.CreateFile(name,
		windows.GENERIC_READ|windows.GENERIC_WRITE,
		0,
		nil,
		windows.OPEN_ALWAYS,
		windows.FILE_ATTRIBUTE_NORMAL,
		0,
	)
	if err != nil {
		return true // открыть не дали — файл кем-то удерживается
	}
	_ = windows.CloseHandle(h)
	return false
}
