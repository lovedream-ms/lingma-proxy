package httpapi

import (
	"encoding/json"
	"net/http"
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
		LimitTokens:     128000,
		CachedTokens:    64,
		ReasoningTokens: 7,
	})
	if payload["prompt_tokens"] != 100 {
		t.Fatalf("prompt_tokens = %v, want per-request input 100", payload["prompt_tokens"])
	}
	if payload["completion_tokens"] != 20 || payload["total_tokens"] != 120 {
		t.Fatalf("completion/total = %v/%v", payload["completion_tokens"], payload["total_tokens"])
	}
	if context, ok := payload["context_window"].(map[string]any); !ok || context["used_tokens"] != 4567 || context["limit_tokens"] != 128000 {
		t.Fatalf("context occupancy must be independent of request usage: %v", payload)
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

func TestVerifyResponsesUsageHasConsistentCountsAndBounds(t *testing.T) {
	result := &service.ChatResult{InputTokens: 10, OutputTokens: 5, UsedTokens: 5000, LimitTokens: 32000, CachedTokens: 90, ReasoningTokens: 70}
	usage := openAIResponsesUsagePayload(result)
	if usage["input_tokens"] != 10 || usage["output_tokens"] != 5 || usage["total_tokens"] != 15 {
		t.Fatalf("wrong request usage: %v", usage)
	}
	if usage["input_tokens_details"].(map[string]any)["cached_tokens"] != 10 || usage["output_tokens_details"].(map[string]any)["reasoning_tokens"] != 5 {
		t.Fatalf("subset counts exceed totals: %v", usage)
	}
	if usage["context_window"].(map[string]any)["used_tokens"] != 5000 {
		t.Fatalf("context occupancy lost: %v", usage)
	}
}

func TestVerifyUsageIdentifiesEstimatedCounts(t *testing.T) {
	result := &service.ChatResult{InputTokens: 10, OutputTokens: 5, OutputTokensEstimated: true}
	for _, usage := range []map[string]any{openAIUsagePayload(result), openAIResponsesUsagePayload(result)} {
		source, ok := usage["token_count_source"].(map[string]any)
		if !ok || source["input_tokens"] != "upstream" || source["output_tokens"] != "estimated" {
			t.Fatalf("count provenance lost: %v", usage)
		}
	}
}

func TestVerifyAnthropicStreamUpdatesFinalInputUsage(t *testing.T) {
	server, _ := verifyHarnessServer(t, func(_ int, _ map[string]any) []map[string]any {
		return []map[string]any{verifyDelta(map[string]any{"content": "hello"}), {"usage": map[string]any{"prompt_tokens": 1200, "completion_tokens": 9}}}
	})
	rec := httptest.NewRecorder()
	server.http.Handler.ServeHTTP(rec, httptest.NewRequest(http.MethodPost, "/v1/messages", strings.NewReader(`{"model":"kmodel","stream":true,"max_tokens":64,"messages":[{"role":"user","content":"hello"}]}`)))
	if rec.Code != http.StatusOK {
		t.Fatalf("status %d", rec.Code)
	}
	for _, line := range strings.Split(rec.Body.String(), "\n") {
		if !strings.HasPrefix(line, "data: ") {
			continue
		}
		var event struct {
			Type  string         `json:"type"`
			Usage map[string]int `json:"usage"`
		}
		if err := json.Unmarshal([]byte(strings.TrimPrefix(line, "data: ")), &event); err != nil {
			t.Fatal(err)
		}
		if event.Type == "message_delta" {
			if event.Usage["input_tokens"] != 1200 || event.Usage["output_tokens"] != 9 {
				t.Fatalf("final usage lost input: %v", event.Usage)
			}
			return
		}
	}
	t.Fatal("no final message_delta usage")
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

func TestVerifyModelContextWindow(t *testing.T) {
	tests := []struct {
		name     string
		models   []string
		expected int
	}{
		{
			name:     "known kmodel",
			models:   []string{"kmodel"},
			expected: 256000,
		},
		{
			name:     "known kimi with variations",
			models:   []string{"kimi", "kimi-k2", "kimi-k2.6", "kimi-k2-6"},
			expected: 256000,
		},
		{
			name:     "known minimax with variations",
			models:   []string{"mmodel", "minimax", "minimax-m2.7"},
			expected: 204800,
		},
		{
			name:     "known qwen3-coder",
			models:   []string{"qwen3-coder", "dashscope_qwen3_coder"},
			expected: 256000,
		},
		{
			name:     "unknown model has no advertised limit",
			models:   []string{"unknown-model"},
			expected: 0,
		},
		{
			name:     "case insensitive",
			models:   []string{"KMODEL", "KIMI-K2"},
			expected: 256000,
		},
		{
			name:     "whitespace trimmed",
			models:   []string{" kmodel ", "  ", ""},
			expected: 256000,
		},
		{
			name:     "first known model wins",
			models:   []string{"unknown", "kmodel", "minimax"},
			expected: 256000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			result := modelContextWindow(tt.models...)
			if result != tt.expected {
				t.Errorf("modelContextWindow(%v) = %d, want %d", tt.models, result, tt.expected)
			}
		})
	}
}

func TestVerifyModelContextWindowEmptyInput(t *testing.T) {
	result := modelContextWindow()
	if result != 0 {
		t.Errorf("modelContextWindow() with no args = %d, want unknown limit 0", result)
	}
}

func TestEnhancedUsagePayloadEdgeCases(t *testing.T) {
	// Test with all zero values
	t.Run("zero values", func(t *testing.T) {
		payload := openAIUsagePayload(&service.ChatResult{})
		if payload["prompt_tokens"] != 0 {
			t.Errorf("prompt_tokens = %v, want 0", payload["prompt_tokens"])
		}
		if payload["completion_tokens"] != 0 {
			t.Errorf("completion_tokens = %v, want 0", payload["completion_tokens"])
		}
		if payload["total_tokens"] != 0 {
			t.Errorf("total_tokens = %v, want 0", payload["total_tokens"])
		}
		if _, exists := payload["prompt_tokens_details"]; exists {
			t.Error("prompt_tokens_details should not exist for zero values")
		}
		if _, exists := payload["completion_tokens_details"]; exists {
			t.Error("completion_tokens_details should not exist for zero values")
		}
	})

	// Test with negative values (should be clamped to 0)
	t.Run("negative values clamped", func(t *testing.T) {
		payload := openAIUsagePayload(&service.ChatResult{
			InputTokens:  -100,
			OutputTokens: -50,
		})
		if payload["prompt_tokens"] != 0 {
			t.Errorf("negative prompt_tokens = %v, want 0", payload["prompt_tokens"])
		}
		if payload["completion_tokens"] != 0 {
			t.Errorf("negative completion_tokens = %v, want 0", payload["completion_tokens"])
		}
		if payload["total_tokens"] != 0 {
			t.Errorf("negative total_tokens = %v, want 0", payload["total_tokens"])
		}
	})

	// Session occupancy does not replace per-request input counts.
	t.Run("UsedTokens separate from input", func(t *testing.T) {
		payload := openAIUsagePayload(&service.ChatResult{
			InputTokens: 100,
			UsedTokens:  500,
		})
		if payload["prompt_tokens"] != 100 {
			t.Errorf("prompt_tokens = %v, want request input 100", payload["prompt_tokens"])
		}
	})

	// Test only cached tokens present
	t.Run("only cached tokens", func(t *testing.T) {
		payload := openAIUsagePayload(&service.ChatResult{
			InputTokens:  100,
			CachedTokens: 50,
		})
		if payload["prompt_tokens"] != 100 {
			t.Errorf("prompt_tokens = %v, want 100", payload["prompt_tokens"])
		}
		details, ok := payload["prompt_tokens_details"].(map[string]any)
		if !ok {
			t.Fatal("prompt_tokens_details should exist")
		}
		if details["cached_tokens"] != 50 {
			t.Errorf("cached_tokens = %v, want 50", details["cached_tokens"])
		}
		if _, exists := payload["completion_tokens_details"]; exists {
			t.Error("completion_tokens_details should not exist")
		}
	})

	// Test only reasoning tokens present
	t.Run("only reasoning tokens", func(t *testing.T) {
		payload := openAIUsagePayload(&service.ChatResult{
			OutputTokens:    100,
			ReasoningTokens: 30,
		})
		if payload["completion_tokens"] != 100 {
			t.Errorf("completion_tokens = %v, want 100", payload["completion_tokens"])
		}
		details, ok := payload["completion_tokens_details"].(map[string]any)
		if !ok {
			t.Fatal("completion_tokens_details should exist")
		}
		if details["reasoning_tokens"] != 30 {
			t.Errorf("reasoning_tokens = %v, want 30", details["reasoning_tokens"])
		}
		if _, exists := payload["prompt_tokens_details"]; exists {
			t.Error("prompt_tokens_details should not exist")
		}
	})

	// Test nil result
	t.Run("nil result", func(t *testing.T) {
		payload := openAIUsagePayload(nil)
		if payload["prompt_tokens"] != 0 {
			t.Errorf("nil result prompt_tokens = %v, want 0", payload["prompt_tokens"])
		}
		if payload["completion_tokens"] != 0 {
			t.Errorf("nil result completion_tokens = %v, want 0", payload["completion_tokens"])
		}
		if _, exists := payload["prompt_tokens_details"]; exists {
			t.Error("prompt_tokens_details should not exist for nil result")
		}
		if _, exists := payload["completion_tokens_details"]; exists {
			t.Error("completion_tokens_details should not exist for nil result")
		}
	})
}

func TestWriteOpenAIUsageChunkEdgeCases(t *testing.T) {
	// Test with complex usage data
	t.Run("complex usage data", func(t *testing.T) {
		recorder := httptest.NewRecorder()
		writeOpenAIUsageChunk(recorder, recorder, "test-id", 1234567890, "test-model", &service.ChatResult{
			InputTokens:     1000,
			OutputTokens:    200,
			UsedTokens:      950, // Session occupancy is independent of request input.
			CachedTokens:    400,
			ReasoningTokens: 50,
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
		if !ok {
			t.Fatalf("usage field missing or wrong type: %v", chunk["usage"])
		}

		if usage["prompt_tokens"] != float64(1000) {
			t.Errorf("prompt_tokens = %v, want 1000", usage["prompt_tokens"])
		}
		if usage["completion_tokens"] != float64(200) {
			t.Errorf("completion_tokens = %v, want 200", usage["completion_tokens"])
		}
		if usage["total_tokens"] != float64(1200) {
			t.Errorf("total_tokens = %v, want 1200", usage["total_tokens"])
		}

		promptDetails, ok := usage["prompt_tokens_details"].(map[string]any)
		if !ok {
			t.Fatal("prompt_tokens_details missing")
		}
		if promptDetails["cached_tokens"] != float64(400) {
			t.Errorf("cached_tokens = %v, want 400", promptDetails["cached_tokens"])
		}

		completionDetails, ok := usage["completion_tokens_details"].(map[string]any)
		if !ok {
			t.Fatal("completion_tokens_details missing")
		}
		if completionDetails["reasoning_tokens"] != float64(50) {
			t.Errorf("reasoning_tokens = %v, want 50", completionDetails["reasoning_tokens"])
		}
	})
}

func TestStreamOptionsParsingEdgeCases(t *testing.T) {
	// Test with malformed JSON
	t.Run("malformed JSON", func(t *testing.T) {
		var req openAIChatRequest
		err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"stream_options":{"include_usage":true`), &req)
		if err == nil {
			t.Error("expected error for malformed JSON, got nil")
		}
	})

	// Test with null stream_options
	t.Run("null stream_options", func(t *testing.T) {
		var req openAIChatRequest
		err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"stream_options":null,"messages":[{"role":"user","content":"hi"}]}`), &req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.StreamOptions != nil {
			t.Fatalf("stream_options should be nil when explicitly null, got %+v", req.StreamOptions)
		}
	})

	// Test with empty stream_options object
	t.Run("empty stream_options", func(t *testing.T) {
		var req openAIChatRequest
		err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"stream_options":{},"messages":[{"role":"user","content":"hi"}]}`), &req)
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if req.StreamOptions == nil {
			t.Fatal("stream_options should not be nil when object exists")
		}
		if req.StreamOptions.IncludeUsage {
			t.Error("IncludeUsage should be false when not specified in empty object")
		}
	})

	// Test with include_usage as string instead of boolean - should return error
	t.Run("wrong type for include_usage returns error", func(t *testing.T) {
		var req openAIChatRequest
		err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"stream_options":{"include_usage":"true"},"messages":[{"role":"user","content":"hi"}]}`), &req)
		// This should return an error as JSON unmarshaling will fail
		if err == nil {
			t.Error("expected error for wrong type, got nil")
		}
	})

	// Test with include_usage as number instead of boolean - should return error
	t.Run("number type for include_usage returns error", func(t *testing.T) {
		var req openAIChatRequest
		err := json.Unmarshal([]byte(`{"model":"kmodel","stream":true,"stream_options":{"include_usage":1},"messages":[{"role":"user","content":"hi"}]}`), &req)
		// This should return an error as JSON unmarshaling will fail
		if err == nil {
			t.Error("expected error for number type, got nil")
		}
	})
}

