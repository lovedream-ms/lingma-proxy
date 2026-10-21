package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"

	"lingma-ipc-proxy/internal/remote"
	"lingma-ipc-proxy/internal/service"
)

func verifyHarnessServer(t *testing.T, reply func(int, map[string]any) []map[string]any) (*Server, *atomic.Int32) {
	t.Helper()
	var count atomic.Int32
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload map[string]any
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Error(err)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		for _, frame := range reply(int(count.Add(1)), payload) {
			inner, _ := json.Marshal(frame)
			outer, _ := json.Marshal(map[string]any{"body": string(inner), "statusCodeValue": 200})
			fmt.Fprintf(w, "data: %s\n\n", outer)
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	}))
	t.Cleanup(upstream.Close)
	auth := filepath.Join(t.TempDir(), "auth.json")
	if err := remote.SaveCredentialFile(remote.Credential{CosyKey: "test", EncryptUserInfo: "test", UserID: "test", MachineID: "test"}, auth); err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Config{Backend: service.BackendRemote, RemoteBaseURL: upstream.URL, RemoteAuthFile: auth})
	t.Cleanup(func() { _ = svc.Close() })
	return NewServer("", svc), &count
}

func verifyDelta(delta map[string]any) map[string]any {
	return map[string]any{"choices": []any{map[string]any{"index": 0, "delta": delta}}}
}

const verifyHarnessRequest = `{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"system","content":"Use tools.web_search({query}) from run_code."},{"role":"user","content":"搜索 Go 文档"}],"tools":[{"type":"function","function":{"name":"run_code","parameters":{"type":"object","properties":{"code":{"type":"string"}},"required":["code"]}}}]}`

func verifyHarnessRequestBody(t *testing.T, s *Server, body string) string {
	t.Helper()
	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(body)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d: %s", rec.Code, rec.Body.String())
	}
	return rec.Body.String()
}

func TestVerifyHarnessRetryDiscardsPreambleAndPreservesUsage(t *testing.T) {
	s, count := verifyHarnessServer(t, func(n int, payload map[string]any) []map[string]any {
		if n == 1 {
			messages := payload["messages"].([]any)
			if len(messages) < 2 || messages[0].(map[string]any)["role"] != "system" {
				t.Error("primary request lost structured history")
			}
			return []map[string]any{verifyDelta(map[string]any{"content": "让我先搜索相关资料："})}
		}
		if _, ok := payload["tools"]; ok {
			t.Error("repair mixes native tools and action emulation")
		}
		return []map[string]any{
			verifyDelta(map[string]any{"content": "```json action\n{\"tool\":\"web_search\",\"parameters\":{\"query\":\"Go docs\"}}\n```"}),
			{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 123, "completion_tokens": 45, "prompt_tokens_details": map[string]any{"cached_tokens": 12}, "completion_tokens_details": map[string]any{"reasoning_tokens": 5}}},
		}
	})
	body := verifyHarnessRequestBody(t, s, verifyHarnessRequest)
	if count.Load() != 2 || strings.Contains(body, "让我") || strings.Contains(body, "json action") || strings.Contains(body, `"error"`) {
		t.Fatalf("retry leaked/failed, count=%d body=%s", count.Load(), body)
	}
	if !strings.Contains(body, `"name":"run_code"`) || !strings.Contains(body, `"finish_reason":"tool_calls"`) {
		t.Fatalf("missing normalized call: %s", body)
	}
	finish, usage, done := strings.Index(body, `"finish_reason":"tool_calls"`), strings.Index(body, `"usage":`), strings.Index(body, "[DONE]")
	if !(finish < usage && usage < done) || !strings.Contains(body, `"prompt_tokens":123`) || !strings.Contains(body, `"completion_tokens":45`) || !strings.Contains(body, `"cached_tokens":12`) || !strings.Contains(body, `"reasoning_tokens":5`) {
		t.Fatalf("usage missing or out of order: %s", body)
	}
}

func TestVerifyHarnessNativeFragmentRepairAndHistory(t *testing.T) {
	s, count := verifyHarnessServer(t, func(n int, payload map[string]any) []map[string]any {
		messages, _ := json.Marshal(payload["messages"])
		if !strings.Contains(string(messages), `"tool_call_id":"previous"`) || !strings.Contains(string(messages), `"tool_calls"`) {
			t.Error("tool history or call ID lost")
		}
		return []map[string]any{
			verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "native-1", "function": map[string]any{"name": "run_code", "arguments": `{"code":"console.`}}}}),
			verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `log(1)",}`}}}}),
		}
	})
	var request map[string]any
	_ = json.Unmarshal([]byte(verifyHarnessRequest), &request)
	request["messages"] = []any{
		map[string]any{"role": "user", "content": "搜索 Go 文档"},
		map[string]any{"role": "assistant", "content": nil, "tool_calls": []any{map[string]any{"id": "previous", "type": "function", "function": map[string]any{"name": "run_code", "arguments": `{"code":"console.log(0)"}`}}}},
		map[string]any{"role": "tool", "tool_call_id": "previous", "content": "0"},
	}
	raw, _ := json.Marshal(request)
	body := verifyHarnessRequestBody(t, s, string(raw))
	if count.Load() != 1 || !strings.Contains(body, `"id":"native-1"`) || strings.Contains(body, "raw_arguments") || strings.Contains(body, `"error"`) || !strings.Contains(body, `"usage":`) {
		t.Fatalf("fragment normalization failed: %s", body)
	}
}

func TestVerifyHarnessMalformedStreamRepairIsBoundedBeforeEmission(t *testing.T) {
	s, count := verifyHarnessServer(t, func(n int, payload map[string]any) []map[string]any {
		return []map[string]any{verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"name": "run_code", "arguments": `{"code":4}`}}}})}
	})
	body := verifyHarnessRequestBody(t, s, verifyHarnessRequest)
	if count.Load() != 2 || !strings.Contains(body, `"error"`) || strings.Contains(body, `"tool_calls"`) || strings.Contains(body, `"finish_reason":"stop"`) || !strings.Contains(body, "[DONE]") {
		t.Fatalf("bad call was silently accepted or retried unboundedly: count=%d %s", count.Load(), body)
	}
}

func TestVerifyHarnessPlainJSONAndUsageOptOut(t *testing.T) {
	s, count := verifyHarnessServer(t, func(n int, payload map[string]any) []map[string]any {
		return []map[string]any{verifyDelta(map[string]any{"content": `{"answer":"OK"}`})}
	})
	body := verifyHarnessRequestBody(t, s, strings.Replace(verifyHarnessRequest, `"include_usage":true`, `"include_usage":false`, 1))
	if count.Load() != 1 || strings.Contains(body, `"error"`) || strings.Contains(body, `"usage":`) || !strings.Contains(body, `"finish_reason":"stop"`) {
		t.Fatalf("ordinary JSON or usage opt-out broken: %s", body)
	}
}
