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

func TestVerifyHarnessDocumentationAcrossProtocols(t *testing.T) {
	text := strings.Repeat("项目功能：支持协议转换与工具调用。", 20) + "\nAction Block 解析 (` ```json action`)。\n字段 `{\"tool\"` 与 `{\"name\"` 是说明。\n```json\n{\"name\":\"Alice\"}\n```\n````markdown\n```json action\n{\"tool\":\"read\",\"parameters\":{}}\n```\n````\n完整总结结束。"
	for _, aggregate := range []string{"false", "true"} {
		for _, stream := range []bool{false, true} {
			for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
				t.Run(fmt.Sprintf("%s/aggregate=%s/stream=%v", path, aggregate, stream), func(t *testing.T) {
					t.Setenv("LINGMA_AGGREGATE_TOOL_STREAM", aggregate)
					s, count := verifyHarnessServer(t, func(int, map[string]any) []map[string]any {
						var frames []map[string]any
						for _, r := range text {
							frames = append(frames, verifyDelta(map[string]any{"content": string(r)}))
						}
						return append(frames, map[string]any{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 200}})
					})
					request := map[string]any{"model": "kmodel", "stream": stream, "messages": []any{map[string]any{"role": "user", "content": "总结项目功能"}}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "read", "parameters": map[string]any{"type": "object"}}}}}
					if path == "/v1/messages" {
						request["max_tokens"] = 2000
						request["tools"] = []any{map[string]any{"name": "read", "input_schema": map[string]any{"type": "object"}}}
					} else if path == "/v1/responses" {
						delete(request, "messages")
						request["input"] = "总结项目功能"
						request["tools"] = []any{map[string]any{"type": "function", "name": "read", "parameters": map[string]any{"type": "object"}}}
					}
					raw, _ := json.Marshal(request)
					rec := httptest.NewRecorder()
					s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw))))
					body := rec.Body.String()
					if rec.Code != http.StatusOK || count.Load() != 1 || strings.Contains(body, "unrecognized tool action") || strings.Contains(body, `"error":{`) {
						t.Fatalf("documentation rejected or retried: status=%d count=%d body=%s", rec.Code, count.Load(), body)
					}
					var got strings.Builder
					if stream {
						for _, line := range strings.Split(body, "\n") {
							if !strings.HasPrefix(line, "data: {") {
								continue
							}
							var frame struct {
								Type    string          `json:"type"`
								Text    string          `json:"text"`
								Delta   json.RawMessage `json:"delta"`
								Choices []struct {
									Delta struct {
										Content string `json:"content"`
									} `json:"delta"`
								} `json:"choices"`
							}
							if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
								t.Fatal(err)
							}
							if path == "/v1/chat/completions" {
								for _, c := range frame.Choices {
									got.WriteString(c.Delta.Content)
								}
							} else if frame.Type == "response.output_text.delta" {
								var delta string
								_ = json.Unmarshal(frame.Delta, &delta)
								got.WriteString(delta)
							} else if frame.Type == "response.output_text.done" && got.Len() == 0 {
								got.WriteString(frame.Text)
							} else if frame.Type == "content_block_delta" {
								var delta struct {
									Text string `json:"text"`
								}
								_ = json.Unmarshal(frame.Delta, &delta)
								got.WriteString(delta.Text)
							}
						}
					} else {
						var response struct {
							Choices []struct {
								Message struct {
									Content string `json:"content"`
								} `json:"message"`
							} `json:"choices"`
							Content []struct {
								Text string `json:"text"`
							} `json:"content"`
							Output []struct {
								Content []struct {
									Text string `json:"text"`
								} `json:"content"`
							} `json:"output"`
						}
						if err := json.Unmarshal(rec.Body.Bytes(), &response); err != nil {
							t.Fatal(err)
						}
						for _, c := range response.Choices {
							got.WriteString(c.Message.Content)
						}
						for _, c := range response.Content {
							got.WriteString(c.Text)
						}
						for _, o := range response.Output {
							for _, c := range o.Content {
								got.WriteString(c.Text)
							}
						}
					}
					if got.String() != text {
						t.Fatalf("text lost or changed: got %q want %q", got.String(), text)
					}
				})
			}
		}
	}
}
