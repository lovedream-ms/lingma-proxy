package service

import (
	"fmt"
	"os"
	"strconv"
	"strings"
)

// RuntimeSettingsFile is shared by desktop and console config readers.
// Pointers preserve explicit zero retries and false streaming overrides.
type RuntimeSettingsFile struct {
	MaxConcurrentRequests  int   `json:"max_concurrent_requests"`
	RemoteRetryCount       *int  `json:"remote_retry_count"`
	RemoteRetryBaseDelayMS int   `json:"remote_retry_base_delay_ms"`
	AggregateToolStream    *bool `json:"aggregate_tool_stream"`
}

func (settings RuntimeSettingsFile) Apply(cfg *Config) {
	if settings.MaxConcurrentRequests != 0 {
		cfg.MaxConcurrentRequests = settings.MaxConcurrentRequests
	}
	if settings.RemoteRetryCount != nil {
		cfg.RemoteRetryCount = settings.RemoteRetryCount
	}
	if settings.RemoteRetryBaseDelayMS != 0 {
		cfg.RemoteRetryBaseDelayMS = settings.RemoteRetryBaseDelayMS
	}
	if settings.AggregateToolStream != nil {
		cfg.AggregateToolStream = settings.AggregateToolStream
	}
}

func NormalizeRuntimeSettings(cfg Config) Config {
	if cfg.MaxConcurrentRequests == 0 {
		cfg.MaxConcurrentRequests = 4
		if value, err := strconv.Atoi(os.Getenv("LINGMA_PROXY_MAX_CONCURRENT")); err == nil && value > 0 {
			cfg.MaxConcurrentRequests = min(value, 16)
		}
	}
	if cfg.RemoteRetryCount == nil {
		retries := 3
		cfg.RemoteRetryCount = &retries
	}
	if cfg.RemoteRetryBaseDelayMS == 0 {
		cfg.RemoteRetryBaseDelayMS = 1000
	}
	if cfg.AggregateToolStream == nil {
		value := strings.ToLower(strings.TrimSpace(os.Getenv("LINGMA_AGGREGATE_TOOL_STREAM")))
		enabled := value == "1" || value == "true" || value == "yes" || value == "on"
		cfg.AggregateToolStream = &enabled
	}
	return cfg
}

func (cfg Config) ValidateRuntimeSettings() error {
	if cfg.MaxConcurrentRequests < 1 || cfg.MaxConcurrentRequests > 16 {
		return fmt.Errorf("最大并发数必须在 1–16 之间")
	}
	if cfg.RemoteRetryCount == nil || *cfg.RemoteRetryCount < 0 || *cfg.RemoteRetryCount > 10 {
		return fmt.Errorf("远程重试次数必须在 0–10 之间")
	}
	if cfg.RemoteRetryBaseDelayMS < 100 || cfg.RemoteRetryBaseDelayMS > 60000 {
		return fmt.Errorf("初始退避时间必须在 100–60000 毫秒之间")
	}
	return nil
}

func (s *Service) MaxConcurrentRequests() int { return max(1, min(16, s.cfg.MaxConcurrentRequests)) }
func (s *Service) AggregateToolStream() *bool { return s.cfg.AggregateToolStream }
