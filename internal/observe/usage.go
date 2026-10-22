package observe

import (
	"bytes"
	"encoding/json"
)

// UsageCapture extracts final usage independently from retained request bodies.
type UsageCapture struct {
	Input    int
	Output   int
	pending  []byte
	skipping bool
}

func (u *UsageCapture) json(raw []byte) {
	var value any
	if json.Unmarshal(raw, &value) != nil {
		return
	}
	var walk func(any)
	walk = func(v any) {
		switch x := v.(type) {
		case map[string]any:
			if usage, ok := x["usage"].(map[string]any); ok {
				number := func(key string) int { f, _ := usage[key].(float64); return max(0, int(f)) }
				if _, ok := usage["prompt_tokens"]; ok {
					u.Input = number("prompt_tokens")
				} else if _, ok := usage["input_tokens"]; ok {
					u.Input = number("input_tokens")
				}
				if _, ok := usage["completion_tokens"]; ok {
					u.Output = number("completion_tokens")
				} else if _, ok := usage["output_tokens"]; ok {
					u.Output = number("output_tokens")
				}
			}
			for _, child := range x {
				walk(child)
			}
		case []any:
			for _, child := range x {
				walk(child)
			}
		}
	}
	walk(value)
}
func (u *UsageCapture) Write(b []byte) {
	if json.Valid(b) {
		u.json(b)
		return
	}
	for len(b) > 0 {
		i := bytes.IndexByte(b, '\n')
		part := b
		if i >= 0 {
			part = b[:i]
		}
		if !u.skipping {
			if len(u.pending)+len(part) > 256*1024 {
				u.pending = nil
				u.skipping = true
			} else {
				u.pending = append(u.pending, part...)
			}
		}
		if i < 0 {
			return
		}
		if !u.skipping {
			u.json(bytes.TrimSpace(bytes.TrimPrefix(u.pending, []byte("data: "))))
		}
		u.pending = nil
		u.skipping = false
		b = b[i+1:]
	}
}
