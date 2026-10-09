# lingma-proxy-patch

给自建 API 源 **LingmaProxy / QoderCN Proxy**（Wails GUI + 无头服务端）打的两批补丁，以及配套的验证材料、构建工具链与分析文档。

**目标**：让 DSH / Qoder 侧恢复 **token 速度（TPS）** 与 **上下文长度占比** 的显示，并查清 ptc 模式下工具执行异常的原因。

> 本项目原先散落在 `C:\extern\externLib\HelloFnos` 下（`.lingma-*`、`.tmp_*`、`.gotool`、`.gocache` 等临时目录）。2026-10-09 已整体迁移到 `C:\extern\externLib\lingma-proxy-patch`，HelloFnos 恢复为纯 fnOS 包项目。

## 当前状态

| 项目 | 状态 |
| --- | --- |
| 第一批补丁：`POST /v1/chat/completions` 流式补发 OpenAI 规范 usage 尾帧 | ✅ 实现 + 编译 + 实测 |
| 第二批补丁 E1–E13：上游真实 usage 透传、cached/reasoning 明细、CJK 感知估算、上下文窗口元数据、TTFT 过滤窗口缩小 | ✅ 实现 + 编译 + 实测 |
| 无头版产物 `dist/lingma-proxy-fixed.exe` | ✅ `go build` 通过（13,021,184 字节） |
| GUI 版产物 `dist/LingmaProxy-gui-fixed.exe` | ✅ `wails build` 通过（13,054,464 字节，冒烟测试窗口正常） |
| 上游 issue 文案（中 / 英） | ✅ `docs/` |
| ptc 工具模拟通路根因分析 | ✅ `docs/lingma-proxy-tool-emulation-analysis.zh.md` |
| 工具模拟 P1–P5 修复补丁 | ⬜ 未实现（方案已定，见分析文档第六节） |
| 向上游提 issue / PR | ⬜ 未提交 |

## 一句话结论

- 客户端（DSH）请求侧本来就带 `stream_options.include_usage`，但代理的 `openAIChatRequest` 没有该字段、静默丢弃；流式分支又从不发 `choices:[]` + `usage` 尾帧 → 客户端拿不到用量 → **TPS 与上下文占比同时消失**。
- 探针实测（真机回放）：上游**确实**返回真实 usage（`prompt 24987 / completion 107 / cached 24985`），是代理把它丢了。补丁后显示的是**真实计数**，不是估算；只有上游未给 usage 时才回退到字符数估算。

## 目录结构

| 路径 | 内容 |
| --- | --- |
| `docs/` | 四份交付文档：上游 issue（中/英）、修复说明、ptc 工具模拟根因分析 |
| `patches/lingma-proxy-usage-fix.patch` | 两批补丁合并后的完整 diff（20,975 字节，+324/−46，3 文件） |
| `dist/` | 两个已编译产物：无头版 `lingma-proxy-fixed.exe`、GUI 版 `LingmaProxy-gui-fixed.exe` |
| `tests/` | 三份留档测试源码（运行时要复制回 clone 的对应包内） |
| `src/lingma-proxy/` | 上游 v1.6.12 的 clone，已应用两批补丁（工作区状态，另提交在本地分支 `usage-fix`） |
| `src/upstream-flat/` | 最初摊平出来的原始上游源文件（server.go / service.go / client.go / remote_client.go / toolemulation.go + 抓包 json），只读参考 |
| `evidence/` | 取证材料：`lingma-run/`（回放请求与探针日志）、`verify-clone/`（干净 clone 验证）、`fix-workspace/`（补丁工作区）、`session76/`（「测试工具功能」窗口时间线）、`dsh-asar/`（从 app.asar 抽出的 DSH 源码） |
| `toolchain/` | 便携构建工具链：`go/`（go1.27.2）、`bin/wails.exe`（wails CLI v2.12.0）、`node20/node-v20.20.2-win-x64/`、`gopath/`（含已下载的 go1.23.6） |
| `cache/gocache/` | Go 构建缓存（保留可让重编从 14 分钟降到 1–2 分钟） |
| `scripts/` | `build-headless.ps1`、`build-gui.ps1` |
| `junk/SystemDrive-caches/` | 迁移时从 HelloFnos 带出的 Windows 图标缓存误建目录，可直接删 |

## 常用命令

```powershell
# 1) 编译无头版 → dist\lingma-proxy-fixed.exe（缓存热约 20 秒）
.\scripts\build-headless.ps1

# 2) 编译 GUI 版 → dist\LingmaProxy-gui-fixed.exe（首次约 14 分钟）
.\scripts\build-gui.ps1

# 3) 把补丁应用到一份干净的上游 clone
git clone --depth 1 --branch v1.6.12 https://github.com/Lutiancheng1/lingma-proxy
git -C lingma-proxy apply ..\patches\lingma-proxy-usage-fix.patch

# 4) 运行无头版（换端口，避免和 GUI 抢 8095）
.\dist\lingma-proxy-fixed.exe -port 8096

# 5) 验证 usage 尾帧是否出现
curl.exe -N -s http://127.0.0.1:8096/v1/chat/completions -H "Content-Type: application/json" -d '{"model":"kmodel","stream":true,"stream_options":{"include_usage":true},"messages":[{"role":"user","content":"hi"}]}'
```

预期在 `finish_reason` 帧之后、`data: [DONE]` 之前看到一帧：

```text
data: {"choices":[],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk","usage":{"completion_tokens":1,"prompt_tokens":100,"total_tokens":101}}
```

## 部署与互斥

- GUI 版与无头版都监听 8095，**只能跑一个**：先停掉占用方再启动另一个。
- 两个版本读同一份配置：`C:\Users\Roborock\.config\lingma-proxy\config.json`（backend remote / model kmodel / port 8095）。
- 客户端（DSH）provider `lingma` 指向 `http://127.0.0.1:8095/v1`；若改用 8096，需要同步改 DSH 的 `cordis.patch.yml`。
- 替换安装目录里的旧版：备份 `C:\extern\externSoftware\LingmaProxy.exe`（原始 13,024,768 字节）后覆盖为新 GUI 版即可。

## 构建环境与坑

- 工具链全部便携、随项目走，不污染系统 PATH（系统 PATH 里没有 go / node）。
- **关键坑**：用 Go 1.27 直接跑 `wails build` 会报 `internal error: package "archive/zip" without types was imported from "lingma-ipc-proxy/desktop"`（wails CLI 内置的 x/tools 读不懂 Go 1.27 的导出数据）。解法：`GOTOOLCHAIN=go1.23.6`（与根 go.mod 的 `toolchain go1.23.6` 一致，已下载到 `toolchain/gopath`）。
- 前端不要用 pnpm 11（`ERR_PNPM_IGNORED_BUILDS: esbuild@0.15.18`，且 `--allow-build` 已移除），用 `toolchain/node20` 里的 npm。
- 上游 `src/lingma-proxy/AGENTS.md` 的约定（改动该仓库时适用）：每次改完代码立即更新 `ITERATION.md`（已 gitignore）；优先改现有文件、不主动新建文档；编译通过后才告知用户测试；代码注释用英文。

## 待办

1. 落地路径三选一：GUI 版常驻 8095（当前即 GUI 版）/ 无头版换 8096 常驻 / 保持现状。
2. 是否实现工具模拟 P1–P5 修复（候选：纯 tool_calls 的 assistant 消息不再丢弃、未知工具名兜底映射到 run_code 的 `tools.<name>()`、长度闸门先剥离 action 代码块、补中文 needle）。
3. 把 `docs/lingma-proxy-usage-issue.md` 提到上游仓库。
