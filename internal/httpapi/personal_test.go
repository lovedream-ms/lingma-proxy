package httpapi

import (
	"context"
	"lingma-ipc-proxy/internal/observe"
	"lingma-ipc-proxy/internal/remote"
	"lingma-ipc-proxy/internal/service"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestPersonalTracePreservesProtocolAndUsage(t *testing.T) {
	s, _ := verifyHarnessServer(t, func(int, map[string]any) []map[string]any {
		return []map[string]any{verifyDelta(map[string]any{"content": "OK"}), {"choices": []any{}, "usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 3}}}
	})
	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(`{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hello"}]}`)))
	records := s.debugRecords(10)
	if rec.Code != 200 || len(records) != 1 || records[0].Trace.ID != rec.Header().Get("X-Request-ID") || records[0].Trace.ActualModel != "kmodel" || records[0].Trace.FirstActivityMS == nil || len(s.Activity()) != 0 || !strings.Contains(rec.Body.String(), `"prompt_tokens":12`) {
		t.Fatalf("trace/protocol lost: %+v %s", records, rec.Body.String())
	}
}

func TestPersonalQueueCancellationIsObservable(t *testing.T) {
	s := NewServer("", service.New(service.Config{MaxConcurrentRequests: 1}))
	s.sem <- struct{}{}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Millisecond)
	defer cancel()
	ctx, trace := observe.New(ctx, "test", "/test")
	if s.acquire(ctx) || trace.Snapshot().Stage != "queued" {
		t.Fatal("canceled queue acquired slot or lost stage")
	}
	s.release()
}

func TestDiagnosticErrorClasses(t *testing.T) {
	for _, tc := range []struct {
		err  error
		code string
	}{
		{&remote.UpstreamError{StatusCode: 429}, "rate_limited"},
		{&remote.UpstreamError{StatusCode: 429, QuotaExhausted: true}, "quota_exhausted"},
		{&remote.UpstreamError{StatusCode: 401}, "authentication"},
		{&remote.UpstreamError{StatusCode: 503}, "upstream_unavailable"},
		{context.DeadlineExceeded, "timeout"},
	} {
		if got := service.DiagnoseError(tc.err); got.Code != tc.code {
			t.Fatalf("got %+v want %s", got, tc.code)
		}
	}
}

var _ = http.MethodGet
