package autostart

import (
	"encoding/xml"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyLinuxLoginRegistration(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("XDG_CONFIG_HOME", dir)
	starter := &linuxAutostarter{appName: "LingmaProxy", appPath: filepath.Join(dir, "Program Files", "Lingma % proxy")}
	if err := starter.Disable(); err != nil {
		t.Fatalf("disable absent entry: %v", err)
	}
	if err := starter.Enable(); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(filepath.Join(dir, "autostart", "LingmaProxy.desktop"))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "Exec=\"") || !strings.Contains(string(data), "Lingma %% proxy\"") {
		t.Fatalf("Exec must quote spaces and escape field codes: %s", data)
	}
	if enabled, err := starter.IsEnabled(); err != nil || !enabled {
		t.Fatalf("registered = %v, %v", enabled, err)
	}
	if err := starter.Disable(); err != nil {
		t.Fatal(err)
	}
	if enabled, err := starter.IsEnabled(); err != nil || enabled {
		t.Fatalf("disabled = %v, %v", enabled, err)
	}
	if err := starter.Disable(); err != nil {
		t.Fatalf("repeated disable: %v", err)
	}
}

func TestVerifyMacLoginRegistrationEscapesXML(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", dir)
	t.Setenv("USERPROFILE", dir)
	starter := &macosAutostarter{appName: "Lingma&Proxy", appPath: filepath.Join(dir, "Lingma & Proxy", "app")}
	if err := starter.Enable(); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "Library", "LaunchAgents", "Lingma&Proxy.plist")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "&amp;") {
		t.Fatalf("expected XML escaped label and executable: %s", data)
	}
	decoder := xml.NewDecoder(strings.NewReader(string(data)))
	for {
		_, err := decoder.Token()
		if err == io.EOF {
			break
		}
		if err != nil {
			t.Fatalf("invalid plist XML: %v", err)
		}
	}
	if err := starter.Disable(); err != nil {
		t.Fatal(err)
	}
	if err := starter.Disable(); err != nil {
		t.Fatal(err)
	}
}

func TestVerifyLoginRegistrationRejectsRelativeExecutable(t *testing.T) {
	starter := &linuxAutostarter{appName: "LingmaProxy", appPath: "proxy"}
	if err := starter.Enable(); err == nil {
		t.Fatal("relative executable must fail")
	}
}
