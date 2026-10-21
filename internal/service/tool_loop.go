package service

import (
	"context"
	"crypto/sha256"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"

	"lingma-ipc-proxy/internal/toolemulation"
)

// ToolLoopError is a local, non-retryable rejection, not an upstream outage.
type ToolLoopError struct {
	Period  int
	Repeats int
}

func (e *ToolLoopError) Error() string {
	return fmt.Sprintf("tool_loop_detected: stopped a repeated tool cycle (period=%d, repetitions=%d) after one correction attempt; existing tool results did not change. Review the task or explicitly request another attempt. 检测到工具循环，纠偏后仍重复，已阻止下发重复调用。", e.Period, e.Repeats)
}

type toolLoopPolicy struct {
	repeats     int
	pollRepeats int
	pollTools   map[string]bool
}

func newToolLoopPolicy() toolLoopPolicy {
	bounded := func(name string, fallback, min, max int) int {
		n, err := strconv.Atoi(os.Getenv(name))
		if err != nil || n < min || n > max {
			return fallback
		}
		return n
	}
	policy := toolLoopPolicy{
		repeats:     bounded("LINGMA_TOOL_LOOP_REPEATS", 3, 3, 20),
		pollRepeats: bounded("LINGMA_TOOL_LOOP_POLL_REPEATS", 12, 3, 100),
		pollTools:   make(map[string]bool),
	}
	names, configured := os.LookupEnv("LINGMA_TOOL_LOOP_POLL_TOOLS")
	if !configured {
		names = "wait,poll,wait_for_completion,check_job_status"
	}
	for _, name := range strings.Split(names, ",") {
		if name = strings.TrimSpace(name); name != "" {
			policy.pollTools[name] = true
		}
	}
	return policy
}

type toolLoopRecord struct {
	call   string
	result [32]byte
	name   string
}

type toolLoopEvidence struct {
	period  int
	repeats int
	calls   map[string]bool
}

func toolCallFingerprint(call toolemulation.ToolCall) string {
	args := call.Arguments
	// Descriptions are UI labels for these execution tools, not execution inputs.
	switch call.Name {
	case "bash", "shell", "terminal", "exec_command", "run_code":
		args = make(map[string]any, len(call.Arguments))
		for k, v := range call.Arguments {
			if k != "description" {
				args[k] = v
			}
		}
	}
	encoded, err := json.Marshal(args)
	if err != nil {
		return ""
	}
	return fmt.Sprintf("%s:%x", call.Name, sha256.Sum256(encoded))
}

// Only paired, completed calls after the latest user instruction are evidence.
// Out-of-order parallel results are restored to assistant call order. Missing or
// ambiguous results break the sequence instead of bridging unrelated work.
func completedToolLoopRecords(messages []ChatMessage) []toolLoopRecord {
	start := 0
	for i := len(messages) - 1; i >= 0; i-- {
		if messages[i].Role == "user" {
			start = i + 1
			break
		}
	}
	var records []toolLoopRecord
	var pending []toolemulation.ToolCall
	results := make(map[string]string)
	for _, message := range messages[start:] {
		switch message.Role {
		case "assistant":
			if len(pending) > 0 {
				records = nil
			}
			pending = message.ToolCalls
			results = make(map[string]string)
		case "tool":
			if message.ToolCallID == "" || len(pending) == 0 {
				records = nil
				continue
			}
			results[message.ToolCallID] = message.Text
			complete := true
			seen := make(map[string]bool)
			for _, call := range pending {
				if _, ok := results[call.ID]; !ok || call.ID == "" || seen[call.ID] {
					complete = false
				}
				seen[call.ID] = true
			}
			if !complete {
				continue
			}
			for _, call := range pending {
				output := strings.ReplaceAll(results[call.ID], "\r\n", "\n")
				records = append(records, toolLoopRecord{call: toolCallFingerprint(call), result: sha256.Sum256([]byte(output)), name: call.Name})
			}
			pending = nil
		}
	}
	if len(pending) > 0 {
		return nil
	}
	return records
}

