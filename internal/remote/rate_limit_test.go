package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

func verifyRateClient(t *testing.T, handler http.HandlerFunc) *Client {
	t.Helper()
	server := httptest.NewServer(handler)
	t.Cleanup(server.Close)
	auth := filepath.Join(t.TempDir(), "auth.json")
	if err := SaveCredentialFile(Credential{CosyKey: "test", EncryptUserInfo: "test", UserID: "test", MachineID: "test"}, auth); err != nil {
		t.Fatal(err)
	}
	return New(Config{BaseURL: server.URL, AuthFile: auth, RetryBaseDelay: time.Millisecond})
}

func TestVerifyProviderQuotaDetailsDoNotBlockNewRequests(t *testing.T) {
	details := `{"error":{"message":"You exceeded your current quota, please check your plan and billing details.","type":"insufficient_quota","code":"insufficient_quota"}}`
	body, _ := json.Marshal(map[string]any{"code": "provider_error", "message": "All models failed", "details": details})
	outer, _ := json.Marshal(map[string]any{"statusCodeValue": 429, "body": string(body), "headers": map[string]any{"Retry-After": []string{"60"}}})
	_, _, err := parseSSEPayload(string(outer))
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || !upstream.QuotaExhausted || upstream.Code != "insufficient_quota" || upstream.RetryAfter != time.Minute || !strings.Contains(upstream.Message, "current quota") {
		t.Fatalf("lost actual provider quota error: %#v %v", upstream, err)
	}
	var calls atomic.Int32
	client := verifyRateClient(t, func(w http.ResponseWriter, r *http.Request) { calls.Add(1); fmt.Fprintf(w, "data: %s\n\n", outer) })
	for i := 0; i < 3; i++ {
		if _, err := client.Chat(context.Background(), ChatRequest{Model: "qmodel", Prompt: "hello"}, nil); !errors.As(err, &upstream) || !upstream.QuotaExhausted {
			t.Fatalf("quota was hidden: %v", err)
		}
	}
	if calls.Load() != 3 {
		t.Fatalf("new requests must reach upstream instead of replaying a cached failure: %d requests", calls.Load())
	}
}

func TestVerify429RetriesAreBoundedAndServerDelayIsHonored(t *testing.T) {
	var calls atomic.Int32
	var previous time.Time
	client := verifyRateClient(t, func(w http.ResponseWriter, r *http.Request) {
		now := time.Now()
		if !previous.IsZero() && now.Sub(previous) < 25*time.Millisecond {
			t.Error("Retry-After was shortened")
		}
		previous = now
		calls.Add(1)
		w.Header().Set("Retry-After", "0.03")
		w.WriteHeader(429)
		fmt.Fprint(w, `{"error":{"code":"rate_limit_exceeded","message":"Too many requests"}}`)
	})
	_, err := client.Chat(context.Background(), ChatRequest{Model: "qmodel", Prompt: "hello"}, nil)
	var upstream *UpstreamError
	if !errors.As(err, &upstream) || upstream.QuotaExhausted || calls.Load() != 4 {
		t.Fatalf("expected initial plus 3 bounded attempts: calls=%d err=%v", calls.Load(), err)
	}
}

func TestVerifyRetryAfterDateAndSSEHeaderSpellings(t *testing.T) {
	now := time.Now().UTC().Truncate(time.Second)
	if parseRetryAfter(now.Add(time.Minute).Format(http.TimeFormat), now) != time.Minute {
		t.Fatal("HTTP date Retry-After not honored")
	}
	for _, headers := range []string{`{"retry-after":"2"}`, `{"Retry-After":["2"]}`} {
		outer, _ := json.Marshal(map[string]any{"statusCodeValue": 429, "body": `{"message":"Too many requests"}`, "headers": json.RawMessage(headers)})
		_, _, err := parseSSEPayload(string(outer))
		var upstream *UpstreamError
		if !errors.As(err, &upstream) || upstream.RetryAfter != 2*time.Second {
			t.Fatalf("missing SSE deadline: %v", err)
		}
	}
}

func TestVerifyNoRetryAfterPartialStreamAndCancellation(t *testing.T) {
	var calls atomic.Int32
	client := verifyRateClient(t, func(w http.ResponseWriter, r *http.Request) {
		calls.Add(1)
		fmt.Fprint(w, "data: {\"choices\":[{\"delta\":{\"content\":\"partial\"}}]}\n\ndata: {\"statusCodeValue\":429,\"body\":\"Too many requests\"}\n\n")
	})
	_, err := client.Chat(context.Background(), ChatRequest{Model: "qmodel", Prompt: "hello"}, func(string) {})
	if err == nil || calls.Load() != 1 {
		t.Fatalf("partial response replayed: %d %v", calls.Load(), err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err = client.Chat(ctx, ChatRequest{Prompt: "cancelled"}, nil)
	if !errors.Is(err, context.Canceled) || calls.Load() != 1 {
		t.Fatalf("cancelled request reached upstream: %v", err)
	}
}

func TestVerifyRemoteCallsAreNotLocallySerialized(t *testing.T) {
	entered := make(chan struct{}, 2)
	release := make(chan struct{})
	client := verifyRateClient(t, func(w http.ResponseWriter, r *http.Request) {
		entered <- struct{}{}
		select {
		case <-release:
		case <-r.Context().Done():
		}
		fmt.Fprint(w, "data: [DONE]\n\n")
	})
	defer close(release)
	second := New(client.cfg)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	done := make(chan error, 2)
	go func() { _, err := client.Chat(ctx, ChatRequest{Prompt: "a"}, nil); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("first request did not start")
	}
	go func() { _, err := second.Chat(ctx, ChatRequest{Prompt: "b"}, nil); done <- err }()
	select {
	case <-entered:
	case <-ctx.Done():
		t.Fatal("new requests were locally serialized")
	}
}
