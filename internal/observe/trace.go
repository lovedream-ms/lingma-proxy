package observe

import (
	"context"
	"sync"
	"time"
)

type Event struct {
	Stage     string `json:"stage"`
	Model     string `json:"model,omitempty"`
	ElapsedMS int64  `json:"elapsedMs"`
	WaitMS    int64  `json:"waitMs,omitempty"`
}

type Snapshot struct {
	InputTokens  int    `json:"inputTokens"`
	OutputTokens int    `json:"outputTokens"`
	UsageKnown   bool   `json:"usageKnown"`
	ErrorCode    string `json:"errorCode,omitempty"`
	ErrorMessage string `json:"errorMessage,omitempty"`
	Action       string `json:"action,omitempty"`

	ID              string  `json:"id"`
	Path            string  `json:"path"`
	Stage           string  `json:"stage"`
	RequestedModel  string  `json:"requestedModel,omitempty"`
	ActualModel     string  `json:"actualModel,omitempty"`
	QueueMS         int64   `json:"queueMs"`
	FirstActivityMS *int64  `json:"firstActivityMs,omitempty"`
	ElapsedMS       int64   `json:"elapsedMs"`
	Events          []Event `json:"events"`
}

type Trace struct {
	mu        sync.Mutex
	start     time.Time
	queueDone bool
	data      Snapshot
}
type contextKey struct{}

func New(ctx context.Context, id, path string) (context.Context, *Trace) {
	t := &Trace{start: time.Now(), data: Snapshot{ID: id, Path: path, Stage: "received", Events: []Event{}}}
	return context.WithValue(ctx, contextKey{}, t), t
}

func (t *Trace) Snapshot() Snapshot {
	t.mu.Lock()
	defer t.mu.Unlock()
	out := t.data
	out.ElapsedMS = time.Since(t.start).Milliseconds()
	out.Events = append([]Event{}, out.Events...)
	return out
}

func Requested(ctx context.Context, model string) {
	if t, ok := ctx.Value(contextKey{}).(*Trace); ok {
		t.mu.Lock()
		if t.data.RequestedModel == "" {
			t.data.RequestedModel = model
		}
		t.mu.Unlock()
	}
}

func Emit(ctx context.Context, stage, model string, wait time.Duration) {
	t, ok := ctx.Value(contextKey{}).(*Trace)
	if !ok {
		return
	}
	t.mu.Lock()
	defer t.mu.Unlock()
	elapsed := time.Since(t.start).Milliseconds()
	if stage == "generating" && t.data.FirstActivityMS != nil {
		return
	}
	if stage == "generating" {
		t.data.FirstActivityMS = &elapsed
	}
	if stage == "upstream" && !t.queueDone {
		t.data.QueueMS = elapsed
		t.queueDone = true
	}
	if model != "" {
		t.data.ActualModel = model
	}
	t.data.Stage = stage
	if len(t.data.Events) < 64 {
		t.data.Events = append(t.data.Events, Event{Stage: stage, Model: model, ElapsedMS: elapsed, WaitMS: wait.Milliseconds()})
	}
}

func Usage(ctx context.Context, input, output int) {
	if t, ok := ctx.Value(contextKey{}).(*Trace); ok {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.data.InputTokens = max(0, input)
		t.data.OutputTokens = max(0, output)
		t.data.UsageKnown = true
	}
}

func Fail(ctx context.Context, code, message, action string) {
	if t, ok := ctx.Value(contextKey{}).(*Trace); ok {
		t.mu.Lock()
		defer t.mu.Unlock()
		t.data.ErrorCode = code
		t.data.ErrorMessage = message
		t.data.Action = action
	}
}
