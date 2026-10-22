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
	ModelAliases map[string]string `json:"model_aliases"`

	APIKey              string   `json:"api_key"`
	AdminKey            string   `json:"admin_key"`
	AllowedOrigins      []string `json:"allowed_origins"`
	LogMode             string   `json:"log_mode"`
	LogBodyMaxBytes     int      `json:"log_body_max_bytes"`
	LogMaxBytes         int      `json:"log_max_bytes"`
	LogRetentionDays    int      `json:"log_retention_days"`
	MaxRequestBytes     int      `json:"max_request_bytes"`
	QueueTimeoutSeconds int      `json:"queue_timeout_seconds"`

	MaxConcurrentRequests  int   `json:"max_concurrent_requests"`
	RemoteRetryCount       *int  `json:"remote_retry_count"`
	RemoteRetryBaseDelayMS int   `json:"remote_retry_base_delay_ms"`
	AggregateToolStream    *bool `json:"aggregate_tool_stream"`
}

func (settings RuntimeSettingsFile) Apply(cfg *Config) {
	cfg.ModelAliases = settings.ModelAliases

	cfg.APIKey = settings.APIKey
	cfg.AdminKey = settings.AdminKey
	cfg.AllowedOrigins = settings.AllowedOrigins
	cfg.LogMode = settings.LogMode
	cfg.LogBodyMaxBytes = settings.LogBodyMaxBytes
	cfg.LogMaxBytes = settings.LogMaxBytes
	cfg.LogRetentionDays = settings.LogRetentionDays
	cfg.MaxRequestBytes = settings.MaxRequestBytes
	cfg.QueueTimeoutSeconds = settings.QueueTimeoutSeconds

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
	if key, ok := os.LookupEnv("LINGMA_PROXY_API_KEY"); ok {
		cfg.APIKey = key
	}
	if key, ok := os.LookupEnv("LINGMA_PROXY_ADMIN_KEY"); ok {
		cfg.AdminKey = key
	}
	if cfg.LogMode == "" {
		cfg.LogMode = "errors"
	}
	if cfg.LogBodyMaxBytes == 0 {
		cfg.LogBodyMaxBytes = 65536
	}
	if cfg.LogMaxBytes == 0 {
		cfg.LogMaxBytes = 16 * 1024 * 1024
	}
	if cfg.LogRetentionDays == 0 {
		cfg.LogRetentionDays = 7
	}
	if cfg.MaxRequestBytes == 0 {
		cfg.MaxRequestBytes = 32 * 1024 * 1024
	}
	if cfg.QueueTimeoutSeconds == 0 {
		cfg.QueueTimeoutSeconds = 30
	}

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
	for alias, target := range cfg.ModelAliases {
		if strings.TrimSpace(alias) == "" || strings.TrimSpace(target) == "" || alias == target {
			return fmt.Errorf("模型别名和目标必须有效且不同")
		}
		if _, chained := cfg.ModelAliases[target]; chained {
			return fmt.Errorf("模型别名不支持链式映射")
		}
	}

	if cfg.LogMode != "metadata" && cfg.LogMode != "errors" && cfg.LogMode != "full" {
		return fmt.Errorf("日志模式必须为 metadata / errors / full")
	}
	if cfg.LogBodyMaxBytes < 1024 || cfg.LogBodyMaxBytes > 1024*1024 {
		return fmt.Errorf("单条正文上限必须在 1 KiB–1 MiB 之间")
	}
	if cfg.LogMaxBytes < 1024*1024 || cfg.LogMaxBytes > 128*1024*1024 {
		return fmt.Errorf("历史记录容量必须在 1–128 MiB 之间")
	}
	if cfg.LogRetentionDays < 1 || cfg.LogRetentionDays > 365 {
		return fmt.Errorf("保留天数必须在 1–365 之间")
	}
	if cfg.MaxRequestBytes < 1024 || cfg.MaxRequestBytes > 128*1024*1024 {
		return fmt.Errorf("请求体上限必须在 1 KiB–128 MiB 之间")
	}
	if cfg.QueueTimeoutSeconds < 1 || cfg.QueueTimeoutSeconds > 600 {
		return fmt.Errorf("排队上限必须在 1–600 秒之间")
	}
	for _, origin := range cfg.AllowedOrigins {
		if origin == "*" {
			return fmt.Errorf("浏览器来源需要填写明确的 Origin，不能使用 *")
		}
	}

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

func (s *Service) RuntimeConfig() Config { s.mu.Lock(); defer s.mu.Unlock(); return s.cfg }
