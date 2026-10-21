package modelmeta

import (
	"fmt"
	"strings"
	"testing"
)

func TestVerifyCatalogueAllScenesAndUpstreamMetadata(t *testing.T) {
	var entries []string
	for i := 0; i < 14; i++ {
		entries = append(entries, fmt.Sprintf(`{"key":"route-%d","display_name":"Model %d","max_input_tokens":180000,"is_vl":false,"enable":true}`, i, i))
	}
	body := `{"data":{"assistant":[` + strings.Join(entries[:6], ",") + `],"chat":[` + strings.Join(entries[6:10], ",") + `],"qwork":[` + strings.Join(entries[10:], ",") + `],"inline":[` + entries[0] + `]}}`
	models, err := ParseJSON([]byte(body))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 14 {
		t.Fatalf("model count=%d, want 14 distinct routes", len(models))
	}
	for _, m := range models {
		if m.ContextWindow != 0 || m.MaxInputTokens != 180000 || m.SupportsVision || !m.SupportsVisionKnown {
			t.Fatalf("metadata lost: %+v", m)
		}
	}
}

func TestVerifyCatalogueCamelCaseAndDuplicateDisplayNames(t *testing.T) {
	models, err := ParseJSON([]byte(`{"app":[{"key":"a","displayName":"Same","maxInputTokens":1000000,"isVl":true},{"key":"b","displayName":"Same","maxInputTokens":"96000","isVl":false}],"byok_teams":[{"key":"off","displayName":"Disabled","enable":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 2 || models[0].MaxInputTokens != 1000000 || models[1].MaxInputTokens != 96000 || models[0].ContextWindow != 0 || models[1].ContextWindow != 0 || models[1].SupportsVision {
		t.Fatalf("unexpected models: %+v", models)
	}
}

func TestVerifyCatalogueMetadataOverridesEarlierAliasHint(t *testing.T) {
	models, err := ParseJSON([]byte(`{"assistant":[{"key":"kmodel","display_name":"Kimi-K2.6"}],"chat":[{"key":"kmodel","display_name":"Kimi-K2.6","context_window":32768,"supports_vision":false}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if len(models) != 1 || models[0].ContextWindow != 32768 || models[0].SupportsVision {
		t.Fatalf("authoritative metadata lost: %+v", models)
	}
}

func TestVerifyDefaultContextWindowIsDistinctFromInputLimit(t *testing.T) {
	models, err := ParseJSON([]byte(`{"app":[{"key":"dmodel","displayName":"DeepSeek","maxInputTokens":96000,"contextConfig":{"1M":{"tokenCount":1000000},"200K":{"isDefault":true,"tokenCount":200000},"400K":{"tokenCount":400000}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if models[0].ContextWindow != 200000 || models[0].MaxInputTokens != 96000 || models[0].MaxContextLength != 1000000 || len(models[0].ContextOptions) != 3 {
		t.Fatalf("input limit confused with default context window: %+v", models)
	}
}

func TestVerifyCatalogueRejectsEmptyAndUnknownHasNoInventedLimit(t *testing.T) {
	for _, body := range []string{`{}`, `{"chat":[]}`, `{"error":"not authenticated"}`} {
		if _, err := ParseJSON([]byte(body)); err == nil {
			t.Fatalf("empty catalogue accepted: %s", body)
		}
	}
	models, err := ParseJSON([]byte(`{"chat":[{"key":"future","display_name":"Future Model","max_input_tokens":-1}]}`))
	if err != nil {
		t.Fatal(err)
	}
	if models[0].ContextWindow != 0 || models[0].SupportsVision {
		t.Fatalf("invented metadata: %+v", models)
	}
}

func TestVerifySnakeCaseContextOptionsAndVision(t *testing.T) {
	models, err := ParseJSON([]byte(`{"app":[{"key":"future","display_name":"Future","is_vl":true,"context_config":{"200K":{"is_default":true,"token_count":200000},"1M":{"token_count":1000000}}}]}`))
	if err != nil {
		t.Fatal(err)
	}
	m := models[0]
	if m.ContextWindow != 200000 || m.MaxContextLength != 1000000 || !m.SupportsVision || len(m.ContextOptions) != 2 {
		t.Fatalf("metadata lost: %+v", m)
	}
}
