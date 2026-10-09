package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"strings"
	"testing"

	"lingma-ipc-proxy/internal/service"
)

func TestVerifyUsagePayloadDetails(t *testing.T) {
	payload := openAIUsagePayload(&service.ChatResult{
		InputTokens:     100,
		OutputTokens:    20,
		UsedTokens:      4567,
		CachedTokens:    64,
		ReasoningTokens: 7,
	})
	if payload["prompt_tokens"] != 4567 {
		t.Fatalf("prompt_tokens = %v, want real context usage 4567", payload["prompt_tokens"])
	}
	if payload["completion_tokens"] != 20 || payload["total_tokens"] != 4587 {
		t.Fatalf("completion/total = %v/%v", payload["completion_tokens"], payload["total_tokens"])
	}
	promptDetails, ok := payload["prompt_tokens_details"].(map[string]any)
	if !ok || promptDetails["cached_tokens"] != 64 {
		t.Fatalf("prompt_tokens_details = %v, want cached_tokens 64", payload["prompt_tokens_details"])
	}
	completionDetails, ok := payload["completion_tokens_details"].(map[string]any)
	if !ok || completionDetails["reasoning_tokens"] != 7 {
		t.Fatalf("completion_tokens_details = %v, want reasoning_tokens 7", payload["completion_tokens_details"])
	}
}

func TestVerifyUsagePayloadOmitsUnknownDetails(t *testing.T) {
	payload := openAIUsagePayload(&service.ChatResult{InputTokens: 10, OutputTokens: 2})
	if _, ok := payload["prompt_tokens_details"]; ok {
		t.Fatalf("unknown cache details must not be advertised: %v", payload)
	}
	if _, ok := payload["completion_tokens_details"]; ok {
		t.Fatalf("unknown reasoning details must not be advertised: %v", payload)
	}
}

func TestVerifyUsageChunkShape(t *testing.T) {
	recorder := httptest.NewRecorder()
	writeOpenAIUsageChunk(recorder, recorder, "chatcmpl-verify", 1700000000, "kmodel", &service.ChatResult{
		InputTokens:     1000,
		OutputTokens:    250,
		CachedTokens:    400,
		ReasoningTokens: 12,
	})
	body := strings.TrimSpace(recorder.Body.String())
	if !strings.HasPrefix(body, "data: ") {
		t.Fatalf("not an SSE frame: %q", body)
	}
	var chunk map[string]any
	if err := json.Unmarshal([]byte(strings.TrimPrefix(body, "data: ")), &chunk); err != nil {
		t.Fatalf("unmarshal frame: %v", err)
	}
	choices, ok := chunk["choices"].([]any)
	if !ok || len(choices) != 0 {
		t.Fatalf("choices = %v, want empty array", chunk["choices"])
	}
	usage, ok := chunk["usage"].(map[string]any)
	if !ok || usage["prompt_tokens"] != float64(1000) || usage["completion_tokens"] != float64(250) {
		t.Fatalf("usage = %v", chunk["usage"])
	}
	t.Logf("frame: %s", body)
}

func TestVerifyToolStreamFilterReleasesFirstChunkEarly(t *testing.T) {
	filter := newToolStreamFilter(true)
	first := filter.Push(strings.Repeat("a", 20))
	released := len([]rune(strings.Join(first, "")))
	if released != 20-actionMarkerTailRunes {
		t.Fatalf("released %d runes of a 20 rune first chunk, want %d", released, 20-actionMarkerTailRunes)
	}
	if released <= 0 {
		t.Fatalf("first chunk still fully buffered, tail = %d", actionMarkerTailRunes)
	}
	marker := filter.Push("\n" + "\u0060\u0060\u0060" + "json action {\"name\":\"run_code\"}\n" + "\u0060\u0060\u0060" + "\n")
	joined := strings.Join(append(append([]string{}, first...), marker...), "")
	if strings.Contains(joined, "action") {
		t.Fatalf("action marker leaked to the client: %q", joined)
	}
	if flushed := filter.Flush(); len(flushed) != 0 {
		t.Fatalf("blocked filter must not flush content: %v", flushed)
	}
}
