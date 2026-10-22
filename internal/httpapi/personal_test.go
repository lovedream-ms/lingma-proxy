package httpapi

import (
	"bytes"
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

func TestPersonalAccessBoundaries(t *testing.T) {
	s := NewServer("0.0.0.0:8096", service.New(service.Config{APIKey: "client", AdminKey: "admin", AllowedOrigins: []string{"http://localhost:3000"}}))
	if s.ValidateListener() != nil {
		t.Fatal("keyed listener rejected")
	}
	if NewServer("0.0.0.0:8096", service.New(service.Config{})).ValidateListener() == nil {
		t.Fatal("unkeyed LAN listener allowed")
	}
	for _, tc := range []struct {
		path, key, origin, addr string
		status                  int
	}{
		{"/version", "", "", "192.0.2.1:1", 401},
		{"/version", "client", "", "192.0.2.1:1", 200},
		{"/debug/activity", "client", "", "127.0.0.1:1", 401},
		{"/debug/activity", "admin", "", "192.0.2.1:1", 200},
		{"/version", "client", "http://evil.example", "127.0.0.1:1", 403},
		{"/version", "client", "http://localhost:3000", "127.0.0.1:1", 200},
	} {
		req := httptest.NewRequest(http.MethodGet, tc.path, nil)
		req.RemoteAddr = tc.addr
		req.Header.Set("Authorization", "Bearer "+tc.key)
		if tc.origin != "" {
			req.Header.Set("Origin", tc.origin)
		}
		rec := httptest.NewRecorder()
		s.http.Handler.ServeHTTP(rec, req)
		if rec.Code != tc.status {
			t.Fatalf("%+v: status=%d %s", tc, rec.Code, rec.Body.String())
		}
	}
	local := NewServer("127.0.0.1:8096", service.New(service.Config{}))
	req := httptest.NewRequest("GET", "/debug/activity", nil)
	req.RemoteAddr = "192.0.2.1:1"
	req.Header.Set("X-Forwarded-For", "127.0.0.1")
	rec := httptest.NewRecorder()
	local.http.Handler.ServeHTTP(rec, req)
	if rec.Code != 403 {
		t.Fatal("forwarded localhost bypassed debug boundary")
	}
}

func TestPersonalMetadataUsageAndCaptureBound(t *testing.T) {
	s, _ := verifyHarnessServer(t, func(int, map[string]any) []map[string]any {
		return []map[string]any{verifyDelta(map[string]any{"content": strings.Repeat("private", 20000)}), {"choices": []any{}, "usage": map[string]any{"prompt_tokens": 12, "completion_tokens": 3}}}
	})
	verifyHarnessRequestBody(t, s, `{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"private"}]}`)
	r := s.debugRecords(1)[0]
	if r.InputTokens != 12 || r.OutputTokens != 3 || r.Request != "" || r.Response != "" {
		t.Fatalf("privacy or usage broken: %+v", r)
	}
	writer := &recordingResponseWriter{ResponseWriter: httptest.NewRecorder(), limit: 1024}
	writer.Write(bytes.Repeat([]byte("x"), 200000))
	if len(writer.body) > 1024 {
		t.Fatal("capture exceeded cap")
	}
}

func TestPersonalRequestLimitAndSecretRedaction(t *testing.T) {
	s := NewServer("", service.New(service.Config{MaxRequestBytes: 1024}))
	rec := httptest.NewRecorder()
	s.http.Handler.ServeHTTP(rec, httptest.NewRequest("POST", "/v1/chat/completions", strings.NewReader(strings.Repeat("x", 1025))))
	if rec.Code != 413 {
		t.Fatalf("large body status %d", rec.Code)
	}
	for _, raw := range []string{`{"api_key":"secret","password":"secret"}`, `data: {"authorization":"Bearer secret"}`, `Bearer secret`} {
		if got := SanitizeLogBody(raw, 1024); strings.Contains(got, "secret") {
			t.Fatalf("secret persisted: %s", got)
		}
	}
}
