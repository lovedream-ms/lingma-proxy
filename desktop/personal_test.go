package main

import (
	"encoding/json"
	"github.com/pelletier/go-toml/v2"
	"gopkg.in/yaml.v3"
	"lingma-ipc-proxy/internal/service"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestClientMergePreservesOtherSettings(t *testing.T) {
	cfg := service.Config{Host: "127.0.0.1", Port: 8096, Model: "kmodel", APIKey: "client-key"}
	for _, tc := range []struct{ tool, raw string }{
		{"claude", `{"permissions":{"allow":["Read"]},"env":{"OTHER":"keep"}}`},
		{"codex", "model_provider = 'existing'\n[projects.'C:\\work']\ntrust_level = 'trusted'\n"},
		{"continue", "name: Existing\nversion: 1.0.0\nschema: v1\nmodels:\n  - name: Existing Model\n    provider: openai\n    model: keep\n"},
	} {
		after, _, err := mergeClientConfig(tc.tool, []byte(tc.raw), cfg)
		if err != nil {
			t.Fatalf("%s %v", tc.tool, err)
		}
		m := map[string]any{}
		switch tc.tool {
		case "claude":
			err = json.Unmarshal(after, &m)
			if m["permissions"] == nil || m["env"].(map[string]any)["OTHER"] != "keep" {
				t.Fatal("Claude settings lost")
			}
		case "codex":
			err = toml.Unmarshal(after, &m)
			if m["model_provider"] != "existing" || m["projects"] == nil || m["profiles"] == nil {
				t.Fatal("Codex defaults/projects lost")
			}
		case "continue":
			err = yaml.Unmarshal(after, &m)
			if len(m["models"].([]any)) != 2 {
				t.Fatal("Continue model lost")
			}
		}
		if err != nil || !strings.Contains(string(after), "8096") || !strings.Contains(string(after), "client-key") {
			t.Fatalf("invalid config %s %v", after, err)
		}
	}
}

func TestClientApplyRestoreAndConflicts(t *testing.T) {
	for _, tool := range []string{"claude", "codex", "continue"} {
		t.Run(tool, func(t *testing.T) {
			dir := t.TempDir()
			t.Setenv("USERPROFILE", dir)
			t.Setenv("HOME", dir)
			t.Setenv("CODEX_HOME", filepath.Join(dir, "codex"))
			t.Setenv("CLAUDE_CONFIG_DIR", filepath.Join(dir, "claude"))
			a := &App{cfg: service.Config{Host: "127.0.0.1", Port: 8096, Model: "kmodel"}}
			preview, err := a.PreviewClientConfig(tool)
			if err != nil {
				t.Fatal(err)
			}
			id, err := a.ApplyClientConfig(tool, preview.Fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			if err = a.RestoreClientConfig(tool, id); err != nil {
				t.Fatal(err)
			}
			if _, err = os.Stat(preview.Path); !os.IsNotExist(err) {
				t.Fatal("new file not removed by restore")
			}
			if err = os.MkdirAll(filepath.Dir(preview.Path), 0700); err != nil {
				t.Fatal(err)
			}
			original := []byte("{}")
			if tool == "codex" {
				original = []byte("# preserve original comment\nmodel_provider='keep'\n")
			}
			if tool == "continue" {
				original = []byte("name: Existing\n")
			}
			if err = os.WriteFile(preview.Path, original, 0600); err != nil {
				t.Fatal(err)
			}
			preview, err = a.PreviewClientConfig(tool)
			if err != nil {
				t.Fatal(err)
			}
			if err = os.WriteFile(preview.Path, append(original, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err = a.ApplyClientConfig(tool, preview.Fingerprint); err == nil {
				t.Fatal("stale preview overwrote external edit")
			}
			preview, err = a.PreviewClientConfig(tool)
			if err != nil {
				t.Fatal(err)
			}
			id, err = a.ApplyClientConfig(tool, preview.Fingerprint)
			if err != nil {
				t.Fatal(err)
			}
			applied, _ := os.ReadFile(preview.Path)
			if err = os.WriteFile(preview.Path, append(applied, '\n'), 0600); err != nil {
				t.Fatal(err)
			}
			if err = a.RestoreClientConfig(tool, id); err == nil {
				t.Fatal("restore overwrote later edit")
			}
			if err = os.WriteFile(preview.Path, applied, 0600); err != nil {
				t.Fatal(err)
			}
			if err = a.RestoreClientConfig(tool, id); err != nil {
				t.Fatal(err)
			}
			restored, _ := os.ReadFile(preview.Path)
			if string(restored) != string(append(original, '\n')) {
				t.Fatal("original bytes not restored")
			}
		})
	}
}

func TestHistoryPolicyPreservesTotals(t *testing.T) {
	now := time.Now()
	a := &App{cfg: service.NormalizeRuntimeSettings(service.Config{LogMode: "metadata"}), stats: TokenStats{TotalTokens: 99}, requests: []RequestRecord{
		{ID: "old", CreatedAt: now.Add(-8 * 24 * time.Hour).Format(time.RFC3339), ReqBody: "private"},
		{ID: "new", Path: "/v1/responses", CreatedAt: now.Format(time.RFC3339), Model: "kmodel", StatusCode: 200, TotalTokens: 33, ReqBody: "private"},
	}}
	a.pruneHistoryLocked()
	if len(a.requests) != 1 || a.requests[0].ReqBody != "" || a.stats.TotalTokens != 99 {
		t.Fatal("privacy retention changed cumulative totals")
	}
	trends := a.GetUsageTrends()
	if len(trends) != 1 || trends[0].Tokens != 33 || trends[0].Success != 1 {
		t.Fatal("retained-history trends incorrect")
	}
}
