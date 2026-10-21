package main

import (
	"encoding/json"
	"lingma-ipc-proxy/internal/service"
	"os"
	"testing"
	"time"
)

func TestVerifyDesktopConfigWorksInConsole(t *testing.T) {
	var file fileConfig
	if err := json.Unmarshal([]byte(`{"max_concurrent_requests":12,"remote_retry_count":0,"remote_retry_base_delay_ms":1500,"aggregate_tool_stream":false,"warmup_timeout":7}`), &file); err != nil {
		t.Fatal(err)
	}
	cfg := service.Config{}
	overlayFileConfig(&cfg, file)
	cfg = service.NormalizeRuntimeSettings(cfg)
	if cfg.MaxConcurrentRequests != 12 || *cfg.RemoteRetryCount != 0 || cfg.RemoteRetryBaseDelayMS != 1500 || *cfg.AggregateToolStream || cfg.WarmupTimeout != 7*time.Second {
		t.Fatalf("desktop/console settings diverged: %+v", cfg)
	}
}

func TestConfigFlagSpellingsResolveBeforeDefaults(t *testing.T) {
	saved := os.Args
	t.Cleanup(func() { os.Args = saved })
	for _, args := range [][]string{
		{"proxy", "-config", "selected.json"},
		{"proxy", "--config", "selected.json"},
		{"proxy", "-config=selected.json"},
		{"proxy", "--config=selected.json"},
		{"proxy", "--config", "old.json", "-config=selected.json"},
	} {
		os.Args = args
		path, found := resolveConfigPath()
		if !found || path != "selected.json" {
			t.Fatalf("args=%q path=%q loaded=%v", args, path, found)
		}
	}
	os.Args = []string{"proxy", "--", "-config=ignored.json"}
	if lookupArgValue("--config") != "" {
		t.Fatal("parsed config after end of flags")
	}
}
