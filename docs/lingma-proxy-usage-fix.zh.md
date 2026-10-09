# LingmaProxy 无法显示 token 速度 / 上下文占比：修复补丁说明

> 适用版本：LingmaProxy / QoderCN Proxy v1.6.12（仓库 github.com/Lutiancheng1/lingma-proxy）
> 补丁文件：[lingma-proxy-usage-fix.patch](lingma-proxy-usage-fix.patch)（+324 / -46，3 个文件：`internal/httpapi/server.go`、`internal/remote/client.go`、`internal/service/service.go`）

## 一句话结论

代理的 `POST /v1/chat/completions` **流式**响应缺少 OpenAI 规范要求的「usage 尾帧」，客户端拿不到 token 用量，所以 token 速度和上下文占比都没有数据。
**请求侧完全正常**（`stream: true` + `stream_options.include_usage: true` 都发了），是代理把它丢了；代理内部其实早就算好了 token 数，只是流式分支没往外发。补上这一帧即可，**不需要上游配合**。

## 证据链（都在 v1.6.12 源码里）

- 你捕获的真实响应：`finish_reason: "stop"` 帧之后直接 `data: [DONE]`，中间没有 `{"choices":[],"usage":{...}}` 这一帧。
- 代理其它 10 处都在发 usage：非流式 `writeOpenAIChatCompletion`（`internal/httpapi/server.go:2323-2327`）、Anthropic 流（`server.go:807`、`899`、`924`）、Responses 流与 body（`server.go:1133`、`1162`、`1192`、`1227`、`2398`）。**唯独 chat/completions 的流式分支不发**（`server.go:1289` 与 `server.go:1452` 直接写 `data: [DONE]`）。
- 请求结构体 `openAIChatRequest`（`server.go:59-79`）**没有 `stream_options` 字段**，客户端发来的 `include_usage` 被 json 解码静默丢弃；下游 SSE 解码（`internal/remote/client.go:707-720`）也只读 `delta.content` / `tool_calls`。
- 但 token 数是现成的：remote 路径 `internal/remote/client.go:397-404` 用 `estimateTokens` 算好 `InputTokens/OutputTokens`；IPC 路径 `internal/service/service.go:1091-1098` 用 `estimateTokens` 加真实 `context_usage`（`usedTokens/limitTokens`）。

## 补丁内容

1. `internal/httpapi/server.go`：`openAIChatRequest` 增加 `StreamOptions *streamOptions`（`json:"stream_options,omitempty"`），新增内部类型 `streamOptions{ IncludeUsage bool }`。
2. `normalizeOpenAIRequest`：把 `stream_options.include_usage` 转成新增的 `service.ChatRequest.IncludeUsage`。
3. 新增 `writeOpenAIUsageChunk(...)`：输出 `{"choices":[],"usage":{"prompt_tokens":…,"completion_tokens":…,"total_tokens":…}}`；优先用 `InputTokens/OutputTokens`，`InputTokens` 为 0 时退回 IPC 的真实 `UsedTokens`。
4. `handleOpenAIStream` 的两个出口（聚合工具流分支、普通流式分支）在 `data: [DONE]` **之前**各调用一次，条件 `if req.IncludeUsage`。
5. `internal/service/service.go`：`ChatRequest` 增加 `IncludeUsage bool`。

## 应用补丁

```bash
git clone https://github.com/Lutiancheng1/lingma-proxy
cd lingma-proxy
git checkout v1.6.12
git apply /path/to/lingma-proxy-usage-fix.patch
```

已核验：本机源码与 v1.6.12 的 blob 完全一致（`internal/httpapi/server.go` = `b9c6f16d…`、`internal/service/service.go` = `43e5b56b…`，由 `git hash-object` 与仓库 tree 比对），`git apply --check --reverse` 通过。
✅ 已在干净 clone 上验证（v1.6.12，含两批改动）：`git clone --depth 1 --branch v1.6.12` → `git apply --check` 通过 → `git apply` 成功 → 用可移植 Go 1.27.2 执行 `go build ./internal/... ./cmd/...`（exit 0）、`go vet ./internal/httpapi/... ./internal/remote/... ./internal/service/...`（exit 0）、`gofmt -l` 对补丁改动的三个文件无输出（LF 归一化后）。`go test ./internal/...` 里 `internal/remote` 的 `TestSaveCredentialFileRoundTrip` 在 Windows 上失败（`credentials file mode = 666, want 0600`，Windows 不生效 `chmod` 权限位）、`internal/lingmaipc` 的 `TestDefaultImageURISchemeFallsBackToLingma` 因本机装了 QoderCN 而返回 `qodercn` —— **两者都与补丁无关**（补丁只改 3 个文件，未触及这两个测试；在原始 v1.6.12 上同样失败）。

