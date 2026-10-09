# `POST /v1/chat/completions` 从不返回 token `usage`（流式忽略 `stream_options.include_usage`）

## 概述

本代理的 OpenAI 兼容 chat completions 无法提供可用的 token 计量：

- **流式**：即使请求里带上 `stream_options.include_usage: true`，响应中也完全没有 `usage`。
- **非流式**：会返回 `usage`，但数字是字符数估算（`len([]rune)/4`），不是真实 token 数。
- **OpenAI 面上没有上下文窗口**：按模型区分的 `context_window_tokens` 只存在于非标准的 `GET /capabilities`；`/v1/models` 不暴露，`/props` 硬编码 `128000`。

后果：任何依据 `usage` 计算 **token 速度**（`completion_tokens / 耗时`）或 **上下文占比**（`prompt_tokens / 上下文窗口`）的客户端，在经由本代理的模型上都显示不出数据。已在 **v1.6.12**（当前最新 release）、Remote API 模式、Windows amd64 上验证。

## 环境

- lingma-proxy v1.6.12（桌面版，Windows amd64），`transport: remote`，远端地址来自 QoderCN 登录缓存（主机名已脱敏）。
- 客户端：一个 OpenAI 兼容的 agent 前端，配置为 `api: openai-completions`、`baseURL: http://127.0.0.1:8095/v1`、`model: kmodel`。
- 用纯 `curl` 即可复现，与客户端实现无关。

## 复现步骤

流式：

```bash
curl -N http://127.0.0.1:8095/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-lingma-local' \
  -d '{"model":"kmodel","stream":true,"max_completion_tokens":16,
       "stream_options":{"include_usage":true},
       "messages":[{"role":"user","content":"只回复两个字：你好"}]}'
```

非流式：同样的 body，改成 `"stream": false`。

## 实际结果

流式 —— 整个响应体只有 4 帧，任何一帧都没有 `usage`：

```text
data: {"choices":[{"delta":{"role":"assistant"},"index":0}],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{"content":"你好"},"index":0}],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"stop","index":0}],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk"}
data: [DONE]
```

按 OpenAI 规范，设置了 `include_usage: true` 时应在 `[DONE]` 之前多一帧，内容为 `"choices": []` 加上完整的 `"usage"` 对象。这一帧从未出现。

非流式 —— `usage` 有，但是估算值。9 个汉字的问题 + 2 个字的回答会得到：

```json
"usage": {"prompt_tokens": 2, "completion_tokens": 0, "total_tokens": 2}
```

（`9/4 = 2` 为整数除法，`2/4 = 0`。）前端无法把它和真实计数区分开 —— 2 个字的回答被报成 0 个输出 token，就是最直观的症状。

真实客户端流量同样如此：`GET /debug/requests` 显示最近 **46/46** 条 `POST /v1/chat/completions`（`stream: true`）请求都带 `"stream_options":{"include_usage":true}`，而 **46/46** 条记录的响应里都不含 `usage` 子串。

## 根因

**1. 请求结构体里根本没有 `stream_options`。** `openAIChatRequest`（`internal/httpapi/server.go:59-79`）只声明了 `model`、`messages`、`stream`、`max_tokens`、`max_completion_tokens`、`tools`、`tool_choice`、`parallel_tool_calls`、`temperature`、`top_p`、`stop`、`presence_penalty`、`frequency_penalty`、`logprobs`、`top_logprobs`、`response_format`、`seed`、`user`、`reasoning_effort`，再无其他。未知字段（包括 `stream_options`）被静默丢弃，所以 `include_usage` 永远无法影响响应。源码和发行二进制里都搜不到 `stream_options` / `include_usage` 这两个字符串。

**2. 没有任何流式帧携带 `usage`。** `writeOpenAIChunk`（`server.go:2641-2651`）只是把收到的 payload 序列化出去，而所有调用点都只构造 `id` / `object` / `created` / `model` / `choices` —— 例如带 `finish_reason` 的收尾帧（`server.go:1437-1449`）。两条流式分支（聚合分支 `server.go:1255-1290`、增量分支 `server.go:1298-1437`）中都不存在 `choices: []` + `usage` 这种帧。

**3. 上游的 token 计数既没被解析，也没被采用，而是用字符数估算。**

