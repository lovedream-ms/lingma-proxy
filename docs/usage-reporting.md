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

### 工具循环保护（v1.6.14）

代理从最近一条 user 指令之后的历史中，按调用 ID 配对已完成的调用与结果。连续出现三轮相同周期（周期长度 1～4），并且各位置的执行参数和结果都不变时，下一次重复调用会被暂扣。执行工具的 `description` 标签不参与比较；其他参数及结果中的数字、时间均保留，不能把结果有变化的轮询当成静止循环。新指令重置判断范围，缺失的调用结果中断证据，检测不共享不同聊天或子代理的状态。

模型可以直接总结或选择新操作。若仍请求执行循环中的相同操作，代理保留历史结果并追加一次纠偏指令；纠偏后仍重复则返回 `tool_loop_detected`，并附带 `retryable: false`、周期与重复次数。非流式和聚合响应使用 HTTP 400；已开始的 SSE 使用错误事件结束。客户端应停止自动重试该错误。代理保证被拦截调用不会下发，但 Harness 是否自动暂停子代理仍需真实客户端验收。

原生工具调用在参数完整、归一化和循环检查通过后才发送，不能逐片段提前执行。普通文本保持流式；已有循环证据的响应先暂存，防止纠偏前言泄漏。工具片段的首包会相应推迟到校验完成。纠偏 usage 累计两次生成的消耗，上下文占用仍使用最后一次生成的数据。

启动前可通过环境变量调整阈值（修改后重启代理）：

| 环境变量 | 默认值 | 含义 |
| --- | --- | --- |
| `LINGMA_TOOL_LOOP_REPEATS` | `3` | 普通调用的相同周期次数，范围 3～20 |
| `LINGMA_TOOL_LOOP_POLL_REPEATS` | `12` | 已指定轮询工具的有限额度，范围 3～100，不低于普通阈值 |
| `LINGMA_TOOL_LOOP_POLL_TOOLS` | `wait,poll,wait_for_completion,check_job_status` | 逗号分隔的精确工具名；显式空字符串取消额外额度 |

通用 `bash` / `run_code` 不会自动获得轮询豁免；若客户端把状态查询封装在这些工具中，可明确配置工具名以提高额度，但对该工具的其他重复调用也会放宽。当前保护覆盖相同执行参数、相同结果的连续周期，不保证识别改写命令、动态时间戳或更长的不规则语义循环。

## 实现位置

| 源文件 | 负责内容 |
| --- | --- |
| `internal/httpapi/server.go` | OpenAI/Anthropic 响应、SSE 顺序、usage 对象和上下文信息 |
| `internal/remote/client.go` | 上游 usage 解析、认证请求头和 SSE 数据处理 |
| `internal/service/service.go` | usage 传递、工具历史、修复边界和重试 |
| `internal/service/tool_loop.go` | 已完成调用/结果配对、周期检测、一次纠偏和下发前拦截 |
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
