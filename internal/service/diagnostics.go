package service

import (
	"context"
	"errors"
	"net"
	"strings"
	"time"

	"lingma-ipc-proxy/internal/remote"
)

type Diagnostic struct {
	Code    string `json:"code"`
	Status  string `json:"status"`
	Message string `json:"message"`
	Action  string `json:"action,omitempty"`
}

func DiagnoseError(err error) Diagnostic {
	d := Diagnostic{Code: "upstream_error", Status: "error", Message: "上游调用失败", Action: "检查网络与请求详情，导出脱敏反馈包"}
	if err == nil {
		return Diagnostic{Code: "ok", Status: "ok", Message: "检查通过"}
	}
	var up *remote.UpstreamError
	if errors.As(err, &up) {
		switch {
		case up.QuotaExhausted:
			return Diagnostic{"quota_exhausted", "error", "上游额度已耗尽", "检查账号额度；重试不会恢复额度"}
		case up.StatusCode == 401 || up.StatusCode == 403:
			return Diagnostic{"authentication", "error", "上游登录失效或账号没有访问权限", "在 Lingma / QoderCN 检查登录和模型权限，再刷新检测"}
		case up.StatusCode == 429:
			return Diagnostic{"rate_limited", "warning", "上游临时限流", "等待重试；必要时降低并发数"}
		case up.StatusCode >= 500:
			return Diagnostic{"upstream_unavailable", "error", "上游服务暂时不可用", "稍后重试，或检查允许的模型回退"}
		}
	}
	var network net.Error
	switch {
	case errors.Is(err, context.Canceled):
		d = Diagnostic{"canceled", "warning", "请求已取消", "可按需重新发起请求"}
	case errors.Is(err, context.DeadlineExceeded):
		d = Diagnostic{"timeout", "error", "等待上游超时", "检查网络、IPC 运行状态和超时设置"}
	case errors.As(err, &network):
		d = Diagnostic{"network", "error", "网络连接失败", "检查网络、代理地址及上游服务"}
	case strings.Contains(strings.ToLower(err.Error()), "credential"):
		d = Diagnostic{"credentials_missing", "error", "无法读取登录凭据", "在 Lingma / QoderCN 登录，或选择有效的导出凭据文件"}
	}
	return d
}

// Diagnose lists models but never sends a generation probe unless explicitly requested.
func (s *Service) Diagnose(ctx context.Context, generate bool) []Diagnostic {
	out := []Diagnostic{}
	if s.cfg.Backend == BackendRemote {
		cred, err := remote.LoadCredential(s.cfg.RemoteAuthFile)
		if err != nil {
			return []Diagnostic{DiagnoseError(err)}
		}
		if remote.IsExpired(cred, 0) {
			return []Diagnostic{{"credentials_expired", "error", "登录凭据已过期", "在 Lingma / QoderCN 重新登录；导出凭据需要重新导出"}}
		}
		out = append(out, Diagnostic{"credentials", "ok", "登录凭据可读取；有效期以服务端校验为准", ""})
	}
	models, err := s.ListModels(ctx)
	if err != nil {
		return append(out, DiagnoseError(err))
	}
	found := false
	for _, model := range models {
		if model.ID == s.DefaultModel() || strings.EqualFold(model.Name, s.DefaultModel()) {
			found = true
		}
	}
	if len(models) == 0 {
		return append(out, Diagnostic{"models_empty", "warning", "当前账号未返回模型", "检查账号权限和远端地址"})
	}
	out = append(out, Diagnostic{"catalog", "ok", "连接与模型目录检查通过；尚未验证生成", ""})
	if !found {
		out = append(out, Diagnostic{"default_model", "warning", "默认模型未出现在当前目录", "到模型页选择当前账号可用的模型"})
	}
	if generate {
		_, err := s.Generate(ctx, ChatRequest{Model: s.DefaultModel(), Messages: []ChatMessage{{Role: "user", Text: "Reply with OK."}}, MaxTokens: 16})
		d := DiagnoseError(err)
		if err == nil {
			d = Diagnostic{"generation", "ok", "最小生成测试通过", ""}
		}
		out = append(out, d)
	}
	return out
}

func (s *Service) DiagnosticTimeout() time.Duration { return 30 * time.Second }