- `innerSSE`（`internal/remote/client.go:707-714`）与 `sseEvent`（`remote_client.go:716-720`）只解码 `choices[].delta.content` 和工具调用 —— 上游 SSE 里即使有 `usage` 字段也会被丢弃。
- `ChatResult` 直接填估算值：`InputTokens: estimateTokens(request.Prompt)`、`OutputTokens: estimateTokens(text)`（`remote_client.go:397-400`），其中 `estimateTokens` 为 `len([]rune(text)) / 4`（`remote_client.go:1375-1381`）；IPC 路径用的是 `max(1, (len([]rune(text))+2)/3)`（`service.go:1562-1568`）。
- 发给上游的请求甚至硬编码了全零 usage：`response_meta.usage = {prompt_tokens: 0, completion_tokens: 0, total_tokens: 0}`（`remote_client.go:521-533`）。

所以端到端的数字来自字符数算术，短回复还会被整数截断。

**4. 其他协议面其实会返回 usage** —— 这说明缺口是 OpenAI chat completions 这一个路径特有的，不是全局拿不到数据：

- Anthropic `/v1/messages` 流式在 `message_start` 带 `usage.input_tokens`（`server.go:808-809`），在 `message_delta` 带 `output_tokens`（`server.go:900`）；非流式在 `server.go:682-683`。
- OpenAI `/v1/responses` 在 `response.completed` 里带回 usage。

## 相关：OpenAI 兼容面上没有上下文窗口

唯一准确的按模型上下文元数据在非标准的 `GET /capabilities`（`server.go:427-456`）里，例如：

```json
"Kimi-K2.6": {"context_window_tokens": 256000, "basis": "official_kimi_docs", ...}
```

但 `GET /v1/models`（`modelResponse`，`server.go:106-111`）只返回 `id/object/created/owned_by`，而 LM Studio 风格的 `/props` 把窗口硬编码成 `128000`（`server.go:493`、`server.go:499`、`server.go:561`）—— 只有 Kimi 真实 256K 的一半。也就是说，上下文占比指标的分母在标准端点上拿不到，而信任 `/props` 的客户端会算错一倍。

## 建议修法

1. **接受 `stream_options.include_usage`**（`/v1/chat/completions` 与 `/api/v1/chat/completions`），置位时在 `data: [DONE]` 之前补一帧 `choices: []` 带 `usage` —— 工具调用路径和错误路径也要覆盖。
2. **优先用真实计数，而不是估算。** 如果远端 Lingma 的 SSE 里有 usage，请在 `innerSSE`/`sseEvent` 中解析并经 `ChatResult` 传出来；如果没有，至少请在文档里说明 OpenAI 面的 `usage` 是估算值，并改用真正的 tokenizer 而非 `len(runes)/4`（当前的截断会对短回复产生 `completion_tokens: 0`）。另外请在上游请求里填 `response_meta.usage`，而不是发零值。
3. **在 OpenAI 兼容面上暴露 `context_window_tokens`** —— 可以在 `/v1/models` 增加字段，或通过 LM Studio 风格的 `/props` 返回，复用 `/capabilities` 里已有的元数据，而不是硬编码 `128000`。

我愿意提 PR；只要告诉我远端上游响应里到底有没有 token 计数（或确认没有），前两项就很好落地。v1.6.12 上直接用 `GET /debug/requests` 就能验证修复：带 `include_usage: true` 的 `stream: true` 请求，其记录的响应里应当出现 usage 帧。
## 附录：真实客户端流量捕获（2026-10-09，`kmodel`）

被捕获的请求体（`messages`/`tools` 省略，其余逐字）：

```json
{"max_completion_tokens":32768,"messages":[...],"model":"kmodel","prompt_cache_key":"session-a8af9802-...","store":false,"stream":true,"stream_options":{"include_usage":true},"tools":[...]}
```

被捕获的响应体（完整 4 帧）：

```text
data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null,"index":0}],"created":1791517195,"id":"chatcmpl-1791517195211765700","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{"content":"Hello from TypeScript!"},"finish_reason":null,"index":0}],"created":1791517195,"id":"chatcmpl-1791517195211765700","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"stop","index":0}],"created":1791517195,"id":"chatcmpl-1791517195211765700","model":"kmodel","object":"chat.completion.chunk"}
data: [DONE]
```

同一时刻 `GET /debug/requests` 复核：50 条 `POST /v1/chat/completions`，**0 条**响应包含 `"usage"`；其中 9 条属于同一个 `prompt_cache_key`。全部响应都没有 `choices: []` 的 usage 帧。

