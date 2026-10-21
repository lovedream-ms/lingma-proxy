package service

import (
	"fmt"
	"testing"

	"lingma-ipc-proxy/internal/toolemulation"
)

func loopTestHistory(sequence, outputs []string) []ChatMessage {
	messages := []ChatMessage{{Role: "user", Text: "audit the project"}}
	for i, command := range sequence {
		id := fmt.Sprintf("call-%d", i)
		messages = append(messages,
			ChatMessage{Role: "assistant", ToolCalls: []toolemulation.ToolCall{{ID: id, Name: "bash", Arguments: map[string]any{"command": command, "description": fmt.Sprintf("label %d", i)}}}},
			ChatMessage{Role: "tool", ToolCallID: id, Text: outputs[i]},
		)
	}
	return messages
}

func TestToolLoopDetection(t *testing.T) {
	policy := toolLoopPolicy{repeats: 3, pollRepeats: 12, pollTools: map[string]bool{"poll": true}}
	for _, tc := range []struct {
		name              string
		commands, outputs []string
		period            int
	}{
		{"A", []string{"A", "A", "A"}, []string{"0", "0", "0"}, 1},
		{"AB", []string{"A", "B", "A", "B", "A", "B"}, []string{"", "0", "", "0", "", "0"}, 2},
		{"ABC", []string{"A", "B", "C", "A", "B", "C", "A", "B", "C"}, []string{"a", "b", "c", "a", "b", "c", "a", "b", "c"}, 3},
		{"ABCD", []string{"A", "B", "C", "D", "A", "B", "C", "D", "A", "B", "C", "D"}, []string{"", "", "", "", "", "", "", "", "", "", "", ""}, 4},
		{"too early", []string{"A", "A"}, []string{"0", "0"}, 0},
		{"progress", []string{"A", "A", "A"}, []string{"0", "1", "2"}, 0},
		{"cycle progress", []string{"A", "B", "A", "B", "A", "B"}, []string{"a", "1", "a", "2", "a", "3"}, 0},
		{"edit between reads", []string{"A", "edit1", "A", "edit2", "A"}, []string{"", "ok", "", "ok", ""}, 0},
	} {
		t.Run(tc.name, func(t *testing.T) {
			history := loopTestHistory(tc.commands, tc.outputs)
			evidence := policy.detect(history)
			if tc.period == 0 {
				if evidence != nil {
					t.Fatalf("false positive: %+v", evidence)
				}
				return
			}
			if evidence == nil || evidence.period != tc.period {
				t.Fatalf("cycle missed: %+v", evidence)
			}
			if !evidence.blocks([]toolemulation.ToolCall{{Name: "bash", Arguments: map[string]any{"description": "changed UI label", "command": tc.commands[0]}}}) {
				t.Fatal("description change bypassed detection")
			}
			if evidence.blocks([]toolemulation.ToolCall{{Name: "bash", Arguments: map[string]any{"command": "new work"}}}) {
				t.Fatal("new operation rejected")
			}
			if evidence.blocks([]toolemulation.ToolCall{{Name: "bash", Arguments: map[string]any{"command": tc.commands[0], "cwd": "another directory"}}}) {
				t.Fatal("different execution parameters rejected")
			}
			history = append(history, ChatMessage{Role: "user", Text: "run it again"})
			if policy.detect(history) != nil {
				t.Fatal("new instruction failed to reset evidence")
			}
		})
	}
}

func TestToolLoopPairsParallelResultsAndRejectsIncompleteEvidence(t *testing.T) {
	policy := toolLoopPolicy{repeats: 3}
	history := []ChatMessage{{Role: "user", Text: "audit"}}
	for i := 0; i < 3; i++ {
		a, b := fmt.Sprintf("a%d", i), fmt.Sprintf("b%d", i)
		history = append(history, ChatMessage{Role: "assistant", ToolCalls: []toolemulation.ToolCall{
			{ID: a, Name: "bash", Arguments: map[string]any{"command": "A"}},
			{ID: b, Name: "bash", Arguments: map[string]any{"command": "B"}},
		}}, ChatMessage{Role: "tool", ToolCallID: b, Text: "0"}, ChatMessage{Role: "tool", ToolCallID: a, Text: ""})
	}
	if evidence := policy.detect(history); evidence == nil || evidence.period != 2 {
		t.Fatalf("parallel result order changed cycle: %+v", evidence)
	}
	if policy.detect(history[:len(history)-1]) != nil {
		t.Fatal("incomplete parallel batch used as evidence")
	}
	history = append(history, ChatMessage{Role: "assistant", ToolCalls: []toolemulation.ToolCall{{ID: "missing", Name: "bash"}}})
	if policy.detect(history) != nil {
		t.Fatal("pending call used as completed evidence")
	}
}

func TestToolLoopPollingAllowanceIsFinite(t *testing.T) {
	policy := toolLoopPolicy{repeats: 3, pollRepeats: 12, pollTools: map[string]bool{"poll": true}}
	history := []ChatMessage{{Role: "user", Text: "wait for job"}}
	for i := 0; i < 12; i++ {
		id := fmt.Sprint(i)
		history = append(history, ChatMessage{Role: "assistant", ToolCalls: []toolemulation.ToolCall{{ID: id, Name: "poll", Arguments: map[string]any{"job_id": "123"}}}}, ChatMessage{Role: "tool", ToolCallID: id, Text: "running"})
		if i < 11 && policy.detect(history) != nil {
			t.Fatal("normal bounded polling blocked")
		}
	}
	if policy.detect(history) == nil {
		t.Fatal("polling allowed an infinite loop")
	}
}

func TestToolLoopCorrectionUsesFreshIPCSession(t *testing.T) {
	if resolveSessionMode(ChatRequest{toolLoopCorrection: true}, SessionModeReuse) != SessionModeFresh {
		t.Fatal("correction instruction would be lost in reused IPC prompt")
	}
}