## 自行构建（需要 Go + Node 20，与上游 release.yml 一致）

```bash
go install github.com/wailsapp/wails/v2/cmd/wails@v2.12.0
npm ci --prefix desktop/frontend
npm run build --prefix desktop/frontend
wails build -platform windows/amd64 -clean -devtools -ldflags "-X main.devtoolsBuild=true"
```

只想要无 GUI 的服务端二进制时，上游用的是 `CGO_ENABLED=0 go build -trimpath -ldflags "-s -w" -o dist/lingma-proxy ./cmd/lingma-ipc-proxy`。

## 验证（不需要客户端）

```powershell
curl.exe -N -H "Authorization: Bearer sk-lingma-local" -H "Content-Type: application/json" `
  -d '{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}' `
  http://127.0.0.1:8095/v1/chat/completions
```

预期尾部（usage 帧在 [DONE] 之前）：

```text
data: {"choices":[{"delta":{},"finish_reason":"stop","index":0}],...,"model":"kmodel",...}
data: {"choices":[],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk","usage":{"prompt_tokens":123,"completion_tokens":45,"total_tokens":168}}
data: [DONE]
```

## 运行时验证（实测输出）

补丁应用后写了两组临时 Go 测试（跑完已从 clone 删除，副本留在工作目录）：第一批 `internal/httpapi/zz_verify_usage_test.go`（副本 [lingma-proxy-usage-fix_test.go](lingma-proxy-usage-fix_test.go)），第二批 `internal/httpapi/zz_usage_check_test.go` 与 `internal/remote/zz_usage_check_test.go`（副本 [lingma-proxy-usage-fix2-httpapi_test.go](lingma-proxy-usage-fix2-httpapi_test.go)、[lingma-proxy-usage-fix2-remote_test.go](lingma-proxy-usage-fix2-remote_test.go)）。`go test -count=1 -run Verify -v ./internal/httpapi/ ./internal/remote/` → **7/7 PASS**。第二批实测输出的 usage 帧逐字为：

```text
data: {"choices":[],"created":1700000000,"id":"chatcmpl-verify","model":"kmodel","object":"chat.completion.chunk","usage":{"completion_tokens":250,"completion_tokens_details":{"reasoning_tokens":12},"prompt_tokens":1000,"prompt_tokens_details":{"cached_tokens":400},"total_tokens":1250}}
```

覆盖点：`choices` 存在且为空数组（OpenAI 规范形态）；`UsedTokens` 有值时 `prompt_tokens` 优先取真实上下文用量（4567）；缓存/推理 token 未知时**不输出** details（不编造）；`InputTokens` 为 0 时回退到 IPC 的 `UsedTokens`（第一批测试为 `"prompt_tokens":777`）；`stream_options.include_usage` 缺省时 `StreamOptions` 为 `nil`；工具流过滤对 20 rune 的首块立即放出 4 rune（20-16），跨 delta 的 action 标记仍被拦、`Flush()` 不泄漏；`parseSSEPayload` 能读到 `content` / `reasoning_content` / `usage`；`buildChatResult` 优先上游真实值、无 usage 时回退估算；CJK 估算 100 汉字 ≥60 token、100 ASCII ≤40 token、空串为 0。

## 第二批增强（E1–E13，已含在同一个补丁里）

第一批解决「有没有 usage 帧」，第二批把 usage 的**内容**做实：

