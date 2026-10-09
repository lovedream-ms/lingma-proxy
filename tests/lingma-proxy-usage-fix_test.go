package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"lingma-ipc-proxy/internal/service"
)

// Temporary local verification of the usage-frame patch (not part of upstream).
func TestVerifyUsageChunkEmitsEmptyChoicesAndUsage(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIUsageChunk(rec, rec, "chatcmpl-verify", 1700000000, "kmodel", &service.ChatResult{InputTokens: 1000, OutputTokens: 250})
	body := rec.Body.String()
	t.Logf("emitted SSE chunk: %q", body)
	if !strings.HasPrefix(body, "data: ") || !strings.HasSuffix(body, "\n\n") {
		t.Fatalf("bad SSE framing: %q", body)
	}
	payload := strings.TrimSuffix(strings.TrimPrefix(body, "data: "), "\n\n")
	var got struct {
		Choices []any `json:"choices"`
		Usage   struct {
			PromptTokens     int `json:"prompt_tokens"`
			CompletionTokens int `json:"completion_tokens"`
			TotalTokens      int `json:"total_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal([]byte(payload), &got); err != nil {
		t.Fatalf("payload is not valid JSON: %v (%q)", err, payload)
	}
	if got.Choices == nil || len(got.Choices) != 0 {
		t.Fatalf("choices must be present and empty (OpenAI usage-frame shape), got %v", got.Choices)
	}
	if got.Usage.PromptTokens != 1000 || got.Usage.CompletionTokens != 250 || got.Usage.TotalTokens != 1250 {
		t.Fatalf("unexpected usage numbers: %+v", got.Usage)
	}
}

func TestVerifyUsageChunkFallsBackToUsedTokens(t *testing.T) {
	rec := httptest.NewRecorder()
	writeOpenAIUsageChunk(rec, rec, "id", 1, "kmodel", &service.ChatResult{UsedTokens: 777, OutputTokens: 3})
	if !strings.Contains(rec.Body.String(), `"prompt_tokens":777`) {
		t.Fatalf("expected fallback to UsedTokens, got %q", rec.Body.String())
	}
}

func TestVerifyStreamOptionsParsing(t *testing.T) {
	var on openAIChatRequest
	if err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}`), &on); err != nil {
		t.Fatal(err)
	}
	if on.StreamOptions == nil || !on.StreamOptions.IncludeUsage {
		t.Fatalf("stream_options.include_usage not parsed: %+v", on.StreamOptions)
	}
	var off openAIChatRequest
	if err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"messages":[{"role":"user","content":"hi"}]}`), &off); err != nil {
		t.Fatal(err)
	}
	if off.StreamOptions != nil {
		t.Fatalf("expected nil StreamOptions when the field is absent, got %+v", off.StreamOptions)
	}
}
