package httpapi

import (
	"encoding/json"
	"regexp"
	"time"
	"unicode/utf8"
)

var secretLogPattern = regexp.MustCompile(`(?i)("(?:api_key|admin_key|password|access_token|refresh_token|authorization|cookie)"\s*:\s*")[^"]*(")`)
var bearerLogPattern = regexp.MustCompile(`(?i)Bearer\s+[A-Za-z0-9._~+/=-]+`)

func limitRecorded(text string, limit int) string {
	text = secretLogPattern.ReplaceAllString(text, "${1}[REDACTED]${2}")
	text = bearerLogPattern.ReplaceAllString(text, "Bearer [REDACTED]")
	if len(text) <= limit {
		return text
	}
	marker := "\n[TRUNCATED]"
	if limit < len(marker) {
		return ""
	}
	text = text[:limit-len(marker)]
	for !utf8.ValidString(text) && len(text) > 0 {
		text = text[:len(text)-1]
	}
	return text + marker
}

func SanitizeLogBody(text string, limit int) string {
	return limitRecorded(sanitizeRecordedBody([]byte(text)), limit)
}

func (s *Server) pruneRecordsLocked() {
	cfg := s.svc.RuntimeConfig()
	cutoff := time.Now().Add(-time.Duration(cfg.LogRetentionDays) * 24 * time.Hour)
	total := 0
	start := len(s.records)
	for i := len(s.records) - 1; i >= 0; i-- {
		record := s.records[i]
		at, _ := time.Parse(time.RFC3339, record.Time)
		raw, _ := json.Marshal(record)
		if i < len(s.records)-200 || at.Before(cutoff) || total+len(raw) > cfg.LogMaxBytes {
			break
		}
		total += len(raw)
		start = i
	}
	s.records = append([]debugRequestRecord(nil), s.records[start:]...)
}