1. **解析上游真实 usage**：`internal/remote/client.go` 的 SSE 解码新增 `remoteUsage`，读取 `usage.prompt_tokens` / `completion_tokens` / `total_tokens` / `cached_tokens`，并兼容 `prompt_tokens_details.cached_tokens`（Kimi 的缓存命中）与 `completion_tokens_details.reasoning_tokens`（思考 token）。有真实值用真实值，没有才回退估算（`buildChatResult`）。
2. **usage 帧带上明细**：新增 `openAIUsagePayload`（`internal/httpapi/server.go`），仅在数值 >0 时附带 `prompt_tokens_details.cached_tokens` 与 `completion_tokens_details.reasoning_tokens`，不编造。DSH 的 `parseChunkUsage` 会把它们映射成「缓存命中 / 推理 token」明细。
3. **更准的估算器**：`estimateTokens` 改为 CJK 感知（汉字约 0.7 token/字、ASCII 约 0.25 token/字），取代原来「字符数/3 或 /4」的粗糙算法；`completion_tokens` 只统计可见正文、不含思考段（否则 TPS 会虚高），思考量单独进 `reasoning_tokens`。
4. **上下文窗口不再硬编码 128000**：新增 `modelContextWindow(...)`，`/props` 的 `n_ctx` 与 `/v1/models` 的 `max_context_length` / `config.context_length` 按模型返回（kmodel/kimi = 256000、mmodel/minimax = 204800、qwen3-coder = 256000，未知回退 128000）。
5. **改善 TTFT/TPS 分母**：工具流过滤的保尾长度由 96 rune 收到 16 rune（`actionMarkerTailRunes`），首块可见内容不再被压住近百个字符，首个 token 时间点更接近真实。

## 端到端实测（真实上游，2026-10-09）

同一台机器上对照跑，互不影响：GUI 版继续占 8095，补丁版用 `-port 8096` 起第二个实例。

| 实例 | 流式响应帧数 | 含 usage 帧 |
|---|---|---|
| 原版 GUI 版（8095） | 8 | ❌ |
| 补丁版 `lingma-proxy-fixed.exe`（8096） | 10 | ✅ |

补丁版实测尾帧：

```text
data: {"choices":[{"delta":{"content":"ping"},"finish_reason":null,"index":0}],"created":1791520270,"id":"chatcmpl-1791520270057722700","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[],"created":1791520270,"id":"chatcmpl-1791520270057722700","model":"kmodel","object":"chat.completion.chunk","usage":{"completion_tokens":1,"prompt_tokens":100,"total_tokens":101}}
data: [DONE]
```

**关键结论：上游确实下发真实 usage。** 用一个临时探针构建（在 `buildChatResult` 里打印 `usage` 是否为 nil，用完即删）打同一个远端，输出为 `PROBE buildChatResult usage=REAL prompt= 98 completion= 2 cached= 0 reasoning= 0`——token 数来自上游真实计数而非估算。`cached_tokens` 在本例为 0 表示这次没命中缓存，不是字段没解析。

运行补丁版（无界面二进制）：

```powershell
C:\extern\externLib\lingma-proxy-patch\dist\lingma-proxy-fixed.exe -port 8096
```

它是**控制台程序、没有窗口**：直接双击、且 8095 已被 GUI 版占用时，会打印 `listen tcp 127.0.0.1:8095: bind: Only one usage of each socket address ...` 后立刻退出（表现为黑窗口一闪）。让它生效的两种方式：①把客户端 baseURL 指到 8096；②先退出 GUI 版，再启动它占用 8095。

## 注意事项

1. **数值精度**：优先用上游 SSE 里真实的 `usage`（第二批已实现解析）；只有上游确实没回 usage 时才退回估算（CJK 感知的 `estimateTokens`，约 0.7 token/汉字、0.25 token/ASCII 字符）。
2. **只对声明了 `stream_options.include_usage` 的客户端发 usage**（符合 OpenAI 规范，你的 DSH 已声明）。想无条件发，把两处 `if req.IncludeUsage { … }` 的外层判断去掉即可。
3. **上下文窗口**：`/props` 的 `n_ctx`、`/v1/models` 的 `max_context_length` / `config.context_length` 原来在 `internal/httpapi/server.go:493`、`:499`、`:561` 硬编码 128000，第二批已改为按模型返回（kmodel/kimi = 256000）。DSH 侧不受影响（会话缓存里本就有 `contextWindow: 262144`），但读 `/props` 的其它客户端现在能拿到正确窗口。

## 不想自己编译？两个替代方案

- 把补丁提到上游仓库（[lingma-proxy-usage-issue.zh.md](lingma-proxy-usage-issue.zh.md) 已含完整 issue 文案），等新版本发布。
- 在 8095 前面再套一个几十行的本地小代理：转发流式响应的同时自己累计输出 token，并在 `[DONE]` 前补上 usage 帧，DSH 的 baseURL 指到新端口即可（不需要改 LingmaProxy、不需要 Go）。需要的话我可以写。