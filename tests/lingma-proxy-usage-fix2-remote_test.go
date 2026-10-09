package remote

import (
	"encoding/json"
	"testing"
)

func TestVerifyParseSSEPayloadReadsUsageAndReasoning(t *testing.T) {
	body := "{\"choices\":[{\"delta\":{\"content\":\"hi\",\"reasoning_content\":\"think\"}}]," +
		"\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":22," +
		"\"prompt_tokens_details\":{\"cached_tokens\":3}," +
		"\"completion_tokens_details\":{\"reasoning_tokens\":4}}}"
	payload := "{\"statusCodeValue\":200,\"body\":" + quoteJSON(body) + "}"
	event, ok, err := parseSSEPayload(payload)
	if err != nil || !ok {
		t.Fatalf("parseSSEPayload ok=%v err=%v", ok, err)
	}
	if event.Content != "hi" || event.Reasoning != "think" {
		t.Fatalf("content=%q reasoning=%q", event.Content, event.Reasoning)
	}
	if event.Usage == nil {
		t.Fatal("usage was dropped")
	}
	if event.Usage.PromptTokens != 11 || event.Usage.CompletionTokens != 22 {
		t.Fatalf("prompt/completion = %d/%d", event.Usage.PromptTokens, event.Usage.CompletionTokens)
	}
	if got := event.Usage.cacheReadTokens(); got != 3 {
		t.Fatalf("cacheReadTokens = %d, want 3", got)
	}
	if got := event.Usage.reasoningTokens(); got != 4 {
		t.Fatalf("reasoningTokens = %d, want 4", got)
	}
}

func TestVerifyBuildChatResultPrefersUpstreamUsage(t *testing.T) {
	usage := parseUsageForTest(t)
	result := buildChatResult(ChatRequest{Prompt: "你好世界"}, "hello world", "thinking", usage, "req-1", "test", nil)
	if result.InputTokens != 11 || result.OutputTokens != 22 {
		t.Fatalf("upstream counts ignored: %+v", result)
	}
	if result.CachedTokens != 3 || result.ReasoningTokens != 4 {
		t.Fatalf("details ignored: %+v", result)
	}
	estimated := buildChatResult(ChatRequest{Prompt: "你好世界"}, "hello world", "reasoning text here", nil, "req-2", "test", nil)
	if estimated.InputTokens <= 0 || estimated.OutputTokens <= 0 || estimated.ReasoningTokens <= 0 {
		t.Fatalf("estimate fallback missing: %+v", estimated)
	}
}

func TestVerifyEstimateTokensIsCJKAware(t *testing.T) {
	cjk := estimateTokens(repeatRune('你', 100))
	ascii := estimateTokens(repeatRune('a', 100))
	if cjk < 60 {
		t.Fatalf("100 Chinese chars estimated at only %d tokens", cjk)
	}
	if ascii > 40 {
		t.Fatalf("100 ASCII chars estimated at %d tokens", ascii)
	}
	if estimateTokens("  ") != 0 {
		t.Fatal("blank text must estimate to zero")
	}
	if estimateTokensOrZero("") != 0 {
		t.Fatal("empty text must estimate to zero")
	}
}

func quoteJSON(value string) string {
	quoted, err := json.Marshal(value)
	if err != nil {
		panic(err)
	}
	return string(quoted)
}

func parseUsageForTest(t *testing.T) *remoteUsage {
	t.Helper()
	body := "{\"choices\":[{\"delta\":{\"content\":\"hi\"}}]," +
		"\"usage\":{\"prompt_tokens\":11,\"completion_tokens\":22," +
		"\"prompt_tokens_details\":{\"cached_tokens\":3}," +
		"\"completion_tokens_details\":{\"reasoning_tokens\":4}}}"
	payload := "{\"statusCodeValue\":200,\"body\":" + quoteJSON(body) + "}"
	event, ok, err := parseSSEPayload(payload)
	if err != nil || !ok || event.Usage == nil {
		t.Fatalf("usage fixture failed: ok=%v err=%v", ok, err)
	}
	return event.Usage
}

func repeatRune(r rune, count int) string {
	out := make([]rune, 0, count)
	for i := 0; i < count; i++ {
		out = append(out, r)
	}
	return string(out)
}
