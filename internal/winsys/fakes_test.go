//go:build windows

package winsys

import (
	"context"
	"sort"
	"strconv"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// fakes_test.go держит подмены, общие для тестов пакета: реестр в памяти и
// внешние команды. Настоящий реестр политик требует прав администратора, а
// команды netstat, tasklist и schtasks на машине сборки отвечают
// непредсказуемо — ни то, ни другое не годится для проверки решений, которые
// принимаются по их ответу.

// fakeReg — реестр в памяти. Ошибки включаются полем, а не окружением: ветки
// «не удалось записать» и «не удалось удалить» иначе не воспроизвести.
type fakeReg struct {
	values map[string]map[string]string

	createErr    error
	openErr      error
	deleteErr    error
	writeErr     error
	setDwordErr  error
	setStringErr error
	deleteValErr error
	readErr      error

	created []string
	deleted []string
}

func newFakeReg() *fakeReg {
	return &fakeReg{values: make(map[string]map[string]string)}
}

func (f *fakeReg) create(_ registry.Key, path string, _ uint32) (regKey, bool, error) {
	f.created = append(f.created, path)
	if f.createErr != nil {
		return nil, false, f.createErr
	}
	_, existed := f.values[path]
	if !existed {
		f.values[path] = make(map[string]string)
	}
	return &fakeKey{reg: f, path: path}, existed, nil
}

func (f *fakeReg) open(_ registry.Key, path string, _ uint32) (regKey, error) {
	if f.openErr != nil {
		return nil, f.openErr
	}
	if _, ok := f.values[path]; !ok {
		return nil, registry.ErrNotExist
	}
	return &fakeKey{reg: f, path: path}, nil
}

func (f *fakeReg) del(_ registry.Key, path string) error {
	f.deleted = append(f.deleted, path)
	if f.deleteErr != nil {
		return f.deleteErr
	}
	if _, ok := f.values[path]; !ok {
		return registry.ErrNotExist
	}
	delete(f.values, path)
	return nil
}

// value возвращает записанное значение — так проверяется, что политика ушла
// именно туда и именно с тем содержимым.
func (f *fakeReg) value(path, name string) string {
	return f.values[path][name]
}

type fakeKey struct {
	reg  *fakeReg
	path string
}

func (k *fakeKey) SetDWordValue(name string, value uint32) error {
	if err := k.reg.setDwordErr; err != nil {
		return err
	}
	if err := k.reg.writeErr; err != nil {
		return err
	}
	k.reg.values[k.path][name] = strconv.FormatUint(uint64(value), 10)
	return nil
}

func (k *fakeKey) SetStringValue(name, value string) error {
	if err := k.reg.setStringErr; err != nil {
		return err
	}
	if err := k.reg.writeErr; err != nil {
		return err
	}
	k.reg.values[k.path][name] = value
	return nil
}

func (k *fakeKey) DeleteValue(name string) error {
	if k.reg.deleteValErr != nil {
		return k.reg.deleteValErr
	}
	delete(k.reg.values[k.path], name)
	return nil
}

func (k *fakeKey) GetStringValue(name string) (string, uint32, error) {
	v, ok := k.reg.values[k.path][name]
	if !ok {
		return "", 0, registry.ErrNotExist
	}
	return v, 1, nil
}

func (k *fakeKey) GetIntegerValue(name string) (uint64, uint32, error) {
	v, ok := k.reg.values[k.path][name]
	if !ok {
		return 0, 0, registry.ErrNotExist
	}
	n, err := strconv.ParseUint(v, 10, 32)
	return n, 4, err
}

func (k *fakeKey) ReadValueNames(int) ([]string, error) {
	if k.reg.readErr != nil {
		return nil, k.reg.readErr
	}
	names := make([]string, 0, len(k.reg.values[k.path]))
	for n := range k.reg.values[k.path] {
		names = append(names, n)
	}
	sort.Strings(names)
	return names, nil
}

func (k *fakeKey) Close() error { return nil }

// withFakeRegistry подменяет обращения к реестру на время теста.
func withFakeRegistry(t *testing.T) *fakeReg {
	t.Helper()
	f := newFakeReg()
	savedCreate, savedOpen, savedDelete := createRegKey, openRegKey, deleteRegKey
	createRegKey = f.create
	openRegKey = f.open
	deleteRegKey = f.del
	t.Cleanup(func() {
		createRegKey, openRegKey, deleteRegKey = savedCreate, savedOpen, savedDelete
	})
	return f
}

// fakeRun — подмена внешней команды с записью вызовов.
type fakeRun struct {
	calls []string
	reply func(name string, args []string) (string, error)
}

func (f *fakeRun) run(_ context.Context, name string, args ...string) (string, error) {
	f.calls = append(f.calls, strings.Join(append([]string{name}, args...), " "))
	if f.reply == nil {
		return "", nil
	}
	return f.reply(name, args)
}

// withFakeRun подменяет внешние команды на время теста.
func withFakeRun(t *testing.T, reply func(name string, args []string) (string, error)) *fakeRun {
	t.Helper()
	f := &fakeRun{reply: reply}
	saved := runExternal
	runExternal = f.run
	t.Cleanup(func() { runExternal = saved })
	return f
}

// called сообщает, вызывалась ли команда с такими аргументами.
func (f *fakeRun) called(substr string) bool {
	for _, c := range f.calls {
		if strings.Contains(c, substr) {
			return true
		}
	}
	return false
}
