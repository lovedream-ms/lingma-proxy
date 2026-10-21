package main

import (
	"context"
	_ "embed"
	"time"

	"github.com/getlantern/systray"
	"github.com/godbus/dbus/v5"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

//go:embed build/appicon.png
var trayIcon []byte

func registerDesktopTray(onReady func()) {
	// Wails owns GTK's main loop. Register on its main thread without a second loop.
	systray.Register(onReady, nil)
}

func hideDesktopWindow(ctx context.Context) {
	// A desktop without an indicator host must keep a taskbar recovery path.
	bus, err := dbus.SessionBus()
	if err == nil {
		checkCtx, cancel := context.WithTimeout(ctx, 300*time.Millisecond)
		defer cancel()
		var hosted bool
		err = bus.BusObject().CallWithContext(checkCtx, "org.freedesktop.DBus.NameHasOwner", 0, "org.kde.StatusNotifierWatcher").Store(&hosted)
		if err == nil && hosted {
			runtime.WindowHide(ctx)
			return
		}
	}
	runtime.WindowMinimise(ctx)
}
