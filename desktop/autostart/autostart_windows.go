package autostart

import (
	"errors"

	"golang.org/x/sys/windows/registry"
)

const windowsRunKey = `Software\Microsoft\Windows\CurrentVersion\Run`

type windowsAutostarter struct {
	appName string
	appPath string
	keyPath string
}

func newWindowsAutostarter(appName, appPath string) Autostarter {
	return &windowsAutostarter{appName: appName, appPath: appPath, keyPath: windowsRunKey}
}

func (w *windowsAutostarter) Enable() error {
	if err := validateEntry(w.appName, w.appPath); err != nil {
		return err
	}
	key, _, err := registry.CreateKey(registry.CURRENT_USER, w.keyPath, registry.SET_VALUE)
	if err != nil {
		return err
	}
	defer key.Close()
	// The Run value is a command line; paths with spaces must remain a single argument.
	return key.SetStringValue(w.appName, `"`+w.appPath+`"`)
}

func (w *windowsAutostarter) Disable() error {
	key, err := registry.OpenKey(registry.CURRENT_USER, w.keyPath, registry.SET_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	defer key.Close()
	err = key.DeleteValue(w.appName)
	if errors.Is(err, registry.ErrNotExist) {
		return nil
	}
	return err
}

func (w *windowsAutostarter) IsEnabled() (bool, error) {
	key, err := registry.OpenKey(registry.CURRENT_USER, w.keyPath, registry.QUERY_VALUE)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	defer key.Close()
	value, _, err := key.GetStringValue(w.appName)
	if errors.Is(err, registry.ErrNotExist) {
		return false, nil
	}
	return value != "", err
}
