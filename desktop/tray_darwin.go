package main

import (
	"context"
	"github.com/wailsapp/wails/v2/pkg/runtime"
)

// macOS restores the existing window through the Dock.
func startDesktopTray(_ *App, _ <-chan struct{}) {}
func stopDesktopTray()                           {}
func hideDesktopWindow(ctx context.Context)      { runtime.Hide(ctx) }