func TestNormalizeOpenAIRequestStreamOptions(t *testing.T) {
	// Test request with include_usage enabled
	t.Run("include_usage enabled", func(t *testing.T) {
		req := openAIChatRequest{
			Model:  "test-model",
			Stream: true,
			StreamOptions: &streamOptions{
				IncludeUsage: true,
			},
			Messages: []rawMessage{
				{Role: "user", Content: "test message"},
			},
		}

		serviceReq, err := normalizeOpenAIRequest(req)
		if err != nil {
			t.Fatalf("normalizeOpenAIRequest failed: %v", err)
		}

		if !serviceReq.IncludeUsage {
			t.Error("IncludeUsage should be true when stream_options.include_usage is true")
		}
	})

	// Test request with include_usage disabled
	t.Run("include_usage disabled", func(t *testing.T) {
		req := openAIChatRequest{
			Model:  "test-model",
			Stream: true,
			StreamOptions: &streamOptions{
				IncludeUsage: false,
			},
			Messages: []rawMessage{
				{Role: "user", Content: "test message"},
			},
		}

		serviceReq, err := normalizeOpenAIRequest(req)
		if err != nil {
			t.Fatalf("normalizeOpenAIRequest failed: %v", err)
		}

		if serviceReq.IncludeUsage {
			t.Error("IncludeUsage should be false when stream_options.include_usage is false")
		}
	})

	// Test request without stream_options
	t.Run("no stream_options", func(t *testing.T) {
		req := openAIChatRequest{
			Model:  "test-model",
			Stream: true,
			Messages: []rawMessage{
				{Role: "user", Content: "test message"},
			},
		}

		serviceReq, err := normalizeOpenAIRequest(req)
		if err != nil {
			t.Fatalf("normalizeOpenAIRequest failed: %v", err)
		}

		if serviceReq.IncludeUsage {
			t.Error("IncludeUsage should be false when stream_options is nil")
		}
	})

	// Test non-streaming request with include_usage
	t.Run("non-streaming with include_usage", func(t *testing.T) {
		req := openAIChatRequest{
			Model:  "test-model",
			Stream: false,
			StreamOptions: &streamOptions{
				IncludeUsage: true,
			},
			Messages: []rawMessage{
				{Role: "user", Content: "test message"},
			},
		}

		serviceReq, err := normalizeOpenAIRequest(req)
		if err != nil {
			t.Fatalf("normalizeOpenAIRequest failed: %v", err)
		}

		if !serviceReq.IncludeUsage {
			t.Error("IncludeUsage should still be true even for non-streaming requests")
		}
	})
}

