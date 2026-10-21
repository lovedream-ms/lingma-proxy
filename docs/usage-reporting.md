# Usage 与工具调用修复

本地修复基于上游 v1.6.12；构建、版本、安装和桌面操作统一见 [README](../README.md)。本文保留协议行为与验证要点，不再维护第二套构建说明。

## Usage 行为

- 流式 OpenAI Chat Completions 在请求显式设置 `stream_options.include_usage: true` 时，于结束帧之后、`[DONE]` 之前发送 `choices: []` 的 usage 帧；未请求时不额外发送。
- 非流式响应返回 usage 对象。优先使用上游返回的计数，缺失时使用本地估算；估算值不能称为 tokenizer 精确计数。
- 可用的缓存、推理 token 明细通过 `prompt_tokens_details.cached_tokens` 与 `completion_tokens_details.reasoning_tokens` 传递。
- `prompt_tokens` / `input_tokens` 表达本次请求的输入，`total_tokens` 等于本次输入与输出之和。IPC 的会话占用不覆盖本次输入计数；提供占用与上限时，另通过 `usage.context_window.used_tokens` / `limit_tokens` 返回。
- 模型上下文与图片能力优先使用上游模型列表字段；缺失时才使用已知模型映射，未知模型不虚构上下文上限。
- 模型的 `context_window` 表达默认总窗口，`max_context_length` 和兼容 Harness 的 `contextWindow` 表达最大可选总窗口，`context_options` 保留上游可选窗口，`max_input_tokens` 单独表达输入预算。新版 IPC 优先读取 `contextConfig` 中默认选项的 `tokenCount`，不会把 180000 的输入预算当成 200000 的总窗口；未知总窗口返回 0。
- `max_input_tokens` 保留上游模型定义值；支持扩展上下文的模型，此值可能大于当前默认窗口。当前会话实际占用与生效上限以 `usage.context_window.used_tokens` / `limit_tokens` 为准。
- 上游多帧 usage 按累计快照合并，保留明确的零值；输出估算包含工具调用和推理文本，缓存与推理明细不超过所属总数。Responses 接口采用相同计数口径，桌面统计不会重复相加不同协议的同义字段。
- 使用估算回退时，`usage.token_count_source` 标明输入与输出各自来自 `estimated` 或 `upstream`。IPC 的会话占用可包含客户端附加指令和历史，不能视为本次提交文本的精确计数。

客户端可用 `completion_tokens / 实测耗时` 计算 TPS，用输入 token 与模型上下文大小估算占用比例。计数或上下文大小存在回退时，客户端显示也具有相应估算性质。

## 工具调用兼容

保留只有 tool_calls 的 assistant 消息、tool_call_id 和空工具结果，避免多轮历史丢失。原生工具调用和 action block 使用一致的名称、参数与 schema 校验。

未满足工具约定时最多进行一次修复；在可重试阶段暂存短规划文字，避免把放弃的前言流式发送给客户端。重复的失败调用、`tool_choice=none` 和禁止并行的约束均须保留。

客户端仅暴露 `run_code` 时，只能按已声明工具及上下文允许的名称转换调用，不应把任意未知工具名视作可执行工具。使用真实客户端时，仍需验证客户端执行工具与返回结果的完整往返。

## 实现位置

| 源文件 | 负责内容 |
| --- | --- |
| `internal/httpapi/server.go` | OpenAI/Anthropic 响应、SSE 顺序、usage 对象和上下文信息 |
| `internal/remote/client.go` | 上游 usage 解析、认证请求头和 SSE 数据处理 |
| `internal/service/service.go` | usage 传递、工具历史、修复边界和重试 |
| `internal/toolemulation/toolemulation.go` | action 解析、工具名称与参数归一化 |
| `internal/utils/token_estimator.go` | 缺失上游计数时的估算辅助逻辑 |

路径均相对仓库根目录。有效回归测试放在对应包的 `_test.go` 中；历史测试原型移入缓存归档，避免多包混放破坏 Go 测试发现。

## 最小验证

先以隔离端口 8096 启动控制台版，再请求流式 usage：

```bash
curl -N http://127.0.0.1:8096/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -d '{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"只回复 OK"}]}'
```

检查结束帧、usage、`[DONE]` 的顺序；分别测试显式 opt-out、非流式响应、上游精确计数与回退估算。工具测试应覆盖历史 ID、空结果、参数修复、声明约束和失败重试边界。不得把合成上游的成功结果写成真实模型或真实客户端的验收结果。

- 当前 DeepSeek Harness 的在线 `readListing` 不读取图像能力，需在其模型配置 `input` 中声明 `["text", "image"]`；代理返回 `supports_vision`、`input_modalities` 和 `capabilities.vision`，但不能替代客户端解析。

- 远程原生工具调用与推理按上游分片实时转发，避免结束后集中回放造成客户端 TPS 虚高；已发送调用出错时终止，不重试重放同一个工具。
- 缺少上游上下文占用时，`usage.context_window.used_tokens` 以本次输入（已含缓存）加输出估算，标记 `source: estimated`；目录提供默认上限时返回 `limit_tokens`。显式上游占用 0 仍保留。Harness 在线上下文按标准输入和缓存字段自行计算，不读取此扩展。
- `output_speed_tokens_per_second` 使用真实首分片至上游完成的秒数；只有一个有效分片时无法测得生成速度，省略该字段。Harness 的 TPS 来自客户端收到分片的计时，历史会话统计不会被代理更新追溯修正。

- SSE 封套 `statusCodeValue: 429` 属于已解析的上游错误，不是 JSON 解析失败。错误的 JSON 字符串 `details` 会递归提取内层原因，HTTP 与 SSE 简单/多值 `Retry-After` 均参与等待。明确配额不足不自动重试，普通临时限流有限退避；开始输出后不重放。非流式保留 429 和等待头；流式已开始时在错误帧提供 `status`、`code`、`retry_after_seconds`，保留真实配额错误代码，但不添加本地 `retryable: false` 禁用标记。

- 不缓存配额失败，不设置固定请求间隔或上游串行锁；429/配额错误可按现有配置尝试其他模型，回退前遵守上游明确的等待指令。默认4并发，用户可通过README所述环境变量调整。
