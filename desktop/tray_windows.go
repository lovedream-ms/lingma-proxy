package main

import (
	"context"
	_ "embed"
	goruntime "runtime"

	"github.com/getlantern/systray"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/windows/icon.ico
var trayIcon []byte

func registerDesktopTray(onReady func()) {
	go func() {
		goruntime.LockOSThread()
		defer goruntime.UnlockOSThread()
		systray.Run(onReady, nil)
	}()
}

func hideDesktopWindow(ctx context.Context) { runtime.WindowHide(ctx) }
