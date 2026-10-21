package service

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"sync/atomic"
	"testing"

	"lingma-ipc-proxy/internal/remote"
)

func TestVerifyExplicitRuntimeSettingsOverrideLegacyEnvironment(t *testing.T) {
	t.Setenv("LINGMA_PROXY_MAX_CONCURRENT", "2")
	t.Setenv("LINGMA_AGGREGATE_TOOL_STREAM", "true")
	zero, disabled := 0, false
	cfg := NormalizeRuntimeSettings(Config{MaxConcurrentRequests: 9, RemoteRetryCount: &zero, AggregateToolStream: &disabled})
	if cfg.MaxConcurrentRequests != 9 || *cfg.RemoteRetryCount != 0 || *cfg.AggregateToolStream || cfg.RemoteRetryBaseDelayMS != 1000 {
		t.Fatalf("explicit zero/false or precedence lost: %+v", cfg)
	}
	if err := cfg.ValidateRuntimeSettings(); err != nil {
		t.Fatal(err)
	}
	legacy := NormalizeRuntimeSettings(Config{})
	if legacy.MaxConcurrentRequests != 2 || !*legacy.AggregateToolStream || *legacy.RemoteRetryCount != 3 {
		t.Fatal("legacy defaults changed")
	}
}

func TestVerifyConfiguredRetryCountReachesRemoteClient(t *testing.T) {
	for _, retries := range []int{0, 1} {
		t.Run(fmt.Sprint(retries), func(t *testing.T) {
			var count atomic.Int32
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				count.Add(1)
				w.WriteHeader(429)
				fmt.Fprint(w, `{"error":{"code":"rate_limit_exceeded","message":"Too many requests"}}`)
			}))
			defer server.Close()
			auth := filepath.Join(t.TempDir(), "auth.json")
			if err := remote.SaveCredentialFile(remote.Credential{CosyKey: "test", EncryptUserInfo: "test", UserID: "test", MachineID: "test"}, auth); err != nil {
				t.Fatal(err)
			}
			svc := New(Config{Backend: BackendRemote, RemoteBaseURL: server.URL, RemoteAuthFile: auth, RemoteRetryCount: &retries, RemoteRetryBaseDelayMS: 100})
			defer svc.Close()
			if _, err := svc.Generate(context.Background(), ChatRequest{Model: "qmodel", Messages: []ChatMessage{{Role: "user", Text: "hello"}}}); err == nil {
				t.Fatal("expected upstream error")
			}
			if count.Load() != int32(retries+1) {
				t.Fatalf("retry setting ignored: %d attempts", count.Load())
			}
		})
	}
}