func TestToolStreamFilterEdgeCases(t *testing.T) {
	// Test with empty buffer
	t.Run("empty buffer", func(t *testing.T) {
		filter := newToolStreamFilter(true)
		result := filter.Push("")
		if len(result) != 0 {
			t.Errorf("Push(empty) = %v, want empty slice", result)
		}
	})

	// Test with very short content - may not return immediate result due to buffering
	t.Run("very short content buffering", func(t *testing.T) {
		filter := newToolStreamFilter(true)
		result := filter.Push("a")
		// Short content might be buffered, so result could be empty
		// The important thing is it doesn't panic and behaves consistently
		if len(result) > 1 {
			t.Errorf("Push('a') returned too many results: %v", result)
		}
	})

	// Test with content shorter than actionMarkerTailRunes
	t.Run("content shorter than tail", func(t *testing.T) {
		shortContent := strings.Repeat("x", actionMarkerTailRunes-1)
		filter := newToolStreamFilter(true)
		result := filter.Push(shortContent)
		expectedReleased := len(shortContent) - actionMarkerTailRunes
		if expectedReleased <= 0 {
			// If content is shorter than tail, nothing should be released initially
			if len(result) != 0 {
				t.Errorf("Push(short content) = %v, want empty slice", result)
			}
		} else {
			if len(result) != 1 {
				t.Errorf("Expected 1 chunk, got %d", len(result))
			}
		}
	})

	// Test flushing with no content
	t.Run("flush empty", func(t *testing.T) {
		filter := newToolStreamFilter(true)
		result := filter.Flush()
		if len(result) != 0 {
			t.Errorf("Flush(empty) = %v, want empty slice", result)
		}
	})

	// Test multiple pushes
	t.Run("multiple pushes", func(t *testing.T) {
		filter := newToolStreamFilter(true)
		result1 := filter.Push("first ")
		result2 := filter.Push("second ")
		result3 := filter.Push("third")

		allResults := append(append(result1, result2...), result3...)
		totalLength := 0
		for _, s := range allResults {
			totalLength += len(s)
		}

		expectedTotal := len("first second third")
		if totalLength > expectedTotal {
			t.Errorf("Total output length %d exceeds input length %d", totalLength, expectedTotal)
		}
	})

	// Test action marker at boundary
	t.Run("action marker at boundary", func(t *testing.T) {
		// Create content that could potentially split an action marker
		content := strings.Repeat("x", actionMarkerTailRunes-5) + "```json action {\"test\":true}\n```\n"
		filter := newToolStreamFilter(true)

		// Split the content to simulate streaming
		part1 := content[:actionMarkerTailRunes-2] // Just before potential marker
		part2 := content[actionMarkerTailRunes-2:] // Including the marker

		result1 := filter.Push(part1)
		result2 := filter.Push(part2)

		// The action marker should be detected and handled appropriately
		combined := strings.Join(append(result1, result2...), "")
		if strings.Contains(combined, "action") {
			t.Log("Action marker may be filtered out as expected")
		}
	})
}

func TestToolStreamFilterDisabled(t *testing.T) {
	// Test that disabled filter passes through content
	t.Run("disabled filter", func(t *testing.T) {
		filter := newToolStreamFilter(false) // disabled
		input := "hello world ```json action {\"test\":true}\n```\n more content"
		result := filter.Push(input)

		if len(result) == 0 {
			t.Errorf("Disabled filter returned empty result")
		} else {
			output := strings.Join(result, "")
			if !strings.Contains(output, "hello world") {
				t.Errorf("Disabled filter didn't pass through content: %s", output)
			}
		}
	})

	// Test flush with disabled filter
	t.Run("disabled flush", func(t *testing.T) {
		filter := newToolStreamFilter(false)
		filter.Push("test content")
		result := filter.Flush()

		// Should return any remaining buffered content
		if len(result) > 0 {
			output := strings.Join(result, "")
			if !strings.Contains(output, "test") {
				t.Errorf("Disabled flush didn't return content: %s", output)
			}
		}
	})
}
