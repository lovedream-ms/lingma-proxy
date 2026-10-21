//go:build linux || windows

package main

import "github.com/getlantern/systray"

func startDesktopTray(app *App, ready <-chan struct{}) {
	registerDesktopTray(func() {
		systray.SetIcon(trayIcon)
		systray.SetTooltip("Lingma Proxy")
		show := systray.AddMenuItem("显示主窗口", "打开正在运行的 Lingma Proxy")
		systray.AddSeparator()
		quit := systray.AddMenuItem("退出 Lingma Proxy", "停止代理并退出")
		go func() {
			<-ready
			for {
				select {
				case <-show.ClickedCh:
					app.ShowWindow()
				case <-quit.ClickedCh:
					app.ForceQuitApp()
					return
				}
			}
		}()
	})
}

func stopDesktopTray() { systray.Quit() }
