package main

import (
	"sort"
	"time"
)

type UsageTrend struct {
	Date     string `json:"date"`
	Model    string `json:"model"`
	Requests int    `json:"requests"`
	Success  int    `json:"success"`
	Tokens   int    `json:"tokens"`
}

// Trends cover retained history only. Cumulative totals remain separate.
func (a *App) GetUsageTrends() []UsageTrend {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.pruneHistoryLocked()
	groups := map[string]*UsageTrend{}
	for _, r := range a.requests {
		if !isUsageBearingRequest(r.Path) {
			continue
		}
		at, err := time.Parse(time.RFC3339, r.CreatedAt)
		if err != nil {
			continue
		}
		date := at.Local().Format("2006-01-02")
		key := date + "\x00" + r.Model
		if groups[key] == nil {
			groups[key] = &UsageTrend{Date: date, Model: r.Model}
		}
		g := groups[key]
		g.Requests++
		if r.StatusCode >= 200 && r.StatusCode < 300 && r.Diagnosis.Status != "error" {
			g.Success++
		}
		g.Tokens += r.TotalTokens
	}
	out := []UsageTrend{}
	for _, g := range groups {
		out = append(out, *g)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Date != out[j].Date {
			return out[i].Date > out[j].Date
		}
		return out[i].Model < out[j].Model
	})
	return out
}
