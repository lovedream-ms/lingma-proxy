package main

import (
	"encoding/json"
	"lingma-ipc-proxy/internal/httpapi"
	"lingma-ipc-proxy/internal/service"
	"time"
)

// Caller holds a.mu. Usage totals are independent from history retention.
func (a *App) pruneHistoryLocked() {
	cfg := service.NormalizeRuntimeSettings(a.cfg)
	cutoff := time.Now().Add(-time.Duration(cfg.LogRetentionDays) * 24 * time.Hour)
	logs := []AppLog{}
	bytes := 0
	for i := len(a.logs) - 1; i >= 0; i-- {
		record := a.logs[i]
		at, ok := parseRecordTimestamp(record.CreatedAt)
		if ok && at.Before(cutoff) {
			continue
		}
		record.Message = httpapi.SanitizeLogBody(record.Message, cfg.LogBodyMaxBytes)
		raw, _ := json.Marshal(record)
		if bytes+len(raw) > cfg.LogMaxBytes/2 {
			break
		}
		bytes += len(raw)
		logs = append(logs, record)
	}
	for i, j := 0, len(logs)-1; i < j; i, j = i+1, j-1 {
		logs[i], logs[j] = logs[j], logs[i]
	}
	a.logs = logs
	requests := []RequestRecord{}
	for i := len(a.requests) - 1; i >= 0; i-- {
		record := a.requests[i]
		at, ok := parseRecordTimestamp(record.CreatedAt)
		if ok && at.Before(cutoff) {
			continue
		}
		failed := record.StatusCode >= 400 || record.Diagnosis.Status == "error"
		if cfg.LogMode == "metadata" || cfg.LogMode == "errors" && !failed {
			record.ReqBody = ""
			record.RespBody = ""
		} else {
			record.ReqBody = httpapi.SanitizeLogBody(record.ReqBody, cfg.LogBodyMaxBytes)
			record.RespBody = httpapi.SanitizeLogBody(record.RespBody, cfg.LogBodyMaxBytes)
		}
		record.HasReqBody = record.ReqBody != ""
		record.HasRespBody = record.RespBody != ""
		raw, _ := json.Marshal(record)
		if bytes+len(raw) > cfg.LogMaxBytes {
			break
		}
		bytes += len(raw)
		requests = append(requests, record)
	}
	for i, j := 0, len(requests)-1; i < j; i, j = i+1, j-1 {
		requests[i], requests[j] = requests[j], requests[i]
	}
	a.requests = requests
}
