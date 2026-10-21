package utils

import (
	"strings"
	"unicode"
)

// TokenEstimator provides optimized token estimation functionality
type TokenEstimator struct{}

// EstimateTokens approximates a tokenizer without shipping one: CJK and other
// full-width scripts cost roughly one token per character while ASCII costs
// roughly one token per four characters. It replaces a flat rune/N heuristic
// that under-counted Chinese prompts by a factor of two or more.
func (te *TokenEstimator) EstimateTokens(text string) int {
	text = strings.TrimSpace(text)
	if text == "" {
		return 1
	}

	// Use a more efficient approach with pre-allocated counters
	var cjkCount, asciiCount int

	for _, r := range text {
		if te.isWideRuneOptimized(r) {
			cjkCount++
		} else {
			asciiCount++
		}
	}

	// Calculate token estimate with better coefficients
	keep := float64(cjkCount)*0.7 + float64(asciiCount)*0.25
	return max(1, 1+int(keep))
}

// EstimateTokensOrZero estimates tokens but reports empty text as zero.
func (te *TokenEstimator) EstimateTokensOrZero(text string) int {
	if strings.TrimSpace(text) == "" {
		return 0
	}
	return te.EstimateTokens(text)
}

// isWideRuneOptimized reports whether a rune belongs to a script that byte-pair
// tokenizers usually emit as its own token. This is an optimized version
// using a lookup table for common ranges.
func (te *TokenEstimator) isWideRuneOptimized(r rune) bool {
	// Use a more efficient range checking with early returns
	switch {
	case r >= 0x4E00 && r <= 0x9FFF: // CJK Unified Ideographs
		return true
	case r >= 0x3400 && r <= 0x4DBF: // CJK Extension A
		return true
	case r >= 0x2E80 && r <= 0x2FDF: // CJK Radicals Supplement
		return true
	case r >= 0x3040 && r <= 0x309F: // Hiragana
		return true
	case r >= 0x30A0 && r <= 0x30FF: // Katakana
		return true
	case r >= 0xAC00 && r <= 0xD7AF: // Hangul Syllables
		return true
	case r >= 0x1100 && r <= 0x11FF: // Hangul Jamo
		return true
	case r >= 0x3130 && r <= 0x318F: // Hangul Compatibility Jamo
		return true
	case r >= 0xF900 && r <= 0xFAFF: // CJK Compatibility Ideographs
		return true
	case r >= 0xFF00 && r <= 0xFFEF: // Halfwidth and Fullwidth Forms
		return true
	case r >= 0x20000 && r <= 0x2FA1F: // CJK Extension B and beyond
		return true
	case r >= 0x2F00 && r <= 0x2FDF: // Kangxi Radicals
		return true
	case r >= 0x2FF0 && r <= 0x2FFF: // Ideographic Description Characters
		return true
	case r >= 0x3100 && r <= 0x312F: // Bopomofo
		return true
	case r >= 0x31A0 && r <= 0x31BF: // Bopomofo Extended
		return true
	case r >= 0x31F0 && r <= 0x31FF: // Katakana Phonetic Extensions
		return true
	case r >= 0xFE30 && r <= 0xFE4F: // CJK Compatibility Forms
		return true
	case r >= 0xFE50 && r <= 0xFE6F: // Small Form Variants
		return true
	case r >= 0x1F000 && r <= 0x1F9FF: // Symbols and Pictographs Extended-A
		return true
	default:
		return unicode.Is(unicode.Han, r) ||
			unicode.Is(unicode.Hiragana, r) ||
			unicode.Is(unicode.Katakana, r) ||
			unicode.Is(unicode.Hangul, r)
	}
}

// max is a helper function to return the maximum of two integers
func max(a, b int) int {
	if a > b {
		return a
	}
	return b
}
