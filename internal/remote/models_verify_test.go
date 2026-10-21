package remote

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestVerifyRemoteDiscoveryAllScenesWithoutGeneration(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodGet || r.URL.Path != modelListPath {
			t.Errorf("unexpected discovery request: %s %s", r.Method, r.URL.Path)
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if !strings.HasPrefix(r.Header.Get("Authorization"), "Bearer COSY.") {
			t.Error("missing signed authentication")
		}
		w.Header().Set("Content-Type", "application/json")
		fmt.Fprint(w, `{"assistant":[`)
		for i := 0; i < 7; i++ {
			if i > 0 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"key":"route-%d","display_name":"Model %d","max_input_tokens":180000,"is_vl":false}`, i, i)
		}
		fmt.Fprint(w, `],"qwork":[`)
		for i := 7; i < 14; i++ {
			if i > 7 {
				fmt.Fprint(w, ",")
			}
			fmt.Fprintf(w, `{"key":"route-%d","displayName":"Model %d","maxInputTokens":96000,"isVl":true}`, i, i)
		}
		fmt.Fprint(w, `]}`)
	}))
	defer server.Close()
	authFile := filepath.Join(t.TempDir(), "credentials.json")
	if err := SaveCredentialFile(Credential{CosyKey: "test", EncryptUserInfo: "test", UserID: "test", MachineID: "test"}, authFile); err != nil {
		t.Fatal(err)
	}
	models, err := New(Config{BaseURL: server.URL, AuthFile: authFile}).ListModels(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 14 {
		t.Fatalf("model count=%d, want 14", len(models))
	}
	if models[0].ContextWindow != 0 || models[0].MaxInputTokens != 180000 || models[0].SupportsVision || models[13].ContextWindow != 0 || models[13].MaxInputTokens != 96000 || !models[13].SupportsVision {
		t.Fatalf("upstream metadata lost: first=%+v last=%+v", models[0], models[13])
	}
}

func TestVerifyModernCredentialCompatibilityDiagnostic(t *testing.T) {
	cacheDir := t.TempDir()
	t.Setenv("LINGMA_CACHE_DIR", cacheDir)
	if err := os.MkdirAll(filepath.Join(cacheDir, "cache"), 0700); err != nil {
		t.Fatal(err)
	}
	statusPath := filepath.Join(cacheDir, "cache", "status.json")
	if err := os.WriteFile(statusPath, []byte(`{"logged_in":true,"version":"1.34.0"}`), 0600); err != nil {
		t.Fatal(err)
	}
	warning := CredentialCompatibilityWarning("")
	if !strings.Contains(warning, "IPC") || !strings.Contains(warning, "COSY") {
		t.Fatalf("missing compatibility diagnosis: %q", warning)
	}
	if warning := CredentialCompatibilityWarning("explicit-credentials.json"); warning != "" {
		t.Fatalf("explicit credentials wrongly warned: %q", warning)
	}
	if err := os.WriteFile(statusPath, []byte(`{"logged_in":false}`), 0600); err != nil {
		t.Fatal(err)
	}
	if warning := CredentialCompatibilityWarning(""); warning != "" {
		t.Fatalf("signed-out cache wrongly warned: %q", warning)
	}
}
