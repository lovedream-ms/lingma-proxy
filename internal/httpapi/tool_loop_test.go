package httpapi

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func loopAPIRequest(stream bool, sequence []string, output string) map[string]any {
	messages := []any{map[string]any{"role": "user", "content": "audit the project"}}
	for i, command := range sequence {
		id := fmt.Sprintf("old-%d", i)
		args, _ := json.Marshal(map[string]any{"command": command, "description": fmt.Sprint(i)})
		messages = append(messages,
			map[string]any{"role": "assistant", "tool_calls": []any{map[string]any{"id": id, "type": "function", "function": map[string]any{"name": "bash", "arguments": string(args)}}}},
			map[string]any{"role": "tool", "tool_call_id": id, "content": output},
		)
	}
	return map[string]any{"model": "gmodel", "messages": messages, "stream": stream, "stream_options": map[string]any{"include_usage": true}, "tools": []any{map[string]any{"type": "function", "function": map[string]any{"name": "bash", "parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}, "required": []string{"command"}}}}}}
}

func loopReply(command string) []map[string]any {
	args, _ := json.Marshal(map[string]any{"command": command, "description": "new label"})
	return []map[string]any{
		verifyDelta(map[string]any{"content": "WITHHELD_PREAMBLE"}),
		verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "new-call", "type": "function", "function": map[string]any{"name": "bash", "arguments": string(args[:len(args)/2])}}}}),
		verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": string(args[len(args)/2:])}}}}),
		{"choices": []any{}, "usage": map[string]any{"prompt_tokens": 100, "completion_tokens": 20}},
	}
}

func TestToolLoopAPIBlocksAAndABBeforeToolEmission(t *testing.T) {
	for _, stream := range []bool{false, true} {
		for _, sequence := range [][]string{{"A", "A", "A"}, {"A", "B", "A", "B", "A", "B"}} {
			t.Run(fmt.Sprintf("stream=%v/%v", stream, sequence), func(t *testing.T) {
				t.Setenv("LINGMA_TOOL_LOOP_REPEATS", "3")
				t.Setenv("LINGMA_AGGREGATE_TOOL_STREAM", "false")
				s, count := verifyHarnessServer(t, func(n int, payload map[string]any) []map[string]any {
					if n == 2 {
						encoded, _ := json.Marshal(payload["messages"])
						if !strings.Contains(string(encoded), "Proxy tool-loop guard") || !strings.Contains(string(encoded), "old-0") {
							t.Error("correction lost evidence or instruction")
						}
					}
					return loopReply("A")
				})
				request := loopAPIRequest(stream, sequence, "0")
				raw, _ := json.Marshal(request)
				rec := httptest.NewRecorder()
				s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/chat/completions", strings.NewReader(string(raw))))
				body := rec.Body.String()
				if count.Load() != 2 || !strings.Contains(body, "tool_loop_detected") || !strings.Contains(body, `"retryable":false`) {
					t.Fatalf("unbounded or unclassified rejection (%d): %s", count.Load(), body)
				}
				if strings.Contains(body, "new-call") || strings.Contains(body, "WITHHELD_PREAMBLE") || strings.Contains(body, `"finish_reason":"tool_calls"`) {
					t.Fatalf("rejected action escaped: %s", body)
				}
				if !stream && rec.Code != http.StatusBadRequest {
					t.Fatalf("retryable HTTP status: %d", rec.Code)
				}
			})
		}
	}
}

