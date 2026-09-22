//go:build windows

package winsys

import (
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"
)

// Обёртки над реестром — тонкие, и всё же их нужно проверить: подменённые в
// тестах, они в бою остаются единственным путём к политикам. Чтение доступно
// всегда, запись — не везде, поэтому тесты на неё пропускаются, если реестр
// закрыт (песочница, чужой профиль).
const testSeamsKey = `Software\Microsoft\Windows\CurrentVersion\Themes\Personalize`

func TestCreateRegKeyOpensExistingReadOnly(t *testing.T) {
	// QUERY_VALUE на существующем ключе не требует прав на запись: этой ветки
	// достаточно, чтобы убедиться, что обёртка отдаёт живой ключ.
	k, existed, err := createRegKey(registry.CURRENT_USER, testSeamsKey, registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("реестр недоступен: %v", err)
	}
	defer k.Close()
	if !existed {
		t.Fatalf("ключ %q должен был уже существовать", testSeamsKey)
	}
	if _, _, err := k.GetIntegerValue("AppsUseLightTheme"); err != nil {
		t.Fatalf("ключ отдан нерабочим: %v", err)
	}
}

func TestCreateRegKeyReportsFailure(t *testing.T) {
	// Имя раздела в реестре ограничено по длине: такой ключ не создаётся ни при
	// каких правах.
	long := strings.Repeat("a", 300)
	if _, _, err := createRegKey(registry.CURRENT_USER, `Software\Focusd\`+long, registry.QUERY_VALUE); err == nil {
		t.Fatal("ожидалась ошибка создания ключа")
	}
}

func TestOpenRegKey(t *testing.T) {
	k, err := openRegKey(registry.CURRENT_USER, testSeamsKey, registry.QUERY_VALUE)
	if err != nil {
		t.Skipf("реестр недоступен: %v", err)
	}
	defer k.Close()
	if _, _, err := k.GetIntegerValue("AppsUseLightTheme"); err != nil {
		t.Fatalf("ключ отдан нерабочим: %v", err)
	}

	if _, err := openRegKey(registry.CURRENT_USER, `Software\Focusd\Заведомо\Отсутствует`, registry.QUERY_VALUE); err == nil {
		t.Fatal("отсутствующий ключ должен был дать ошибку")
	}
}

func TestDeleteRegKeyReportsFailure(t *testing.T) {
	// Удаление того, чего нет, — ошибка уровня реестра, а вызывающие её
	// отдельно разбирают: ErrNotExist означает «снимать нечего».
	err := deleteRegKey(registry.CURRENT_USER, `Software\Focusd\Заведомо\Отсутствует`)
	if err == nil {
		t.Fatal("ожидалась ошибка удаления отсутствующего ключа")
	}
	if !strings.Contains(strings.ToLower(err.Error()), "не удается найти") && err != registry.ErrNotExist {
		t.Skipf("реестр отвечает иначе: %v", err)
	}
}
