package main

import (
	"encoding/json"
	"strings"
	"testing"
	"time"
)

func TestExtractUsageFromJSONUsageWrapper(t *testing.T) {
	input, output := extractUsageFromJSON(`{"usage":{"prompt_tokens":161,"completion_tokens":3,"total_tokens":164}}`)
	if input != 161 || output != 3 {
		t.Fatalf("extractUsageFromJSON usage wrapper = (%d, %d), want (161, 3)", input, output)
	}
}

func TestVerifyUsageAliasesDoNotDoubleCount(t *testing.T) {
	input, output := extractUsageFromJSON(`{"usage":{"input_tokens":100,"prompt_tokens":100,"output_tokens":20,"completion_tokens":20}}`)
	if input != 100 || output != 20 {
		t.Fatalf("aliases doubled counts: %d/%d", input, output)
	}
	input, output = extractUsageFromJSON(`{"usage":{"input_tokens":0,"prompt_tokens":100,"output_tokens":0,"completion_tokens":20}}`)
	if input != 0 || output != 0 {
		t.Fatalf("explicit zero lost: %d/%d", input, output)
	}
}

func TestExtractUsageFromJSONFlatTokens(t *testing.T) {
	input, output := extractUsageFromJSON(`{"prompt_tokens":161,"completion_tokens":3,"total_tokens":164}`)
	if input != 161 || output != 3 {
		t.Fatalf("extractUsageFromJSON flat tokens = (%d, %d), want (161, 3)", input, output)
	}
}

func TestExtractTokenUsageStreamingFlatTokens(t *testing.T) {
	resp := "event: message\n" +
		`data: {"type":"message_start","prompt_tokens":161}` + "\n\n" +
		`data: {"type":"message_delta","completion_tokens":3,"total_tokens":164}` + "\n\n"
	input, output := extractTokenUsage(resp)
	if input != 161 || output != 3 {
		t.Fatalf("extractTokenUsage stream flat tokens = (%d, %d), want (161, 3)", input, output)
	}
}

func TestRedactAndLimitPayloadJSON(t *testing.T) {
	raw := `{"authorization":"Bearer abc123","access_token":"secret","image":"data:image/png;base64,AAAA","normal":"ok"}`
	got := redactAndLimitPayload(raw)
	if strings.Contains(got, "abc123") || strings.Contains(got, "secret") || strings.Contains(got, "data:image/png") {
		t.Fatalf("redactAndLimitPayload should redact secrets, got %s", got)
	}
	if !strings.Contains(got, "[REDACTED]") {
		t.Fatalf("redactAndLimitPayload should include redaction markers, got %s", got)
	}
}

func TestResolveFeedbackRangeCustom(t *testing.T) {
	startAt, endAt, err := resolveFeedbackRange(FeedbackExportOptions{
		RangePreset: "custom",
		StartAt:     "2026-05-13T10:00",
		EndAt:       "2026-05-13T11:30",
	})
	if err != nil {
		t.Fatalf("resolveFeedbackRange custom returned error: %v", err)
	}
	if startAt.After(endAt) {
		t.Fatalf("resolveFeedbackRange returned invalid range: %s > %s", startAt, endAt)
	}
}

func TestFilterRequestsByRangeUsesCreatedAt(t *testing.T) {
	now := time.Now()
	requests := []RequestRecord{
		{CreatedAt: now.Add(-15 * time.Minute).Format(time.RFC3339), Path: "/v1/messages"},
		{CreatedAt: now.Add(-3 * time.Hour).Format(time.RFC3339), Path: "/v1/models"},
		{Path: "/unknown"},
	}
	filtered := filterRequestsByRange(requests, now.Add(-30*time.Minute), now)
	if len(filtered) != 1 {
		t.Fatalf("filterRequestsByRange len = %d, want 1", len(filtered))
	}
	if filtered[0].Path != "/v1/messages" {
		t.Fatalf("filterRequestsByRange path = %s, want /v1/messages", filtered[0].Path)
	}
}

func TestBackfillRequestCreatedAtAcrossDays(t *testing.T) {
	anchor := time.Date(2026, 5, 13, 10, 0, 0, 0, time.Local)
	requests := []RequestRecord{
		{Time: "23:40:00", Path: "/oldest"},
		{Time: "00:20:00", Path: "/middle"},
		{Time: "09:15:00", Path: "/newest"},
	}

	if !backfillRequestCreatedAt(requests, anchor) {
		t.Fatalf("backfillRequestCreatedAt should report mutation")
	}

	if requests[2].CreatedAt == "" || requests[1].CreatedAt == "" || requests[0].CreatedAt == "" {
		t.Fatalf("backfillRequestCreatedAt should populate all createdAt values: %#v", requests)
	}

	if got, want := requests[2].CreatedAt[:10], "2026-05-13"; got != want {
		t.Fatalf("newest request date = %s, want %s", got, want)
	}
	if got, want := requests[1].CreatedAt[:10], "2026-05-13"; got != want {
		t.Fatalf("middle request date = %s, want %s", got, want)
	}
	if got, want := requests[0].CreatedAt[:10], "2026-05-12"; got != want {
		t.Fatalf("oldest request date = %s, want %s", got, want)
	}
}

func TestGetLogDetailUsesIDForSameSecondLogs(t *testing.T) {
	app := &App{}
	createdAt := "2026-05-27T10:47:35+08:00"
	app.logs = []AppLog{
		{ID: "first", CreatedAt: createdAt, Time: "10:47:35", Source: "app", Level: "info", Message: "first full message"},
		{ID: "second", CreatedAt: createdAt, Time: "10:47:35", Source: "app", Level: "info", Message: "second full message"},
	}

	got, err := app.GetLogDetail("first")
	if err != nil {
		t.Fatalf("GetLogDetail by id failed: %v", err)
	}
	if got.Message != "first full message" {
		t.Fatalf("GetLogDetail returned wrong same-second log: %#v", got)
	}
}

func TestVerifyModelCachePreservesCapabilityChanges(t *testing.T) {
	var models []ModelInfo
	if err := json.Unmarshal([]byte(`[{"id":"future","name":"Future","context_window":200000,"max_context_length":1000000,"context_options":[200000,400000,1000000],"supports_vision":true,"input_modalities":["text","image"]}]`), &models); err != nil {
		t.Fatal(err)
	}
	m := models[0]
	if m.MaxContextLength != 1000000 || m.SupportsVision == nil || !*m.SupportsVision || len(m.ContextOptions) != 3 {
		t.Fatalf("cache lost metadata: %+v", m)
	}
	previous := append([]ModelInfo(nil), models...)
	previous[0].MaxContextLength = 200000
	if sameModelList(previous, models) {
		t.Fatal("context change ignored")
	}
	previous = append([]ModelInfo(nil), models...)
	no := false
	previous[0].SupportsVision = &no
	if sameModelList(previous, models) {
		t.Fatal("vision change ignored")
	}
}
