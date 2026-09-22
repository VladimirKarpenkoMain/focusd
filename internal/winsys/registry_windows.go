//go:build windows

package winsys

import "golang.org/x/sys/windows/registry"

// regKey — то немногое, что Focusd делает с ключом реестра.
//
// Интерфейс заведён ради тестов. Ветки «не удалось записать», «не удалось
// удалить» и «не удалось прочитать» на живой системе либо требуют прав
// администратора, либо не наступают вовсе, — а знать, что при них происходит,
// нужно: от этого зависит, останется ли браузер без политики. Настоящий
// registry.Key этому интерфейсу удовлетворяет как есть, поэтому в бою поведение
// не меняется ни на байт.
type regKey interface {
	SetDWordValue(name string, value uint32) error
	SetStringValue(name, value string) error
	DeleteValue(name string) error
	GetStringValue(name string) (string, uint32, error)
	GetIntegerValue(name string) (uint64, uint32, error)
	ReadValueNames(n int) ([]string, error)
	Close() error
}

// Точки подмены. В бою здесь всегда функции golang.org/x/sys/windows/registry.
var (
	createRegKey = func(hive registry.Key, path string, access uint32) (regKey, bool, error) {
		k, existing, err := registry.CreateKey(hive, path, access)
		if err != nil {
			return nil, existing, err
		}
		return k, existing, nil
	}
	openRegKey = func(hive registry.Key, path string, access uint32) (regKey, error) {
		k, err := registry.OpenKey(hive, path, access)
		if err != nil {
			return nil, err
		}
		return k, nil
	}
	deleteRegKey = registry.DeleteKey
)