func (p toolLoopPolicy) detect(messages []ChatMessage) *toolLoopEvidence {
	records := completedToolLoopRecords(messages)
	for period := 1; period <= 4; period++ {
		repeats := p.repeats
		if len(records) < period {
			continue
		}
		cycle := records[len(records)-period:]
		allPolling := true
		for _, record := range cycle {
			allPolling = allPolling && p.pollTools[record.name]
		}
		if allPolling {
			repeats = max(repeats, p.pollRepeats)
		}
		if len(records) < period*repeats {
			continue
		}
		tail := records[len(records)-period*repeats:]
		match := true
		for i, record := range tail {
			if record.call == "" || record != cycle[i%period] {
				match = false
				break
			}
		}
		if match {
			calls := make(map[string]bool)
			for _, record := range cycle {
				calls[record.call] = true
			}
			return &toolLoopEvidence{period: period, repeats: repeats, calls: calls}
		}
	}
	return nil
}

func (e *toolLoopEvidence) blocks(calls []toolemulation.ToolCall) bool {
	for _, call := range calls {
		if e.calls[toolCallFingerprint(call)] {
			return true
		}
	}
	return false
}

// This boundary protects every API and backend. Tool fragments are released by
// the HTTP adapters only from the validated final result, never speculatively.
func (s *Service) generateGuarded(ctx context.Context, req ChatRequest, onDelta func(StreamEvent)) (*ChatResult, error) {
	generate := s.generateWithReconnect
	if s.backend() == BackendRemote {
		generate = s.generateRemote
	}
	evidence := s.toolLoopPolicy.detect(req.Messages)
	req.StreamNativeTools = onDelta != nil
	forward := onDelta
	observe := func(event StreamEvent) {
		if onDelta != nil && event.Type == StreamEventActivity {
			onDelta(event)
		}
	}
	if evidence != nil && onDelta != nil {
		// A suspected cycle may need correction; no preamble can escape first.
		forward = observe
	}
	result, err := generate(ctx, req, forward)
	if err != nil || result == nil || evidence == nil {
		return result, err
	}
	if !evidence.blocks(result.ToolCalls) {
		if onDelta != nil {
			if result.ThoughtText != "" {
				onDelta(StreamEvent{Type: StreamEventThinking, Delta: result.ThoughtText})
			}
			if result.Text != "" {
				onDelta(StreamEvent{Type: StreamEventText, Delta: result.Text})
			}
		}
		return result, nil
	}
	log.Printf("tool_loop_guard: correction period=%d repeats=%d model=%s", evidence.period, evidence.repeats, req.Model)
	// A separate request-local instruction preserves all existing tool results.
	// It is not a fabricated tool result or an execution of the rejected call.
	correction := fmt.Sprintf("\n\nProxy tool-loop guard: the recent completed tool calls repeat a cycle of %d calls at least %d times with identical results. Your latest proposed call was withheld and was NOT executed. Use the existing results. Summarize the findings and finish if sufficient, or choose a different operation/arguments that obtains new information. Do not repeat any call from this unchanged cycle. This is your only correction attempt.", evidence.period, evidence.repeats)
	req.System += correction
	req.toolLoopCorrection = true
	var retryDelta func(StreamEvent)
	if onDelta != nil {
		retryDelta = observe
	}
	corrected, err := generate(ctx, req, retryDelta)
	if err != nil {
		return nil, err
	}
	if corrected == nil {
		return nil, fmt.Errorf("tool-loop correction returned no result")
	}
	if evidence.blocks(corrected.ToolCalls) {
		log.Printf("tool_loop_guard: blocked period=%d repeats=%d model=%s", evidence.period, evidence.repeats, req.Model)
		return nil, &ToolLoopError{Period: evidence.period, Repeats: evidence.repeats}
	}
	// Context occupancy belongs to the final attempt, not the sum of attempts.
	if !corrected.ContextUsageKnown && corrected.UsedTokens <= 0 {
		corrected.UsedTokens = max(0, corrected.InputTokens) + max(0, corrected.OutputTokens)
		corrected.ContextUsageEstimated = true
	}
	// Request usage includes the withheld attempt as well as the correction.
	corrected.InputTokens += result.InputTokens
	corrected.OutputTokens += result.OutputTokens
	corrected.CachedTokens += result.CachedTokens
	corrected.ReasoningTokens += result.ReasoningTokens
	corrected.InputTokensEstimated = corrected.InputTokensEstimated || result.InputTokensEstimated
	corrected.OutputTokensEstimated = corrected.OutputTokensEstimated || result.OutputTokensEstimated
	if onDelta != nil {
		if corrected.ThoughtText != "" {
			onDelta(StreamEvent{Type: StreamEventThinking, Delta: corrected.ThoughtText})
		}
		if corrected.Text != "" {
			onDelta(StreamEvent{Type: StreamEventText, Delta: corrected.Text})
		}
	}
	return corrected, nil
}
