package toolemulation

import (
	"strings"
	"testing"
)

func verifyCodeTool() []ToolDef {
	return []ToolDef{{Name: "run_code", InputSchema: map[string]any{
		"type": "object", "properties": map[string]any{"code": map[string]any{"type": "string"}}, "required": []any{"code"},
	}}}
}

func TestVerifyHarnessActionFormats(t *testing.T) {
	for _, raw := range []string{
		`{"tool":"run_code","parameters":{"code":"console.log(1)"}}`,
		`{"name":"run_code","arguments":"{\"code\":\"console.log(1)\"}"}`,
		`{"function":{"name":"run_code","arguments":"{\"code\":\"console.log(1)\"}"}}`,
		"```action\n{\"tool\":\"run_code\",\"parameters\":{\"code\":\"console.log(1)\",},}\n```",
		"```json\n{\"tool\":\"run_code\",\"parameters\":{\"code\":\"console.log(1)\"}}\n```",
	} {
		calls, text, err := ParseActionBlocks(raw, verifyCodeTool(), Config{})
		if err != nil || len(calls) != 1 || text != "" || calls[0].Arguments["code"] != "console.log(1)" {
			t.Fatalf("format %q: calls=%+v remaining=%q err=%v", raw, calls, text, err)
		}
	}
}

func TestVerifyHarnessPreservesStringContents(t *testing.T) {
	code := "console.log('“保留原文”, }');"
	raw := `{"tool":"run_code","parameters":{"code":"` + code + `"}}`
	calls, _, err := ParseActionBlocks(raw, verifyCodeTool(), Config{})
	if err != nil || len(calls) != 1 || calls[0].Arguments["code"] != code {
		t.Fatalf("valid string corrupted: %+v %v", calls, err)
	}
}

func TestVerifyHarnessPTCRequiresDeclaredFunction(t *testing.T) {
	call := ToolCall{Name: "web_search", Arguments: map[string]any{"query": "a\";throw 1;"}}
	calls, err := NormalizeToolCalls([]ToolCall{call}, verifyCodeTool(), ToolChoice{}, "Use tools.web_search for searching.")
	if err != nil || len(calls) != 1 || calls[0].Name != "run_code" || !strings.Contains(calls[0].Arguments["code"].(string), `tools["web_search"]`) {
		t.Fatalf("PTC mapping = %+v, %v", calls, err)
	}
	for _, context := range []string{"", "tools.web_search_extra", "tools.other"} {
		if _, err := NormalizeToolCalls([]ToolCall{call}, verifyCodeTool(), ToolChoice{}, context); err == nil {
			t.Fatalf("undeclared PTC tool accepted with context %q", context)
		}
	}
}

func TestVerifyHarnessRejectsBadArgumentsAndChoice(t *testing.T) {
	for _, args := range []map[string]any{{}, {"code": 5}, {"raw_arguments": "{broken"}, {"code": ""}} {
		if _, err := NormalizeToolCalls([]ToolCall{{Name: "run_code", Arguments: args}}, verifyCodeTool(), ToolChoice{}, ""); err == nil {
			t.Fatalf("invalid arguments accepted: %+v", args)
		}
	}
	if _, err := NormalizeToolCalls([]ToolCall{{Name: "run_code", Arguments: map[string]any{"code": "ok"}}}, verifyCodeTool(), ToolChoice{Mode: "none"}, ""); err == nil {
		t.Fatal("tool_choice=none ignored")
	}
}

func TestVerifyHarnessRepairsNativeJSONAndDeduplicates(t *testing.T) {
	call := ToolCall{ID: "call-original", Name: "RUN_CODE", Arguments: map[string]any{"raw_arguments": `{"code":"console.log(1)",}`}}
	calls, err := NormalizeToolCalls([]ToolCall{call, call}, verifyCodeTool(), ToolChoice{}, "")
	if err != nil || len(calls) != 1 || calls[0].ID != "call-original" || calls[0].Arguments["code"] != "console.log(1)" {
		t.Fatalf("native normalization = %+v, %v", calls, err)
	}
}
