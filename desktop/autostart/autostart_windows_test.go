package autostart

import (
	"fmt"
	"testing"
	"time"

	"golang.org/x/sys/windows/registry"
)

func TestVerifyWindowsLoginRegistration(t *testing.T) {
	// Exercise native registry behavior in a disposable non-startup key only.
	path := fmt.Sprintf(`Software\LingmaProxyAutostartTest-%d`, time.Now().UnixNano())
	t.Cleanup(func() { _ = registry.DeleteKey(registry.CURRENT_USER, path) })
	starter := &windowsAutostarter{appName: "LingmaProxy", appPath: `C:\Program Files\Lingma Proxy\proxy.exe`, keyPath: path}
	if enabled, err := starter.IsEnabled(); err != nil || enabled {
		t.Fatalf("absent registration = %v, %v", enabled, err)
	}
	if err := starter.Disable(); err != nil {
		t.Fatal(err)
	}
	if err := starter.Enable(); err != nil {
		t.Fatal(err)
	}
	key, err := registry.OpenKey(registry.CURRENT_USER, path, registry.QUERY_VALUE)
	if err != nil {
		t.Fatal(err)
	}
	value, _, err := key.GetStringValue(starter.appName)
	key.Close()
	if err != nil || value != `"C:\Program Files\Lingma Proxy\proxy.exe"` {
		t.Fatalf("command = %q, %v", value, err)
	}
	if enabled, err := starter.IsEnabled(); err != nil || !enabled {
		t.Fatalf("registration = %v, %v", enabled, err)
	}
	if err := starter.Disable(); err != nil {
		t.Fatal(err)
	}
	if err := starter.Disable(); err != nil {
		t.Fatal(err)
	}
}
