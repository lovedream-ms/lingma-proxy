package main

import (
	"context"
	"lingma-ipc-proxy/internal/observe"
	"lingma-ipc-proxy/internal/service"
)

func (a *App) RunDiagnostics(generate bool) []service.Diagnostic {
	a.mu.RLock()
	cfg := a.cfg
	running := a.running
	a.mu.RUnlock()
	out := []service.Diagnostic{}
	if !running {
		out = append(out, service.Diagnostic{Code: "listener", Status: "warning", Message: "代理未启动；以下检查使用当前已保存配置", Action: "启动代理后再连接客户端"})
	}
	svc := service.New(cfg)
	defer svc.Close()
	ctx, cancel := context.WithTimeout(context.Background(), svc.DiagnosticTimeout())
	defer cancel()
	return append(out, svc.Diagnose(ctx, generate)...)
}

func (a *App) GetActivity() []observe.Snapshot {
	a.mu.RLock()
	server := a.server
	a.mu.RUnlock()
	if server == nil {
		return []observe.Snapshot{}
	}
	return server.Activity()
}
