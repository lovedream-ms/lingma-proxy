package service

import (
	"strings"
	"testing"

	"lingma-ipc-proxy/internal/remote"
	"lingma-ipc-proxy/internal/toolemulation"
)

func TestVerifyHarnessTranscriptKeepsToolOnlyAndEmptyResult(t *testing.T) {
	req := ChatRequest{Messages: []ChatMessage{
		{Role: "user", Text: "输出1"},
		{Role: "assistant", ToolCalls: []toolemulation.ToolCall{{ID: "c1", Name: "run_code", Arguments: map[string]any{"code": "console.log(1)"}}}},
		{Role: "tool", ToolCallID: "c1"},
	}}
	prompt, err := buildLingmaPrompt(req, SessionModeFresh, true)
	if err != nil || !strings.Contains(prompt, "id=c1 name=run_code") || !strings.Contains(prompt, "(empty tool output)") {
		t.Fatalf("transcript lost tool history: %s %v", prompt, err)
	}
	messages := remoteMessagesFromRequest(req)
	if len(messages) != 3 || messages[2].Role != "tool" || messages[2].ToolCallID != "c1" {
		t.Fatalf("native history = %+v", messages)
	}
}

func TestVerifyIPCUsageKeepsRequestAndSessionCountsSeparate(t *testing.T) {
	svc := New(Config{Backend: BackendIPC})
	result := svc.buildChatResult(ChatRequest{}, "", "", "hello", &promptRunResult{
		ThoughtText:  strings.Repeat("thinking ", 100),
		ContextUsage: map[string]any{"usedTokens": float64(5000), "limitTokens": float64(32000)},
	}, SessionModeFresh)
	if result.InputTokens >= 5000 || result.UsedTokens != 5000 || result.LimitTokens != 32000 {
		t.Fatalf("context conflated with request usage: %+v", result)
	}
	if result.OutputTokens < result.ReasoningTokens || result.OutputTokens <= 1 {
		t.Fatalf("reasoning excluded from output: %+v", result)
	}
}

func TestVerifyHarnessDoesNotRepeatIdenticalFailedTool(t *testing.T) {
	call := toolemulation.ToolCall{ID: "failed", Name: "run_code", Arguments: map[string]any{"code": "require('fs')"}}
	req := ChatRequest{Tools: []toolemulation.ToolDef{{Name: "run_code"}}, Messages: []ChatMessage{
		{Role: "user", Text: "读取文件"}, {Role: "assistant", ToolCalls: []toolemulation.ToolCall{call}},
		{Role: "tool", ToolCallID: "failed", Text: "Error: ReferenceError: require is not defined"},
	}}
	if err := normalizeRemoteToolResult(req, &remote.ChatResult{ToolCalls: []toolemulation.ToolCall{call}}); err == nil {
		t.Fatal("identical failed call accepted")
	}
	corrected := call
	corrected.Arguments = map[string]any{"code": "await import('node:fs')"}
	if err := normalizeRemoteToolResult(req, &remote.ChatResult{ToolCalls: []toolemulation.ToolCall{corrected}}); err != nil {
		t.Fatalf("corrected call rejected: %v", err)
	}
	req.Messages = append(req.Messages, ChatMessage{Role: "user", Text: "重新执行同一个调用"})
	if err := normalizeRemoteToolResult(req, &remote.ChatResult{ToolCalls: []toolemulation.ToolCall{call}}); err != nil {
		t.Fatalf("explicit later request rejected: %v", err)
	}
}

func TestVerifyHarnessConceptualAnswerDoesNotForceTool(t *testing.T) {
	req := ChatRequest{Tools: []toolemulation.ToolDef{{Name: "run_code"}}}
	for _, text := range []string{"解释一下：我需要使用工具时，要先确认参数格式。", `{"answer":"ok"}`, "这是 JavaScript 的模块加载规则。"} {
		if shouldRetryRemoteNativeTool(req, text) {
			t.Fatalf("conceptual answer forced a tool: %q", text)
		}
	}
	if !shouldRetryRemoteNativeTool(req, "让我先搜索 Go 文档。") {
		t.Fatal("short unfulfilled plan did not trigger repair")
	}
}
