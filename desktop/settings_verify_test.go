package main

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"

	"lingma-ipc-proxy/internal/service"
)

func TestVerifySettingsSaveAndReloadSameFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "portable", "config.json")
	t.Setenv("LINGMA_PROXY_CONFIG", path)
	if err := os.MkdirAll(filepath.Dir(path), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(`{"unmanaged_option":{"keep":true}}`), 0600); err != nil {
		t.Fatal(err)
	}
	zero, disabled := 0, false
	cfg := service.NormalizeRuntimeSettings(service.Config{Host: "127.0.0.1", Port: 8096, Model: "future-route", Backend: service.BackendRemote, Timeout: 2 * time.Second, WarmupTimeout: 7 * time.Second, MaxConcurrentRequests: 9, RemoteRetryCount: &zero, RemoteRetryBaseDelayMS: 1500, AggregateToolStream: &disabled})
	app := &App{cfg: cfg}
	if err := app.saveConfig(cfg); err != nil {
		t.Fatal(err)
	}
	loaded := defaultConfig()
	if loaded.Model != "future-route" || loaded.MaxConcurrentRequests != 9 || *loaded.RemoteRetryCount != 0 || loaded.RemoteRetryBaseDelayMS != 1500 || *loaded.AggregateToolStream || loaded.Timeout != 2*time.Second || loaded.WarmupTimeout != 7*time.Second {
		t.Fatalf("settings did not survive restart: %+v", loaded)
	}
	app.cfg = loaded
	frontend := app.GetConfig()
	if frontend.Timeout != 2 || frontend.WarmupTimeout != 7 {
		t.Fatal("frontend duration units changed")
	}
	var raw map[string]any
	data, _ := os.ReadFile(path)
	if json.Unmarshal(data, &raw) != nil || raw["unmanaged_option"] == nil {
		t.Fatal("unmanaged configuration erased")
	}
	loaded.MaxConcurrentRequests = 17
	if err := app.UpdateConfig(loaded); err == nil {
		t.Fatal("invalid concurrency accepted")
	}
	if app.cfg.MaxConcurrentRequests != 9 {
		t.Fatal("invalid save changed running configuration")
	}
}

func TestExplicitConfigIsolatesDesktopInstanceAndState(t *testing.T) {
	t.Setenv("LINGMA_PROXY_CONFIG", "")
	if desktopInstanceID() != "lingma-proxy-desktop" {
		t.Fatal("default instance changed")
	}
	first := filepath.Join(t.TempDir(), "first.json")
	second := filepath.Join(t.TempDir(), "second.json")
	t.Setenv("LINGMA_PROXY_CONFIG", first)
	firstID := desktopInstanceID()
	state, err := appStatePath()
	if err != nil || state != first+".state.json" {
		t.Fatalf("state = %q: %v", state, err)
	}
	t.Setenv("LINGMA_PROXY_CONFIG", second)
	if desktopInstanceID() == firstID {
		t.Fatal("different profiles share a lock")
	}
	t.Setenv("LINGMA_PROXY_CONFIG", first)
	if desktopInstanceID() != firstID {
		t.Fatal("same profile cannot restore its window")
	}
}
