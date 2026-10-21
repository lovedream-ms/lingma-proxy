package httpapi

import (
	"encoding/json"
	"net/http/httptest"
	"testing"
	"time"

	"lingma-ipc-proxy/internal/remote"
	"lingma-ipc-proxy/internal/service"
	"lingma-ipc-proxy/internal/toolemulation"
)

func TestVerifyUISettingsReachHTTPExecution(t *testing.T) {
	t.Setenv("LINGMA_PROXY_MAX_CONCURRENT", "2")
	t.Setenv("LINGMA_AGGREGATE_TOOL_STREAM", "true")
	disabled := false
	svc := service.New(service.Config{MaxConcurrentRequests: 9, AggregateToolStream: &disabled})
	server := NewServer("", svc)
	if cap(server.sem) != 9 {
		t.Fatal("HTTP concurrency ignored saved setting")
	}
	req := service.ChatRequest{Tools: []toolemulation.ToolDef{{Name: "echo"}}}
	server.applyDefaultModel(&req)
	if shouldAggregateToolStream(req) {
		t.Fatal("explicit realtime streaming was overridden by environment")
	}
}

func TestVerifyUpstreamRateLimitStatusAndInstructions(t *testing.T) {
	err := &remote.UpstreamError{StatusCode: 429, Code: "rate_limit_exceeded", Message: "Too many requests", RetryAfter: 1100 * time.Millisecond}
	rec := httptest.NewRecorder()
	writeUpstreamAPIError(rec, err, false)
	if rec.Code != 429 || rec.Header().Get("Retry-After") != "2" {
		t.Fatalf("rate limit became 500 or deadline rounded down: %d %s", rec.Code, rec.Header())
	}
	var body struct {
		Error map[string]any `json:"error"`
	}
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error["type"] != "rate_limit_error" || body.Error["retry_after_seconds"] != float64(2) {
		t.Fatalf("instructions missing: %s", rec.Body.String())
	}
	err.QuotaExhausted = true
	err.Code = "insufficient_quota"
	rec = httptest.NewRecorder()
	writeUpstreamAPIError(rec, err, false)
	if rec.Header().Get("Retry-After") != "" {
		t.Fatal("quota exhaustion incorrectly advertised as transient")
	}
	if json.Unmarshal(rec.Body.Bytes(), &body) != nil || body.Error["code"] != "insufficient_quota" {
		t.Fatalf("quota reason lost: %s", rec.Body.String())
	}
}

func TestVerifyDefaultConcurrencyRestored(t *testing.T) {
	t.Setenv("LINGMA_PROXY_MAX_CONCURRENT", "")
	if service.New(service.Config{}).MaxConcurrentRequests() != 4 {
		t.Fatal("default concurrency is still restricted")
	}
	t.Setenv("LINGMA_PROXY_MAX_CONCURRENT", "99")
	if service.New(service.Config{}).MaxConcurrentRequests() != 16 {
		t.Fatal("existing configured concurrency range changed")
	}
	payload := upstreamAPIErrorPayload(&remote.UpstreamError{StatusCode: 429, Code: "insufficient_quota", QuotaExhausted: true})
	if _, exists := payload["retryable"]; exists {
		t.Fatal("local policy still forbids a new attempt")
	}
}
