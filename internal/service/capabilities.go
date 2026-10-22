package service

type Capability struct {
	Status string `json:"status"`
	Detail string `json:"detail"`
}

// CapabilityProfile describes the adapter path, not a promise of model quality.
func (s *Service) CapabilityProfile() map[string]Capability {
	tools := Capability{"emulated", "当前常规工具请求通过提示和 action block 适配，实际可靠性需要按模型验证"}
	images := Capability{"requires_ipc", "当前图片路径需要可连接的本地 Lingma / QoderCN IPC；目录视觉标记不表示当前环境已验证"}
	reasoning := Capability{"unavailable", "远端未提供独立思考块，无法保证单独展示"}
	if s.backend() == BackendIPC {
		tools = Capability{"emulated", "通过提示和 action block 模拟工具调用"}
		images = Capability{"requires_ipc", "需要运行中的本地 IPC，图像能力取决于模型与运行时"}
		reasoning = Capability{"conditional", "仅当 IPC 上游实际输出独立思考块时转发"}
	}
	return map[string]Capability{
		"tools": tools, "images": images, "reasoning": reasoning,
		"structured_output": {Status: "unsupported", Detail: "接收 response_format 字段，但未保证 JSON Schema 约束，不应依赖严格结构化输出"},
		"context":           {Status: "catalog_or_hint", Detail: "目录数据优先；部分容量来自兼容提示，未知上限保持未知"},
	}
}
