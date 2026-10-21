package remote

import (
	"encoding/json"
	"strings"
	"testing"

	"lingma-ipc-proxy/internal/toolemulation"
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
	if result.InputTokensEstimated || result.OutputTokensEstimated {
		t.Fatal("upstream counts marked as estimates")
	}
	if result.CachedTokens != 3 || result.ReasoningTokens != 4 {
		t.Fatalf("details ignored: %+v", result)
	}
	estimated := buildChatResult(ChatRequest{Prompt: "你好世界"}, "hello world", "reasoning text here", nil, "req-2", "test", nil)
	if estimated.InputTokens <= 0 || estimated.OutputTokens <= 0 || estimated.ReasoningTokens <= 0 {
		t.Fatalf("estimate fallback missing: %+v", estimated)
	}
	if !estimated.InputTokensEstimated || !estimated.OutputTokensEstimated {
		t.Fatal("fallback estimates not identified")
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

func TestVerifyUsageSnapshotsPreserveZeroAndPartialCounts(t *testing.T) {
	var earlier, later remoteUsage
	if err := json.Unmarshal([]byte(`{"input_tokens":1200,"input_tokens_details":{"cached_tokens":800}}`), &earlier); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"output_tokens":0,"output_tokens_details":{"reasoning_tokens":0}}`), &later); err != nil {
		t.Fatal(err)
	}
	merged := mergeRemoteUsage(&earlier, &later)
	result := buildChatResult(ChatRequest{Prompt: "fallback prompt"}, "ignored estimate", "", merged, "", "", nil)
	if result.InputTokens != 1200 || result.OutputTokens != 0 || result.CachedTokens != 800 {
		t.Fatalf("partial/zero usage lost: %+v", result)
	}
	result = buildChatResult(ChatRequest{}, "", "", mergeRemoteUsage(merged, &later), "", "", nil)
	if result.InputTokens != 1200 || result.OutputTokens != 0 {
		t.Fatalf("snapshot added twice: %+v", result)
	}
}

func TestVerifyUsageDetailZerosAndSplitFrame(t *testing.T) {
	var earlier, zero remoteUsage
	if err := json.Unmarshal([]byte(`{"prompt_tokens":100,"completion_tokens":20,"cached_tokens":80}`), &earlier); err != nil {
		t.Fatal(err)
	}
	if err := json.Unmarshal([]byte(`{"cached_tokens":0,"completion_tokens_details":{"reasoning_tokens":0}}`), &zero); err != nil {
		t.Fatal(err)
	}
	result := buildChatResult(ChatRequest{}, "", "thought text", mergeRemoteUsage(&earlier, &zero), "", "", nil)
	if result.CachedTokens != 0 || result.ReasoningTokens != 0 {
		t.Fatalf("upstream zero details lost: %+v", result)
	}
	if err := json.Unmarshal([]byte(`{"prompt_tokens":100,"prompt_tokens_details":{"cached_tokens":80}}`), &earlier); err != nil {
		t.Fatal(err)
	}
	if mergeRemoteUsage(&earlier, &zero).cacheReadTokens() != 0 {
		t.Fatal("top-level explicit zero fell back to stale nested cache count")
	}
	if err := json.Unmarshal([]byte(`{"prompt_tokens_details":{"cached_tokens":0}}`), &zero); err != nil {
		t.Fatal(err)
	}
	if mergeRemoteUsage(&earlier, &zero).cacheReadTokens() != 0 {
		t.Fatal("nested explicit zero fell back to stale top-level cache count")
	}
	inner := `{"usage":{"prompt_tokens":1200},"choices":[{"usage":{"completion_tokens":9},"delta":{"content":"hello"}}]}`
	event, ok, err := parseSSEPayload(`{"statusCodeValue":200,"body":` + quoteJSON(inner) + `}`)
	if err != nil || !ok || event.Usage == nil || event.Usage.PromptTokens != 1200 || event.Usage.CompletionTokens != 9 {
		t.Fatalf("partial root/choice usage lost: %+v, %v", event, err)
	}
}

func TestVerifyStandardSSEUsageFrame(t *testing.T) {
	frame := `{"choices":[{"delta":{"content":"hello"}}],"usage":{"prompt_tokens":12,"completion_tokens":5}}`
	event, ok, err := parseSSEPayload(frame)
	if err != nil || !ok || event.Content != "hello" || event.Usage == nil || event.Usage.PromptTokens != 12 || event.Usage.CompletionTokens != 5 {
		t.Fatalf("standard gateway frame lost: %+v, %v", event, err)
	}
	event, ok, err = parseSSEPayload(`{"choices":[],"usage":{"input_tokens":12,"output_tokens":0}}`)
	if err != nil || !ok || event.Usage == nil || !event.Usage.completionPresent {
		t.Fatalf("standard usage-only zero lost: %+v, %v", event, err)
	}
	if _, _, err := parseSSEPayload(`{"error":{"message":"invalid credentials"}}`); err == nil {
		t.Fatal("gateway error was accepted as an empty successful completion")
	}
}

func TestVerifyOutputEstimatesIncludeToolsAndReasoning(t *testing.T) {
	toolPayload := parseUsageForTest(t)
	toolPayload.CompletionTokens = 10
	toolPayload.CompletionTokensDetails.ReasoningTokens = 100
	toolPayload.PromptTokensDetails.CachedTokens = 1000
	result := buildChatResult(ChatRequest{}, "", "", toolPayload, "", "", nil)
	if result.CachedTokens > result.InputTokens || result.ReasoningTokens > result.OutputTokens {
		t.Fatalf("invalid subset ranges: %+v", result)
	}
	result = buildChatResult(ChatRequest{}, "", strings.Repeat("reason ", 100), nil, "", "", nil)
	if result.OutputTokens <= 1 || result.OutputTokens < result.ReasoningTokens {
		t.Fatalf("reasoning excluded from completion: %+v", result)
	}
	result = buildChatResult(ChatRequest{}, "", "", nil, "", "", nil)
	if result.InputTokens != 0 || result.OutputTokens != 0 {
		t.Fatalf("empty output fabricated tokens: %+v", result)
	}
	result = buildChatResult(ChatRequest{}, "", "", nil, "", "", []toolemulation.ToolCall{{Name: "run_code", Arguments: map[string]any{"code": strings.Repeat("x", 400)}}})
	if result.OutputTokens < 100 {
		t.Fatalf("tool-only output estimated as %d tokens", result.OutputTokens)
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

func TestRemoteUsageParsingEdgeCases(t *testing.T) {
	// Test with nil usage object
	t.Run("nil usage object", func(t *testing.T) {
		var usage *remoteUsage
		if usage.cacheReadTokens() != 0 {
			t.Errorf("nil.cacheReadTokens() = %d, want 0", usage.cacheReadTokens())
		}
		if usage.reasoningTokens() != 0 {
			t.Errorf("nil.reasoningTokens() = %d, want 0", usage.reasoningTokens())
		}
	})

	// Test with zero values
	t.Run("zero values", func(t *testing.T) {
		usage := &remoteUsage{}
		if usage.cacheReadTokens() != 0 {
			t.Errorf("zero.cacheReadTokens() = %d, want 0", usage.cacheReadTokens())
		}
		if usage.reasoningTokens() != 0 {
			t.Errorf("zero.reasoningTokens() = %d, want 0", usage.reasoningTokens())
		}
	})

	// Test with nested prompt_tokens_details
	t.Run("nested prompt_tokens_details", func(t *testing.T) {
		usage := &remoteUsage{
			PromptTokensDetails: &struct {
				CachedTokens int `json:"cached_tokens"`
			}{CachedTokens: 100},
		}
		if usage.cacheReadTokens() != 100 {
			t.Errorf("nested.cacheReadTokens() = %d, want 100", usage.cacheReadTokens())
		}
	})

	// Test with top-level cached_tokens
	t.Run("top-level cached_tokens", func(t *testing.T) {
		usage := &remoteUsage{
			CachedTokens: 200,
		}
		if usage.cacheReadTokens() != 200 {
			t.Errorf("top-level.cacheReadTokens() = %d, want 200", usage.cacheReadTokens())
		}
	})

	// Test priority: nested should take precedence over top-level
	t.Run("nested priority over top-level", func(t *testing.T) {
		usage := &remoteUsage{
			CachedTokens: 200, // top-level
			PromptTokensDetails: &struct {
				CachedTokens int `json:"cached_tokens"`
			}{CachedTokens: 100}, // nested (should win)
		}
		if usage.cacheReadTokens() != 100 {
			t.Errorf("priority.cacheReadTokens() = %d, want 100 (nested should win)", usage.cacheReadTokens())
		}
	})

	// Test reasoning tokens with nil CompletionTokensDetails
	t.Run("nil CompletionTokensDetails", func(t *testing.T) {
		usage := &remoteUsage{
			CompletionTokensDetails: nil,
		}
		if usage.reasoningTokens() != 0 {
			t.Errorf("nil CompletionTokensDetails.reasoningTokens() = %d, want 0", usage.reasoningTokens())
		}
	})

	// Test reasoning tokens with zero value
	t.Run("zero reasoning tokens", func(t *testing.T) {
		usage := &remoteUsage{
			CompletionTokensDetails: &struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 0},
		}
		if usage.reasoningTokens() != 0 {
			t.Errorf("zero reasoningTokens() = %d, want 0", usage.reasoningTokens())
		}
	})

	// Test reasoning tokens with positive value
	t.Run("positive reasoning tokens", func(t *testing.T) {
		usage := &remoteUsage{
			CompletionTokensDetails: &struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 42},
		}
		if usage.reasoningTokens() != 42 {
			t.Errorf("positive reasoningTokens() = %d, want 42", usage.reasoningTokens())
		}
	})
}

func TestParseSSEPayloadUsageEdgeCases(t *testing.T) {
	// Test with usage in choices array instead of root
	t.Run("usage in choices", func(t *testing.T) {
		body := `{"choices":[{"delta":{"content":"test"},"usage":{"prompt_tokens":10,"completion_tokens":5}}]}`
		payload := `{"statusCodeValue":200,"body":` + quoteJSON(body) + `}`
		event, ok, err := parseSSEPayload(payload)
		if err != nil || !ok {
			t.Fatalf("parseSSEPayload failed: ok=%v err=%v", ok, err)
		}
		if event.Usage == nil {
			t.Fatal("usage should be parsed from choices")
		}
		if event.Usage.PromptTokens != 10 {
			t.Errorf("prompt_tokens = %d, want 10", event.Usage.PromptTokens)
		}
		if event.Usage.CompletionTokens != 5 {
			t.Errorf("completion_tokens = %d, want 5", event.Usage.CompletionTokens)
		}
	})

	// Test with usage in both root and choices (root should win)
	t.Run("usage in both root and choices", func(t *testing.T) {
		body := `{"usage":{"prompt_tokens":100,"completion_tokens":50},"choices":[{"delta":{"content":"test"},"usage":{"prompt_tokens":10,"completion_tokens":5}}]}`
		payload := `{"statusCodeValue":200,"body":` + quoteJSON(body) + `}`
		event, ok, err := parseSSEPayload(payload)
		if err != nil || !ok {
			t.Fatalf("parseSSEPayload failed: ok=%v err=%v", ok, err)
		}
		if event.Usage == nil {
			t.Fatal("usage should be parsed")
		}
		// Root usage should take precedence over choices usage
		if event.Usage.PromptTokens != 100 {
			t.Errorf("prompt_tokens = %d, want 100 (root should win)", event.Usage.PromptTokens)
		}
	})

	// Test with malformed usage JSON
	t.Run("malformed usage JSON", func(t *testing.T) {
		body := `{"usage":{"prompt_tokens":10,"completion_tokens":"invalid"}}`
		payload := `{"statusCodeValue":200,"body":` + quoteJSON(body) + `}`
		_, _, err := parseSSEPayload(payload)
		// Should handle gracefully, possibly with zero values
		if err != nil {
			t.Logf("Expected error for malformed usage: %v", err)
		}
	})

	// Test with partial usage fields
	t.Run("partial usage fields", func(t *testing.T) {
		body := `{"usage":{"prompt_tokens":10}}` // Missing completion_tokens
		payload := `{"statusCodeValue":200,"body":` + quoteJSON(body) + `}`
		event, ok, err := parseSSEPayload(payload)
		if err != nil || !ok {
			t.Fatalf("parseSSEPayload failed: ok=%v err=%v", ok, err)
		}
		if event.Usage == nil {
			t.Fatal("usage should be parsed")
		}
		if event.Usage.PromptTokens != 10 {
			t.Errorf("prompt_tokens = %d, want 10", event.Usage.PromptTokens)
		}
		if event.Usage.CompletionTokens != 0 {
			t.Errorf("completion_tokens = %d, want 0 (not provided)", event.Usage.CompletionTokens)
		}
	})
}

func TestBuildChatResultEdgeCases(t *testing.T) {
	// Test with nil usage (should fall back to estimates)
	t.Run("nil usage fallback", func(t *testing.T) {
		request := ChatRequest{Prompt: "test prompt"}
		result := buildChatResult(request, "test response", "test reasoning", nil, "req-1", "test-source", nil)

		if result.InputTokens <= 0 {
			t.Errorf("InputTokens = %d, want positive (fallback to estimate)", result.InputTokens)
		}
		if result.OutputTokens <= 0 {
			t.Errorf("OutputTokens = %d, want positive (fallback to estimate)", result.OutputTokens)
		}
		if result.ReasoningTokens <= 0 {
			t.Errorf("ReasoningTokens = %d, want positive (fallback to estimate)", result.ReasoningTokens)
		}
	})

	// Test with zero usage values (should use estimates)
	t.Run("zero usage values", func(t *testing.T) {
		usage := &remoteUsage{
			PromptTokens:     0,
			CompletionTokens: 0,
		}
		request := ChatRequest{Prompt: "test prompt"}
		result := buildChatResult(request, "test response", "", usage, "req-2", "test-source", nil)

		if result.InputTokens <= 0 {
			t.Errorf("InputTokens = %d, want positive (fallback to estimate when zero)", result.InputTokens)
		}
		if result.OutputTokens <= 0 {
			t.Errorf("OutputTokens = %d, want positive (fallback to estimate when zero)", result.OutputTokens)
		}
	})

	// Test with positive usage values (should use provided values)
	t.Run("positive usage values", func(t *testing.T) {
		usage := &remoteUsage{
			PromptTokens:     100,
			CompletionTokens: 50,
			CachedTokens:     20,
			CompletionTokensDetails: &struct {
				ReasoningTokens int `json:"reasoning_tokens"`
			}{ReasoningTokens: 10},
		}
		request := ChatRequest{Prompt: "test prompt"}
		result := buildChatResult(request, "test response", "test reasoning", usage, "req-3", "test-source", nil)

		if result.InputTokens != 100 {
			t.Errorf("InputTokens = %d, want 100 (from usage)", result.InputTokens)
		}
		if result.OutputTokens != 50 {
			t.Errorf("OutputTokens = %d, want 50 (from usage)", result.OutputTokens)
		}
		if result.CachedTokens != 20 {
			t.Errorf("CachedTokens = %d, want 20 (from usage)", result.CachedTokens)
		}
		if result.ReasoningTokens != 10 {
			t.Errorf("ReasoningTokens = %d, want 10 (from usage)", result.ReasoningTokens)
		}
	})

	// Test reasoning tokens fallback
	t.Run("reasoning tokens fallback", func(t *testing.T) {
		usage := &remoteUsage{
			CompletionTokens:        50,
			CompletionTokensDetails: nil, // No reasoning tokens in usage
		}
		request := ChatRequest{Prompt: "test"}
		result := buildChatResult(request, "response", "some reasoning text", usage, "req-4", "test-source", nil)

		if result.OutputTokens != 50 {
			t.Errorf("OutputTokens = %d, want 50 (from usage)", result.OutputTokens)
		}
		if result.ReasoningTokens <= 0 {
			t.Errorf("ReasoningTokens = %d, want positive (fallback to estimate)", result.ReasoningTokens)
		}
	})

	// Test empty reasoning text fallback
	t.Run("empty reasoning text", func(t *testing.T) {
		usage := &remoteUsage{
			CompletionTokens:        50,
			CompletionTokensDetails: nil,
		}
		request := ChatRequest{Prompt: "test"}
		result := buildChatResult(request, "response", "", usage, "req-5", "test-source", nil)

		if result.ReasoningTokens != 0 {
			t.Errorf("ReasoningTokens = %d, want 0 (empty reasoning text)", result.ReasoningTokens)
		}
	})
}

func TestRemoteUsageJSONParsing(t *testing.T) {
	// Test parsing various JSON structures for usage
	testCases := []struct {
		name     string
		jsonStr  string
		expected func(*remoteUsage)
	}{
		{
			name:    "basic usage",
			jsonStr: `{"usage":{"prompt_tokens":100,"completion_tokens":50,"total_tokens":150}}`,
			expected: func(u *remoteUsage) {
				if u.PromptTokens != 100 || u.CompletionTokens != 50 || u.TotalTokens != 150 {
					t.Errorf("Basic usage parsing failed: %+v", u)
				}
			},
		},
		{
			name:    "usage with cached tokens",
			jsonStr: `{"usage":{"prompt_tokens":100,"completion_tokens":50,"cached_tokens":20}}`,
			expected: func(u *remoteUsage) {
				if u.PromptTokens != 100 || u.CompletionTokens != 50 || u.CachedTokens != 20 {
					t.Errorf("Usage with cached tokens parsing failed: %+v", u)
				}
			},
		},
		{
			name:    "usage with prompt_tokens_details",
			jsonStr: `{"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":30}}}`,
			expected: func(u *remoteUsage) {
				if u.PromptTokens != 100 || u.CompletionTokens != 50 || u.PromptTokensDetails.CachedTokens != 30 {
					t.Errorf("Usage with prompt_tokens_details parsing failed: %+v", u)
				}
			},
		},
		{
			name:    "usage with completion_tokens_details",
			jsonStr: `{"usage":{"prompt_tokens":100,"completion_tokens":50,"completion_tokens_details":{"reasoning_tokens":15}}}`,
			expected: func(u *remoteUsage) {
				if u.PromptTokens != 100 || u.CompletionTokens != 50 || u.CompletionTokensDetails.ReasoningTokens != 15 {
					t.Errorf("Usage with completion_tokens_details parsing failed: %+v", u)
				}
			},
		},
		{
			name:    "usage with both details objects",
			jsonStr: `{"usage":{"prompt_tokens":100,"completion_tokens":50,"prompt_tokens_details":{"cached_tokens":20},"completion_tokens_details":{"reasoning_tokens":15}}}`,
			expected: func(u *remoteUsage) {
				if u.PromptTokens != 100 || u.CompletionTokens != 50 ||
					u.PromptTokensDetails.CachedTokens != 20 || u.CompletionTokensDetails.ReasoningTokens != 15 {
					t.Errorf("Usage with both details parsing failed: %+v", u)
				}
			},
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			var outer struct {
				Usage *remoteUsage `json:"usage"`
			}

			err := json.Unmarshal([]byte(tc.jsonStr), &outer)
			if err != nil {
				t.Fatalf("Failed to unmarshal JSON: %v", err)
			}

			if outer.Usage == nil {
				t.Fatal("Usage object is nil")
			}

			tc.expected(outer.Usage)
		})
	}
}

func TestEstimateTokensEdgeCases(t *testing.T) {
	// Test empty and whitespace-only strings
	t.Run("empty strings", func(t *testing.T) {
		if estimateTokens("") != 0 {
			t.Errorf("estimateTokens(\"\") = %d, want 0", estimateTokens(""))
		}
		if estimateTokens("   ") != 0 {
			t.Errorf("estimateTokens(\"   \") = %d, want 0", estimateTokens("   "))
		}
		if estimateTokens("\t\n\r") != 0 {
			t.Errorf("estimateTokens(whitespace) = %d, want 0", estimateTokens("\t\n\r"))
		}
	})

	// Test single characters
	t.Run("single characters", func(t *testing.T) {
		// Single ASCII character should be 1 token (minimum)
		if estimateTokens("a") < 1 {
			t.Errorf("estimateTokens(\"a\") = %d, want at least 1", estimateTokens("a"))
		}
		// Single CJK character should be ~0.7 tokens, rounded up
		if estimateTokens("你") < 1 {
			t.Errorf("estimateTokens(\"你\") = %d, want at least 1", estimateTokens("你"))
		}
	})

	// Test mixed ASCII and CJK
	t.Run("mixed ASCII and CJK", func(t *testing.T) {
		text := "Hello 世界"
		result := estimateTokens(text)
		// Should be more than pure ASCII but less than pure CJK
		asciiOnly := estimateTokens("Hello ")
		cjkOnly := estimateTokens("世界")
		expectedMin := asciiOnly + cjkOnly/2 // Rough estimate
		expectedMax := asciiOnly*2 + cjkOnly // Upper bound

		if result < expectedMin || result > expectedMax {
			t.Errorf("estimateTokens(\"%s\") = %d, want between %d and %d",
				text, result, expectedMin, expectedMax)
		}
	})

	// Test very long strings
	t.Run("long strings", func(t *testing.T) {
		longASCII := "a"
		for i := 0; i < 1000; i++ {
			longASCII += "bcdef"
		}
		result := estimateTokens(longASCII)
		if result < 1000 { // Should be substantial
			t.Errorf("estimateTokens(long ASCII) = %d, want much higher", result)
		}
	})

	// Test special Unicode characters
	t.Run("special Unicode", func(t *testing.T) {
		specialChars := "αβγδε∑∏π∫∂∇∞≤≥≠≈±°"
		result := estimateTokens(specialChars)
		// Just ensure it produces a reasonable positive count
		if result <= 0 {
			t.Errorf("estimateTokens(special Unicode) = %d, want positive count", result)
		}
	})

	// Test emoji and symbols
	t.Run("emoji and symbols", func(t *testing.T) {
		emojis := "😀😃😄😁😆😅😂🤣"
		result := estimateTokens(emojis)
		// Just ensure it produces a reasonable positive count
		if result <= 0 {
			t.Errorf("estimateTokens(emojis) = %d, want positive count", result)
		}
	})
}

func TestEstimateTokensOrZeroEdgeCases(t *testing.T) {
	// Test that estimateTokensOrZero returns 0 for empty strings
	t.Run("returns zero for empty", func(t *testing.T) {
		if estimateTokensOrZero("") != 0 {
			t.Errorf("estimateTokensOrZero(\"\") = %d, want 0", estimateTokensOrZero(""))
		}
		if estimateTokensOrZero("   ") != 0 {
			t.Errorf("estimateTokensOrZero(\"   \") = %d, want 0", estimateTokensOrZero("   "))
		}
		if estimateTokensOrZero("\t\n\r") != 0 {
			t.Errorf("estimateTokensOrZero(whitespace) = %d, want 0", estimateTokensOrZero("\t\n\r"))
		}
	})

	// Test that it returns positive for non-empty
	t.Run("returns positive for non-empty", func(t *testing.T) {
		if estimateTokensOrZero("a") <= 0 {
			t.Errorf("estimateTokensOrZero(\"a\") = %d, want positive", estimateTokensOrZero("a"))
		}
		if estimateTokensOrZero("你好") <= 0 {
			t.Errorf("estimateTokensOrZero(\"你好\") = %d, want positive", estimateTokensOrZero("你好"))
		}
	})

	// Compare with estimateTokens behavior
	t.Run("compares with estimateTokens", func(t *testing.T) {
		testCases := []string{
			"",
			"   ",
			"\t\n\r",
			"a",
			"hello",
			"你好世界",
		}

		for _, tc := range testCases {
			regular := estimateTokens(tc)
			zeroAware := estimateTokensOrZero(tc)

			if tc == "" || len(tc) == 0 || len([]rune(strings.TrimSpace(tc))) == 0 {
				if zeroAware != 0 {
					t.Errorf("estimateTokensOrZero(\"\") = %d, want 0", zeroAware)
				}
				// For empty strings, regular estimateTokens might return 1 due to minimum
			} else {
				if zeroAware <= 0 {
					t.Errorf("estimateTokensOrZero(non-empty) = %d, want positive", zeroAware)
				}
				// Both should return positive for non-empty
				if regular <= 0 {
					t.Errorf("estimateTokens(non-empty) = %d, want positive", regular)
				}
			}
		}
	})
}

func TestIsWideRuneEdgeCases(t *testing.T) {
	// Test boundary values
	boundaryTests := []struct {
		rune     rune
		expected bool
		desc     string
	}{
		{0x10FF, false, "just before 0x1100"},
		{0x1100, true, "start of Hangul Jamo"},
		{0x11FF, true, "end of Hangul Jamo"},
		{0x1200, false, "after Hangul Jamo"},
		{0x2E7F, false, "just before CJK Extension A"},
		{0x2E80, true, "start of CJK Radicals Supplement"},
		{0x303F, true, "end of CJK Symbols and Punctuation"},
		{0x3040, true, "start of Hiragana"},
		{0x30FF, true, "end of Katakana"},
		{0x3100, false, "after Katakana"},
		{0x312F, false, "just before Bopomofo"},
		{0x3130, true, "start of Hangul Compatibility Jamo"},
		{0x318F, true, "end of Hangul Compatibility Jamo"},
		{0x3190, false, "after Hangul Compatibility Jamo"},
		{0x4DBF, true, "end of CJK Extension A"},
		{0x4DC0, false, "after CJK Extension A"},
		{0x9FFF, true, "end of CJK Unified Ideographs"},
		{0xA000, false, "after CJK Unified Ideographs"},
		{0xA95F, false, "just before Hangul Syllables"},
		{0xA960, true, "start of Hangul Syllables"},
		{0xA97F, true, "end of Hangul Syllables"},
		{0xAA00, false, "after Hangul Syllables"},
		{0xD7AF, true, "end of Hangul Syllables"},
		{0xD7B0, false, "after Hangul Syllables"},
		{0xFAFF, true, "end of CJK Compatibility Ideographs"},
		{0xFB00, false, "after CJK Compatibility Ideographs"},
		{0xFFEF, true, "end of Halfwidth and Fullwidth Forms"},
		{0xFFF0, false, "after Halfwidth and Fullwidth Forms"},
		{0x2FA1F, true, "end of CJK Compatibility Ideographs Supplement"},
		{0x2FA20, false, "after CJK Compatibility Ideographs Supplement"},
	}

	for _, tt := range boundaryTests {
		result := isWideRune(tt.rune)
		if result != tt.expected {
			t.Errorf("%s: isWideRune(0x%04X) = %t, want %t", tt.desc, uint32(tt.rune), result, tt.expected)
		}
	}

	// Test common ASCII characters
	asciiTests := []rune{'a', 'Z', '0', '!', ' ', '@'}
	for _, r := range asciiTests {
		if isWideRune(r) {
			t.Errorf("isWideRune('%c') = true, want false for ASCII", r)
		}
	}

	// Test common CJK characters
	cjkTests := []rune{'你', '好', '世', '界', '中', '国', '日', '本', '韓', '國'}
	for _, r := range cjkTests {
		if !isWideRune(r) {
			t.Errorf("isWideRune('%c') = false, want true for CJK", r)
		}
	}
}
