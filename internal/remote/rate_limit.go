package remote

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"net/http"
	"strconv"
	"strings"
	"time"
)

// UpstreamError preserves HTTP and SSE status without disguising throttling as JSON failure.
type UpstreamError struct {
	StatusCode     int
	Code           string
	Message        string
	RetryAfter     time.Duration
	QuotaExhausted bool
}

func (e *UpstreamError) Error() string {
	message := fmt.Sprintf("remote upstream status %d", e.StatusCode)
	if e.Code != "" {
		message += " (" + e.Code + ")"
	}
	if e.Message != "" {
		message += ": " + e.Message
	}
	if e.RetryAfter > 0 {
		message += fmt.Sprintf("; retry after %.0fs", e.RetryAfter.Seconds())
	}
	return message
}

func parseRetryAfter(value string, now time.Time) time.Duration {
	if seconds, err := strconv.ParseFloat(strings.TrimSpace(value), 64); err == nil && seconds >= 0 && seconds <= float64((1<<63-1)/int64(time.Second)) {
		return time.Duration(seconds * float64(time.Second))
	}
	if date, err := http.ParseTime(value); err == nil && date.After(now) {
		return date.Sub(now)
	}
	return 0
}

func newUpstreamError(status int, body string, headers http.Header) *UpstreamError {
	e := &UpstreamError{StatusCode: status, RetryAfter: parseRetryAfter(headers.Get("Retry-After"), time.Now())}
	var raw any
	if json.Unmarshal([]byte(body), &raw) == nil {
		var walk func(any)
		walk = func(raw any) {
			if text, ok := raw.(string); ok {
				var nested any
				if json.Unmarshal([]byte(text), &nested) == nil {
					if _, ok := nested.(map[string]any); ok {
						walk(nested)
					}
				}
				return
			}
			value, ok := raw.(map[string]any)
			if !ok {
				return
			}
			for _, key := range []string{"code", "error_code", "errorCode", "type"} {
				if v, ok := value[key].(string); ok && (e.Code == "" || e.Code == "provider_error") {
					e.Code = v
				}
			}
			for _, key := range []string{"message", "msg", "error_message", "errorMessage"} {
				if v, ok := value[key].(string); ok && (e.Message == "" || e.Message == "All models failed") {
					e.Message = truncate(v, 500)
				}
			}
			for _, key := range []string{"retry_after", "retryAfter", "Retry-After", "retry-after"} {
				if v, ok := value[key]; ok {
					if delay := parseRetryAfter(fmt.Sprint(v), time.Now()); delay > e.RetryAfter {
						e.RetryAfter = delay
					}
				}
			}
			for _, key := range []string{"error", "data", "headers", "details"} {
				walk(value[key])
			}
		}
		walk(raw)
	}
	if e.Message == "" && !strings.HasPrefix(strings.TrimSpace(body), "{") {
		e.Message = truncate(strings.TrimSpace(body), 500)
	}
	text := strings.ToLower(e.Code + " " + e.Message)
	for _, marker := range []string{"insufficient_quota", "quota_exhausted", "credits_exhausted", "credit exhausted", "insufficient credit", "quota has been exhausted", "余额不足", "额度已用完", "额度耗尽", "积分不足"} {
		if strings.Contains(text, marker) {
			e.QuotaExhausted = true
		}
	}
	return e
}

func waitForContext(ctx context.Context, delay time.Duration) error {
	if delay <= 0 {
		return ctx.Err()
	}
	timer := time.NewTimer(delay)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func (c *Client) Chat(ctx context.Context, request ChatRequest, onDelta func(string)) (*ChatResult, error) {
	baseDelay := c.cfg.RetryBaseDelay
	if baseDelay <= 0 {
		baseDelay = time.Second
	}
	maxRetries := c.cfg.MaxRetries
	if maxRetries == 0 {
		maxRetries = 3
	}
	for attempt := 0; ; attempt++ {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		emitted := false
		current := request
		if request.OnReasoning != nil {
			current.OnReasoning = func(s string) {
				if s != "" {
					emitted = true
				}
				request.OnReasoning(s)
			}
		}
		if request.OnToolDelta != nil {
			current.OnToolDelta = func(call ToolCallFragment) { emitted = true; request.OnToolDelta(call) }
		}
		result, err := c.chatAttempt(ctx, current, func(s string) {
			if s != "" {
				emitted = true
			}
			if onDelta != nil {
				onDelta(s)
			}
		})
		if err == nil {
			return result, nil
		}
		var upstream *UpstreamError
		if errors.As(err, &upstream) && upstream.QuotaExhausted {
			return nil, err
		}
		if !errors.As(err, &upstream) || upstream.StatusCode != http.StatusTooManyRequests {
			return nil, err
		}
		delay := baseDelay * time.Duration(1<<min(attempt, 5))
		if delay > 30*time.Second {
			delay = 30 * time.Second
		}
		delay = time.Duration(float64(delay) * (0.85 + rand.Float64()*0.30))
		// A server deadline is a lower bound and is never shortened by jitter or the cap.
		if upstream.RetryAfter > delay {
			delay = upstream.RetryAfter
		}
		if emitted || attempt >= maxRetries {
			return nil, err
		}
		if err := waitForContext(ctx, delay); err != nil {
			return nil, err
		}
	}
}

// sseResponseHeaders accepts both simple and Spring-style multi-value headers.
func sseResponseHeaders(raw json.RawMessage) http.Header {
	var fields map[string]any
	if json.Unmarshal(raw, &fields) != nil {
		return nil
	}
	headers := make(http.Header)
	for key, value := range fields {
		switch v := value.(type) {
		case string:
			headers.Set(key, v)
		case []any:
			for _, item := range v {
				if text, ok := item.(string); ok {
					headers.Add(key, text)
				}
			}
		}
	}
	return headers
}
