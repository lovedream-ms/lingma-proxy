package modelmeta

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
)

// Model is the common model catalogue format used by IPC and HTTP upstreams.
type Model struct {
	ID                  string
	Name                string
	Scene               string
	MaxContextLength    int
	ContextOptions      []int
	ContextWindow       int
	ContextWindowKnown  bool
	MaxInputTokens      int
	SupportsVision      bool
	SupportsVisionKnown bool
}

// ContextWindow provides compatibility hints only for recognized model aliases.
// Unknown models deliberately have no advertised limit.
func ContextWindow(ids ...string) int {
	windows := map[string]int{
		"kmodel": 256000, "kimi": 256000, "kimi-k2": 256000, "kimi-k2.6": 256000, "kimi-k2-6": 256000,
		"mmodel": 204800, "minimax": 204800, "minimax-m2.7": 204800,
		"qwen3-coder": 256000, "dashscope_qwen3_coder": 256000,
		"qwen3-max": 256000, "dashscope_qwen_max_latest": 256000,
		"qwen3.6-plus": 204800, "dashscope_qmodel": 204800,
	}
	for _, id := range ids {
		if n, ok := windows[strings.ToLower(strings.TrimSpace(id))]; ok {
			return n
		}
	}
	return 0
}

func Vision(ids ...string) bool {
	for _, id := range ids {
		switch strings.ToLower(strings.TrimSpace(id)) {
		case "kmodel", "kimi-k2.6", "kimi-k2-6", "qwen3.6-plus", "dashscope_qmodel":
			return true
		}
	}
	return false
}

// Parse collects all supported scenes and envelopes, deduplicating by the
// upstream routing ID rather than a human display name or scene count.
func Parse(raw any) []Model {
	seen := map[string]int{}
	var out []Model
	var walk func(string, any)
	walk = func(scene string, value any) {
		switch v := value.(type) {
		case []any:
			for _, item := range v {
				walk(scene, item)
			}
		case map[string]any:
			id := text(v, "key", "id", "modelId", "model_id")
			name := text(v, "display_name", "displayName", "name", "label", "title")
			if id == "" && name != "" {
				id = text(v, "model")
			}
			if id != "" && name != "" {
				if enabled, ok := v["enable"].(bool); ok && !enabled {
					return
				}
				if enabled, ok := v["enabled"].(bool); ok && !enabled {
					return
				}
				model := Model{ID: id, Name: name, Scene: scene, ContextWindow: ContextWindow(id, name, text(v, "model")), SupportsVision: Vision(id, name, text(v, "model"))}
				for _, key := range []string{"max_input_tokens", "maxInputTokens"} {
					if n := positiveInt(v[key]); n > 0 {
						model.MaxInputTokens = n
						break
					}
				}
				for _, key := range []string{"context_window", "contextWindow", "max_context_length", "maxContextLength", "max_context_tokens"} {
					if n := positiveInt(v[key]); n > 0 {
						model.ContextWindow = n
						model.ContextWindowKnown = true
						break
					}
				}
				for _, key := range []string{"contextConfig", "context_config"} {
					if configs, ok := v[key].(map[string]any); ok {
						for _, value := range configs {
							config, ok := value.(map[string]any)
							if !ok {
								continue
							}
							count := positiveInt(config["tokenCount"])
							if count == 0 {
								count = positiveInt(config["token_count"])
							}
							if count == 0 {
								continue
							}
							model.ContextOptions = append(model.ContextOptions, count)
							selected, _ := config["isDefault"].(bool)
							snakeSelected, _ := config["is_default"].(bool)
							if (selected || snakeSelected) && !model.ContextWindowKnown {
								model.ContextWindow = count
								model.ContextWindowKnown = true
							}
						}
					}
				}
				sort.Ints(model.ContextOptions)
				model.MaxContextLength = model.ContextWindow
				for _, count := range model.ContextOptions {
					if count > model.MaxContextLength {
						model.MaxContextLength = count
					}
				}
				for _, key := range []string{"max_context_length", "maxContextLength", "max_context_tokens"} {
					if count := positiveInt(v[key]); count > model.MaxContextLength {
						model.MaxContextLength = count
					}
				}
				for _, key := range []string{"supports_vision", "supportsVision", "is_vl", "isVl", "vision"} {
					if b, ok := v[key].(bool); ok {
						model.SupportsVision = b
						model.SupportsVisionKnown = true
						break
					}
				}
				if i, ok := seen[id]; ok {
					if !out[i].ContextWindowKnown && model.ContextWindowKnown {
						out[i].MaxContextLength = model.MaxContextLength
						out[i].ContextWindow = model.ContextWindow
						out[i].ContextWindowKnown = true
					}
					if !out[i].SupportsVisionKnown && model.SupportsVisionKnown {
						out[i].SupportsVision = model.SupportsVision
						out[i].SupportsVisionKnown = true
					}
					if model.MaxContextLength > out[i].MaxContextLength {
						out[i].MaxContextLength = model.MaxContextLength
					}
					if len(model.ContextOptions) > len(out[i].ContextOptions) {
						out[i].ContextOptions = model.ContextOptions
					}
					if out[i].MaxInputTokens == 0 {
						out[i].MaxInputTokens = model.MaxInputTokens
					}
					return
				}
				seen[id] = len(out)
				out = append(out, model)
				return
			}
			// Process scenes in a stable order so duplicates cannot change capabilities.
			keys := make([]string, 0, len(v))
			for k := range v {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, key := range keys {
				next := scene
				switch strings.ToLower(key) {
				case "chat", "assistant", "developer", "experts", "quest", "inline":
					next = key
				}
				walk(next, v[key])
			}
		}
	}
	walk("", raw)
	return out
}

func text(v map[string]any, keys ...string) string {
	for _, key := range keys {
		if s, ok := v[key].(string); ok && strings.TrimSpace(s) != "" {
			return strings.TrimSpace(s)
		}
	}
	return ""
}
func positiveInt(v any) int {
	var s string
	switch n := v.(type) {
	case float64:
		if n <= 0 || n != float64(int(n)) {
			return 0
		}
		return int(n)
	case int:
		if n > 0 {
			return n
		}
		return 0
	case json.Number:
		s = string(n)
	case string:
		s = n
	default:
		return 0
	}
	n, err := strconv.Atoi(strings.TrimSpace(s))
	if err != nil || n <= 0 {
		return 0
	}
	return n
}

// ParseJSON retains JSON number precision when reading catalogue responses.
func ParseJSON(body []byte) ([]Model, error) {
	decoder := json.NewDecoder(strings.NewReader(string(body)))
	decoder.UseNumber()
	var raw any
	if err := decoder.Decode(&raw); err != nil {
		return nil, fmt.Errorf("parse model catalogue: %w", err)
	}
	models := Parse(raw)
	if len(models) == 0 {
		return nil, fmt.Errorf("model catalogue contains no enabled models")
	}
	return models, nil
}