## 拟议修法（已在 v1.6.12 上验证：可应用、可编译、测试通过）

干净 v1.6.12 clone 上 `git apply` 通过，`go build ./internal/... ./cmd/...` 与 `go vet` 通过；`go test ./internal/...` 仅有两个**预先存在、与补丁无关**的环境相关失败（`TestSaveCredentialFileRoundTrip` 在 Windows 上拿不到 0600 权限位、`TestDefaultImageURISchemeFallsBackToLingma` 在本机装了 QoderCN 时返回 `qodercn`）。临时单测 7/7 通过，实测补发的帧为 `data: {"choices":[],"created":1700000000,"id":"chatcmpl-verify","model":"kmodel","object":"chat.completion.chunk","usage":{"completion_tokens":250,"completion_tokens_details":{"reasoning_tokens":12},"prompt_tokens":1000,"prompt_tokens_details":{"cached_tokens":400},"total_tokens":1250}}`。

代理内部其它 10 处都在发 `usage`（非流式 `writeOpenAIChatCompletion`、Anthropic 流、Responses 流），唯独 `/v1/chat/completions` 的**流式**分支在 `finish_reason` 帧之后直接写 `data: [DONE]`（`internal/httpapi/server.go:1289`、`:1452`），既没解析请求里的 `stream_options.include_usage`（`openAIChatRequest` 结构体里没有该字段，`server.go:59-79`），也没有补发 usage 帧。

最小修法：

1. `openAIChatRequest` 增加 `StreamOptions *streamOptions` 字段（`json:"stream_options,omitempty"`），并新增 `type streamOptions struct { IncludeUsage bool `json:"include_usage,omitempty"` }`；
2. 在 `handleOpenAIStream` 的两个出口、`data: [DONE]` **之前**插入：

```go
if includeUsage {
    _ = writeOpenAIChunk(w, flusher, map[string]any{
        "id": chatID, "object": "chat.completion.chunk", "created": created, "model": model,
        "choices": []map[string]any{},
        "usage": map[string]any{
            "prompt_tokens":     result.InputTokens,
            "completion_tokens": result.OutputTokens,
            "total_tokens":      result.InputTokens + result.OutputTokens,
        },
    })
}
```

流式结果里 `InputTokens/OutputTokens` 已经由 `estimateTokens` 填好（remote 路径见 `internal/remote/client.go` 的 `Chat`，IPC 路径见 `internal/service/service.go` 的 `buildChatResult`），因此**不需要上游配合**即可输出 usage。数值为字符数估算，若要精确需解析上游 SSE 的真实用量。

另外建议顺带修正上下文窗口：`/props` 的 `n_ctx`、`/v1/models` 附近的窗口在 `internal/httpapi/server.go:493/499/561` 硬编码为 128000，而 `/capabilities` 中 Kimi-K2.6 的 `context_window_tokens` 为 256000。

端到端实测（2026-10-09，真实上游）：原版 v1.6.12 GUI 版对一次流式请求返回 8 帧、无 `usage`；补丁版在另一个端口起实例后返回 10 帧，尾部为 `data: {"choices":[],…,"usage":{"completion_tokens":1,"prompt_tokens":100,"total_tokens":101}}` 加 `data: [DONE]`。再用一个会打印「解码到的 usage 是否为 nil」的探针构建打同一远端，确认上游**确实下发 usage**（`prompt=98, completion=2`）——即报出的 token 数是真实计数而非估算，代理只是原先把它丢了。

更完整的修法已在本地实现并验证：`lingma-proxy-usage-fix.patch`（+324 / -46，涉及 `internal/httpapi/server.go`、`internal/remote/client.go`、`internal/service/service.go`）。除补 usage 帧外还包括：(a) 解析上游 SSE 的真实 `usage`（含 `prompt_tokens_details.cached_tokens` 与 `completion_tokens_details.reasoning_tokens`），有真实值优先于估算；(b) 这两个 details 仅在数值 >0 时输出；(c) 估算器改为 CJK 感知（汉字约 0.7 token、ASCII 约 0.25 token），且 `completion_tokens` 不含思考段，避免 TPS 虚高；(d) `/props` 的 `n_ctx` 与 `/v1/models` 的 `max_context_length`、`config.context_length` 按模型返回而非硬编码 128000（Kimi = 256000）；(e) 工具流过滤保尾从 96 rune 收到 16 rune，首个可见 token 更早到达客户端，TTFT/TPS 分母更真实。
