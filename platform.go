package main

import (
	"context"
	"os/exec"

	"github.com/wailsapp/wails/v2"
	"github.com/wailsapp/wails/v2/pkg/runtime"

	"focusd/internal/winsys"
)

// Всё, что приложение просит у внешнего мира — Windows и Wails, — собрано в
// этом файле в две переменные. Так сделано ради тестов: настоящие политики
// лежат в HKLM и требуют прав администратора, задачи планировщика создаются
// по-настоящему, а runtime.* на пустом контексте паникует. Проверять при этом
// нужно не их, а решения приложения: когда снять политику, что записать в
// реестр, когда показать окно. В бою обе переменные собраны из настоящих
// вызовов и не меняются.

// systemAPI — системные возможности Windows, которыми пользуется приложение.
type systemAPI struct {
	IsElevated             func() bool
	SystemPrefersDark      func() bool
	SystemLanguage         func() string
	RunningBrowsers        func(context.Context) []string
	SitesBlockedInBrowsers func() bool
	BlockSitesInBrowsers   func(domains, allow []string) error
	UnblockSitesInBrowsers func() error
	SetBrowserProxy        func(addr string) error
	ClearBrowserProxy      func() error
	BrowserProxyAddr       func() string
	SetSystemProxy         func(addr string) (winsys.SystemProxySettings, error)
	RestoreSystemProxy     func(prev winsys.SystemProxySettings) error
	SystemProxyAddr        func() string
	DisableSystemProxy     func() error
	ExecutablePath         func() (string, error)
	SetGuard               func(ctx context.Context, enable bool, exe string) error
	SetProxyGuard          func(ctx context.Context, enable bool, exe string) error
	SetAutostart           func(ctx context.Context, enable bool, exe string) error
	AcquireLock            func(path string) (*winsys.LockFile, error)
	IsLocked               func(path string) bool
}

// sys — указатель, а не значение: тесты подменяют отдельные возможности уже
// после сборки приложения, и подмена должна быть видна всем, кто держит ссылку.
var sys = &systemAPI{
	IsElevated:             winsys.IsElevated,
	SystemPrefersDark:      winsys.SystemPrefersDark,
	SystemLanguage:         winsys.SystemLanguage,
	RunningBrowsers:        winsys.RunningBrowsers,
	SitesBlockedInBrowsers: winsys.SitesBlockedInBrowsers,
	BlockSitesInBrowsers:   winsys.BlockSitesInBrowsers,
	UnblockSitesInBrowsers: winsys.UnblockSitesInBrowsers,
	SetBrowserProxy:        winsys.SetBrowserProxy,
	ClearBrowserProxy:      winsys.ClearBrowserProxy,
	BrowserProxyAddr:       winsys.BrowserProxyAddr,
	SetSystemProxy:         winsys.SetSystemProxy,
	RestoreSystemProxy:     winsys.RestoreSystemProxy,
	SystemProxyAddr:        winsys.SystemProxyAddr,
	DisableSystemProxy:     winsys.DisableSystemProxy,
	ExecutablePath:         winsys.ExecutablePath,
	SetGuard:               winsys.SetGuard,
	SetProxyGuard:          winsys.SetProxyGuard,
	SetAutostart:           winsys.SetAutostart,
	AcquireLock:            winsys.AcquireLock,
	IsLocked:               winsys.IsLocked,
}

// wailsRuntime — окно и события Wails. Именованный тип, чтобы в тестах можно
// было собрать такую же структуру с подставными вызовами.
type wailsRuntime struct {
	EventsEmit                func(ctx context.Context, name string, data ...any)
	ScreenGetAll              func(ctx context.Context) ([]runtime.Screen, error)
	WindowCenter              func(ctx context.Context)
	WindowGetPosition         func(ctx context.Context) (int, int)
	WindowSetAlwaysOnTop      func(ctx context.Context, onTop bool)
	WindowSetBackgroundColour func(ctx context.Context, r, g, b, a uint8)
	WindowSetMinSize          func(ctx context.Context, width, height int)
	WindowSetPosition         func(ctx context.Context, x, y int)
	WindowSetSize             func(ctx context.Context, width, height int)
	WindowShow                func(ctx context.Context)
	WindowUnminimise          func(ctx context.Context)
}

var appRuntime = wailsRuntime{
	EventsEmit:                runtime.EventsEmit,
	ScreenGetAll:              runtime.ScreenGetAll,
	WindowCenter:              runtime.WindowCenter,
	WindowGetPosition:         runtime.WindowGetPosition,
	WindowSetAlwaysOnTop:      runtime.WindowSetAlwaysOnTop,
	WindowSetBackgroundColour: runtime.WindowSetBackgroundColour,
	WindowSetMinSize:          runtime.WindowSetMinSize,
	WindowSetPosition:         runtime.WindowSetPosition,
	WindowSetSize:             runtime.WindowSetSize,
	WindowShow:                runtime.WindowShow,
	WindowUnminimise:          runtime.WindowUnminimise,
}

// runWails поднимает окно. Отдельная переменная: поднять настоящее окно в
// тесте нельзя, а проверить, что приложение вообще до него доходит, нужно.
var runWails = wails.Run

// execCommand — запуск внешних программ.
var execCommand = exec.Command

// proxyTestURL — адрес для проверки прокси на живом запросе. Обычный сайт,
// который есть в любой сети и не входит ни в одну группу блокировки.
//
// Переменная, а не константа: тесты подставляют сюда свой локальный сервер,
// чтобы проверка не зависела от интернета.
var proxyTestURL = "http://example.com/"
