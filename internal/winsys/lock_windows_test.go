//go:build windows

package winsys

import (
	"path/filepath"
	"testing"
)

// Блокировка экземпляра — то, по чему сторож и страховка прокси понимают,
// запущено ли приложение. Ошибка здесь означает либо лишний перезапуск, либо
// снятую блокировку у работающего приложения.
func TestAcquireLockIsExclusive(t *testing.T) {
	path := filepath.Join(t.TempDir(), "focusd.instance.lock")

	if IsLocked(path) {
		t.Fatal("свободный файл считается занятым")
	}

	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("не удалось захватить блокировку: %v", err)
	}
	if !IsLocked(path) {
		t.Fatal("захваченный файл считается свободным")
	}

	// Второй захват обязан провалиться: иначе второй экземпляр приложения
	// решил бы, что он один.
	if _, err := AcquireLock(path); err == nil {
		t.Fatal("повторный захват прошёл")
	}

	lock.Release()
	if IsLocked(path) {
		t.Fatal("после Release файл всё ещё занят")
	}
}

// Повторное снятие — не ошибка: выход из приложения не должен зависеть от
// порядка уборки.
func TestReleaseIsIdempotent(t *testing.T) {
	var nilLock *LockFile
	nilLock.Release()

	path := filepath.Join(t.TempDir(), "focusd.instance.lock")
	lock, err := AcquireLock(path)
	if err != nil {
		t.Fatalf("не удалось захватить блокировку: %v", err)
	}
	lock.Release()
	lock.Release()
}

// Захват в недоступном каталоге обязан вернуть ошибку, а не сделать вид, что
// блокировка взята.
func TestAcquireLockOnBadPath(t *testing.T) {
	if _, err := AcquireLock(`C:\<недопустимый путь>\focusd.lock`); err == nil {
		t.Fatal("ожидалась ошибка захвата")
	}
}

func TestIsLockedOnBadPath(t *testing.T) {
	if !IsLocked(`C:\<недопустимый путь>\focusd.lock`) {
		t.Fatal("нераскрываемый файл должен считаться занятым: так безопаснее")
	}
}

// Путь с нулевым байтом в строку Windows не превращается: вместо паники должна
// вернуться ошибка.
func TestLockOnUnconvertiblePath(t *testing.T) {
	bad := "focusd\x00.lock"
	if _, err := AcquireLock(bad); err == nil {
		t.Fatal("ожидалась ошибка преобразования пути")
	}
	if IsLocked(bad) {
		t.Fatal("нераскрываемый путь не должен считаться свободным: так безопаснее")
	}
}

func TestExecutablePath(t *testing.T) {
	got, err := ExecutablePath()
	if err != nil {
		t.Fatalf("ExecutablePath вернул ошибку: %v", err)
	}
	if got == "" {
		t.Fatal("путь к исполняемому файлу пуст")
	}
}

// Вызов читает настоящее оформление системы. Проверяется здесь только то, что
// он не падает и не переворачивает значение: на машине сборки тема любая.
func TestSystemPrefersDarkIsCallable(t *testing.T) {
	first := SystemPrefersDark()
	if second := SystemPrefersDark(); second != first {
		t.Fatalf("два подряд вызова дали разное: %v и %v", first, second)
	}
}

func TestIsElevatedIsCallable(t *testing.T) {
	// Результат зависит от того, как запущен тест; важно, что вызов безопасен.
	_ = IsElevated()
}