func TestToolLoopCorrectionCanFinishOrDoNewWork(t *testing.T) {
	for _, finish := range []bool{false, true} {
		t.Run(fmt.Sprint(finish), func(t *testing.T) {
			t.Setenv("LINGMA_TOOL_LOOP_REPEATS", "3")
			t.Setenv("LINGMA_AGGREGATE_TOOL_STREAM", "false")
			s, count := verifyHarnessServer(t, func(n int, payload map[string]any) []map[string]any {
				if n == 1 {
					return loopReply("A")
				}
				if finish {
					return []map[string]any{verifyDelta(map[string]any{"content": "Audit complete."}), {"choices": []any{}, "usage": map[string]any{"prompt_tokens": 110, "completion_tokens": 10}}}
				}
				return loopReply("different operation")
			})
			raw, _ := json.Marshal(loopAPIRequest(true, []string{"A", "A", "A"}, "0"))
			body := verifyHarnessRequestBody(t, s, string(raw))
			if count.Load() != 2 || strings.Contains(body, `"error"`) {
				t.Fatalf("valid correction failed (%d): %s", count.Load(), body)
			}
			if finish {
				if !strings.Contains(body, "Audit complete.") || strings.Contains(body, "WITHHELD_PREAMBLE") || !strings.Contains(body, `"prompt_tokens":210`) || !strings.Contains(body, `"used_tokens":120`) {
					t.Fatalf("correction output/usage/context incorrect: %s", body)
				}
			} else if !strings.Contains(body, "different operation") || !strings.Contains(body, `"finish_reason":"tool_calls"`) {
				t.Fatalf("new work blocked: %s", body)
			}
		})
	}
}

func TestToolLoopAcrossProtocolsAndAggregation(t *testing.T) {
	for _, aggregate := range []string{"false", "true"} {
		for _, stream := range []bool{false, true} {
			for _, path := range []string{"/v1/chat/completions", "/v1/messages", "/v1/responses"} {
				t.Run(fmt.Sprintf("%s/aggregate=%s/stream=%v", path, aggregate, stream), func(t *testing.T) {
					t.Setenv("LINGMA_AGGREGATE_TOOL_STREAM", aggregate)
					t.Setenv("LINGMA_TOOL_LOOP_REPEATS", "3")
					s, count := verifyHarnessServer(t, func(int, map[string]any) []map[string]any { return loopReply("A") })
					request := loopAPIRequest(stream, []string{"A", "A", "A"}, "0")
					if path == "/v1/responses" {
						input := []any{map[string]any{"role": "user", "content": "audit"}}
						for i := 0; i < 3; i++ {
							id := fmt.Sprint(i)
							input = append(input, map[string]any{"type": "function_call", "call_id": id, "name": "bash", "arguments": `{"command":"A"}`}, map[string]any{"type": "function_call_output", "call_id": id, "output": "0"})
						}
						request = map[string]any{"model": "gmodel", "stream": stream, "input": input, "tools": []any{map[string]any{"type": "function", "name": "bash", "parameters": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}}}}}}
					} else if path == "/v1/messages" {
						messages := []any{map[string]any{"role": "user", "content": "audit"}}
						for i := 0; i < 3; i++ {
							id := fmt.Sprint(i)
							messages = append(messages, map[string]any{"role": "assistant", "content": []any{map[string]any{"type": "tool_use", "id": id, "name": "bash", "input": map[string]any{"command": "A"}}}}, map[string]any{"role": "user", "content": []any{map[string]any{"type": "tool_result", "tool_use_id": id, "content": "0"}}})
						}
						request = map[string]any{"model": "gmodel", "stream": stream, "max_tokens": 1000, "messages": messages, "tools": []any{map[string]any{"name": "bash", "input_schema": map[string]any{"type": "object", "properties": map[string]any{"command": map[string]any{"type": "string"}, "description": map[string]any{"type": "string"}}}}}}
					}
					raw, _ := json.Marshal(request)
					rec := httptest.NewRecorder()
					s.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, path, strings.NewReader(string(raw))))
					body := rec.Body.String()
					if count.Load() != 2 || !strings.Contains(body, "tool_loop_detected") || strings.Contains(body, "new-call") || strings.Contains(body, "WITHHELD_PREAMBLE") {
						t.Fatalf("protocol guard failed (status=%d upstream=%d): %s", rec.Code, count.Load(), body)
					}
				})
			}
		}
	}
}
