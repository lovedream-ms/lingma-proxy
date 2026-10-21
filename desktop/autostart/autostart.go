// Package autostart manages per-user desktop login startup registrations.
package autostart

import (
	"bytes"
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

type Autostarter interface {
	Enable() error
	Disable() error
	IsEnabled() (bool, error)
}

func New(appName, appPath string) Autostarter {
	switch runtime.GOOS {
	case "windows":
		return newWindowsAutostarter(appName, appPath)
	case "darwin":
		return &macosAutostarter{appName: appName, appPath: appPath}
	case "linux":
		return &linuxAutostarter{appName: appName, appPath: appPath}
	default:
		return &dummyAutostarter{}
	}
}

type dummyAutostarter struct{}

func (*dummyAutostarter) Enable() error {
	return fmt.Errorf("autostart not supported on this platform")
}
func (*dummyAutostarter) Disable() error {
	return fmt.Errorf("autostart not supported on this platform")
}
func (*dummyAutostarter) IsEnabled() (bool, error) {
	return false, fmt.Errorf("autostart not supported on this platform")
}

func validateEntry(appName, appPath string) error {
	if appName == "" || strings.ContainsAny(appName, "/\\\r\n") || appName == "." || appName == ".." {
		return fmt.Errorf("invalid autostart application name")
	}
	if !filepath.IsAbs(appPath) || strings.ContainsAny(appPath, "\r\n\x00") {
		return fmt.Errorf("autostart requires an absolute executable path")
	}
	return nil
}

func removeEntry(path string) error {
	if err := os.Remove(path); err != nil && !os.IsNotExist(err) {
		return err
	}
	return nil
}

func entryExists(path string) (bool, error) {
	info, err := os.Stat(path)
	if os.IsNotExist(err) {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return !info.IsDir(), nil
}

type macosAutostarter struct {
	appName string
	appPath string
}

func (m *macosAutostarter) path() (string, error) {
	if err := validateEntry(m.appName, m.appPath); err != nil {
		return "", err
	}
	home, err := os.UserHomeDir()
	return filepath.Join(home, "Library", "LaunchAgents", m.appName+".plist"), err
}

func (m *macosAutostarter) Enable() error {
	path, err := m.path()
	if err != nil {
		return err
	}
	escape := func(value string) string {
		var out bytes.Buffer
		_ = xml.EscapeText(&out, []byte(value))
		return out.String()
	}
	plist := fmt.Sprintf(`<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN" "http://www.apple.com/DTDs/PropertyList-1.0.dtd">
<plist version="1.0"><dict>
<key>Label</key><string>%s</string>
<key>ProgramArguments</key><array><string>%s</string></array>
<key>RunAtLoad</key><true/>
</dict></plist>
`, escape(m.appName), escape(m.appPath))
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	// LaunchAgents are discovered at the next login. Loading now would start a second app.
	return os.WriteFile(path, []byte(plist), 0644)
}

func (m *macosAutostarter) Disable() error {
	path, err := m.path()
	if err != nil {
		return err
	}
	return removeEntry(path)
}

func (m *macosAutostarter) IsEnabled() (bool, error) {
	path, err := m.path()
	if err != nil {
		return false, err
	}
	return entryExists(path)
}

type linuxAutostarter struct {
	appName string
	appPath string
}

func (l *linuxAutostarter) path() (string, error) {
	if err := validateEntry(l.appName, l.appPath); err != nil {
		return "", err
	}
	dir := os.Getenv("XDG_CONFIG_HOME")
	if !filepath.IsAbs(dir) {
		home, err := os.UserHomeDir()
		if err != nil {
			return "", err
		}
		dir = filepath.Join(home, ".config")
	}
	return filepath.Join(dir, "autostart", l.appName+".desktop"), nil
}

func (l *linuxAutostarter) Enable() error {
	path, err := l.path()
	if err != nil {
		return err
	}
	// Desktop Exec has its own quoting rules, followed by desktop string escaping.
	command := strings.NewReplacer("\\", "\\\\", "\"", "\\\"", "`", "\\`", "$", "\\$", "%", "%%").Replace(l.appPath)
	command = strings.ReplaceAll("\""+command+"\"", "\\", "\\\\")
	entry := fmt.Sprintf("[Desktop Entry]\nType=Application\nName=%s\nExec=%s\nTerminal=false\nHidden=false\nX-GNOME-Autostart-enabled=true\n", l.appName, command)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		return err
	}
	return os.WriteFile(path, []byte(entry), 0644)
}

func (l *linuxAutostarter) Disable() error {
	path, err := l.path()
	if err != nil {
		return err
	}
	return removeEntry(path)
}

func (l *linuxAutostarter) IsEnabled() (bool, error) {
	path, err := l.path()
	if err != nil {
		return false, err
	}
	return entryExists(path)
}
