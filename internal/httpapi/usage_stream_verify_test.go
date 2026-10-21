package httpapi

import (
	"bufio"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"lingma-ipc-proxy/internal/remote"
	"lingma-ipc-proxy/internal/service"
)

func TestVerifyNativeToolsArriveBeforeUpstreamCompletes(t *testing.T) {
	release := make(chan struct{})
	var once sync.Once
	unblock := func() { once.Do(func() { close(release) }) }
	defer unblock()
	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodGet {
			io.WriteString(w, `{"app":[{"key":"kmodel","display_name":"Kimi","context_window":200000}]}`)
			return
		}
		w.Header().Set("Content-Type", "text/event-stream")
		first := verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "id": "stream-call", "type": "function", "function": map[string]any{"name": "run_code", "arguments": `{"code":"`}}}})
		encoded, _ := json.Marshal(first)
		fmt.Fprintf(w, "data: %s\n\n", encoded)
		w.(http.Flusher).Flush()
		select {
		case <-release:
		case <-r.Context().Done():
			return
		}
		last := verifyDelta(map[string]any{"tool_calls": []any{map[string]any{"index": 0, "function": map[string]any{"arguments": `console.log(1)"}`}}}})
		encoded, _ = json.Marshal(last)
		fmt.Fprintf(w, "data: %s\n\ndata: {\"usage\":{\"prompt_tokens\":1200,\"completion_tokens\":40,\"prompt_tokens_details\":{\"cached_tokens\":1000}}}\n\ndata: [DONE]\n\n", encoded)
	}))
	defer upstream.Close()
	auth := filepath.Join(t.TempDir(), "auth.json")
	if err := remote.SaveCredentialFile(remote.Credential{CosyKey: "test", EncryptUserInfo: "test", UserID: "test", MachineID: "test"}, auth); err != nil {
		t.Fatal(err)
	}
	svc := service.New(service.Config{Backend: service.BackendRemote, RemoteBaseURL: upstream.URL, RemoteAuthFile: auth})
	defer svc.Close()
	if _, err := svc.ListModels(context.Background()); err != nil {
		t.Fatal(err)
	}
	proxy := httptest.NewServer(NewServer("", svc).http.Handler)
	defer proxy.Close()
	// Unblock the upstream before closing servers, including on assertion failure.
	defer unblock()
	client := &http.Client{Timeout: 3 * time.Second}
	response, err := client.Post(proxy.URL+"/v1/chat/completions", "application/json", strings.NewReader(verifyHarnessRequest))
	if err != nil {
		t.Fatal(err)
	}
	defer response.Body.Close()
	scanner := bufio.NewScanner(response.Body)
	var arguments strings.Builder
	var usage map[string]any
	toolFrames := 0
	for scanner.Scan() {
		line := scanner.Text()
		if !strings.HasPrefix(line, "data: ") || strings.Contains(line, "[DONE]") {
			continue
		}
		var frame struct {
			Choices []struct {
				Delta struct {
					ToolCalls []struct {
						Function struct{ Name, Arguments string }
					} `json:"tool_calls"`
				} `json:"delta"`
			} `json:"choices"`
			Usage map[string]any `json:"usage"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &frame); err != nil {
			t.Fatal(err)
		}
		for _, choice := range frame.Choices {
			for _, call := range choice.Delta.ToolCalls {
				toolFrames++
				arguments.WriteString(call.Function.Arguments)
				if call.Function.Name == "run_code" {
					unblock()
				}
			}
		}
		if frame.Usage != nil {
			usage = frame.Usage
		}
	}
	if err := scanner.Err(); err != nil {
		t.Fatalf("first tool fragment was buffered until completion: %v", err)
	}
	if toolFrames != 2 || arguments.String() != `{"code":"console.log(1)"}` {
		t.Fatalf("tool arguments duplicated or lost: frames=%d arguments=%q", toolFrames, arguments.String())
	}
	if usage["prompt_tokens"] != float64(1200) || usage["completion_tokens"] != float64(40) {
		t.Fatalf("request counts lost: %v", usage)
	}
	occupancy, _ := usage["context_window"].(map[string]any)
	if occupancy["used_tokens"] != float64(1240) || occupancy["limit_tokens"] != float64(200000) || occupancy["source"] != "estimated" {
		t.Fatalf("missing context or cache counted twice: %v", occupancy)
	}
}

func TestVerifySpeedRequiresMultipleMeasuredFragments(t *testing.T) {
	start := time.Now()
	result := &service.ChatResult{InputTokens: 1200, OutputTokens: 400, CachedTokens: 1000,
		StartTime: start.Add(-time.Second), FirstTokenTime: start, EndTime: start.Add(2 * time.Second), StreamedTokenEvents: 3}
	usage := openAIUsagePayload(result)
	if usage["output_speed_tokens_per_second"] != float64(200) {
		t.Fatalf("wrong speed units: %v", usage)
	}
	if result.OutputSpeed != 0 {
		t.Fatal("usage serialization mutated the result")
	}
	result.StreamedTokenEvents = 1
	result.OutputSpeed = 164543
	if _, ok := openAIUsagePayload(result)["output_speed_tokens_per_second"]; ok {
		t.Fatal("a single buffered frame cannot measure decoding speed")
	}
}
