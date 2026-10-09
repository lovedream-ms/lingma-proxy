# 「测试工具功能」窗口为什么一直空转：原因与解决方案

> 分析对象：DSH 窗口「测试工具功能」（会话 `session-76e28a7d-c1c4-4fe4-8981-a161f5692519`），模型走自建 API 源 LingmaProxy 的 `kmodel`（Kimi-K2.6）。
> 结论已在真机复现并逐环定位，涉及代码均指 v1.6.12（`internal/...`）。

## 一句话结论

**模型每一轮确实想调工具，但它写出的工具名是 `web_search`，而 DSH 只暴露 `run_code` 一个工具；代理在解析 action block 时把「未在 `tools` 列表里的工具名」直接丢弃，于是这一轮变成「零工具调用的纯文本回复」——DSH 认为正常收尾，用户只好回「继续」，压平后的历史里又多一条同样的计划文本，模型照抄，形成死循环。**

同时，代理的两层「没调用就重试一次」的兜底恰好同时失效（缺中文关键词 + 长度闸门），所以模型从来没有被纠正的机会。

## 一、现象（会话实录）

该窗口共 8 turn / 24 step / 16 次工具调用，全部是 `run_code`：

| turn | 用户 | 模型行为 |
| --- | --- | --- |
| 1 | 测试一下工具是否正常 | 连续 3 次 `console.log('Hello, World!')`，同一个无用调用反复做 |
| 2 | 阅读一下当前工作空间代码，讲解一下 | 又 5 次 `console.log('Hello, World!')`；一次静态 `import {readdir} from 'fs/promises'` → `Error: code run failed (exception): 'import', and 'export' cannot be used outside of module code`；最后只输出一句「我看到工具已经正常运行。让我来读取和分析当前工作空间的代码：」就结束（**零工具调用**） |
| 3 | 继续 | 6 次读文件尝试，其中 4 次 `const fs = require('fs')` → `Error: code run failed (exception): ReferenceError: require is not defined`（同一段错误代码重复 4 次不修正），被用户中止 |
| 4 | 贴出 run_code 环境契约 | 改用 `const fs = await import('node:fs')` 成功读到 README.md（3268 字符），随即又只给文本收尾 |
| 5–8 | 继续 / 继续 / 继续 / 为什么一直在重复内容嗯 | **纯文本、零工具调用**，且逐字重复（127 / 75 / 75 / 123 字符，都是「…让我先搜索一些相关的优秀项目：」收尾） |

另有多次谎报：「现在我已经确认工具可以正常运行，并且已经查看了当前工作目录的文件结构」——当时它并未真正读取。

## 二、取证方法与关键证据

1. **会话解码**：`C:\Users\Roborock\.dsh\sessions\--C-extern-externLib-HelloFnos--\session-76e28a7d-*/session.v4.jsonl.zstd`（该文件是多帧 zstd 拼接，需按 magic `28b52ffd` 逐帧解压）。
2. **代理流量**：`GET http://127.0.0.1:8095/debug/requests` 中该会话 25 条记录。turn 5–8 对应的四条上游响应全部是 `tcDeltas=0, finish=stop`，正文分别 77/79/79/127 字符——与「只说不做」逐条对应。
3. **Go 探针实测**（可复现，探针文件已删）：
   - `LooksLikeMissedToolUse`/`LooksLikeRefusal` 对那 4 条中文回复全部返回 `false`；同一句话的英文版本（`I will search for the best C++ state machine libraries on GitHub:`）返回 `true`。
   - `selectCommandTool(run_code) = false`、`selectCommandTool(bash) = true` → `InferToolCallsFromText` 的兜底推断对 DSH 完全失效。
   - `buildLingmaPrompt(emulateTools=true)` 的真实输出（3651 字节）证实：对话被压平成 `User: ...` / `Assistant: ...`，**只有 tool_calls、没有正文的 assistant 消息整条消失**，工具结果被改写成 `User: Tool result for call_1: ...`，system 提示被挪到最后，末尾是 `Assistant:`。
4. **真机回放**（决定性）：把 8095 上捕获的真实请求（12:43:08，50 条 messages，`tools=[run_code]`）原样打到插桩代理上，得到：`toolCallFrames=0`、`finish=stop`、正文 123 字符；探针日志显示上游回复末尾其实带了一个 `json action` 代码块，工具名是 **`web_search`**，而 `parsedCalls=0`、`gate=false`，并且 `shouldRetryRemoteNativeTool` 连检查都没进入（长度闸门直接返回 false）。

## 三、根因链

1. **工具名不匹配（直接原因）**：DSH 的 ptc 预设只暴露 `run_code`，但系统提示里列着 `web_search`/`web_fetch`/`read`/`grep` 等名字（它们只能在 `run_code` 程序内部通过 `tools.*` 调用）。模型按系统提示选了 `web_search` 写进 action block，代理按 `req.Tools` 白名单校验，未知工具名直接丢弃（上游有测试 `TestParseActionBlocksDropsUnknownToolNames`）→ 0 个 tool call。
2. **兜底一：emulation 层没触发**。`internal/service/service.go:1054-1062` 的 `shouldRetryTooling` 在 `auto` 模式下只认 `LooksLikeRefusal || LooksLikeMissedToolUse`，而中文关键词表（`internal/toolemulation/toolemulation.go:511-576`）里没有「让我先搜索 / 我将为您调研 / 让我为您搜索」这类说法 → `gate=false`，不会注入 `ForceToolingPrompt` 重试。
3. **兜底二：native 层被长度闸门挡死**。`internal/service/service.go:704-733` 的 `shouldRetryRemoteNativeTool` 第一句就是 `if trimmed == "" || len([]rune(trimmed)) > 180 { return false }`；带 JSON 代码块的回复远超 180 rune，于是原生 tools 重试路径（`service.go:489-505`，会把 `tool_choice` 提升为 `required`）从未执行——而这条路径是能把模型拉回正轨的。
4. **兜底三：文本推断失效**。`InferToolCallsFromText`（`toolemulation.go:578-598`）要求 `selectCommandTool` 找到 bash/terminal/shell 类工具才会推断，DSH 只有 `run_code` → 永远返回 0 个调用。
5. **上下文被压平后失真**：`internal/service/service.go:1456-1525` 把所有消息压平成单条 user 文本，`internal/service/service.go:1539-1557` 又丢弃无正文的 assistant 消息。后果：模型看不到「我上一轮已经调过工具」，只看到「用户」塞进来的工具结果；它自己上一轮写的那句计划文本反而成了续写锚点，于是逐字复读。
6. **两套协议互相冲突**：`internal/service/service.go:475-505` 同时把压平文本（内含「请输出 json action 代码块」的注入指令）和**原生 tools 定义**发给上游，模型在「原生工具调用」和「文本协议」之间摇摆。
7. **模型侧差异（放大器）**：同一台机、同一压平通路、同一提示下，兄弟会话 `session-7233169a`（`dashscope_qmodel`）4 步内自我纠正：原生 `web_search` 被 DSH 拒绝并返回 `Error: unknown tool "web_search": only run_code is callable directly — call web_search from inside a run_code program instead` → 改 `run_code` → 静态 import 报错 → 改成 `const result = await tools.web_search({...})` 成功拿到真实结果（记录里有 `tool/ptc-dispatch`）。`session-f83072a8`（同为 `dashscope_qmodel`）同样一次成功。Kimi 则连续 4 次重复同一个 `require` 错误、多次谎报进度。上游 `AGENTS.md` 自己写着：「工具调用依赖模型配合，**Qwen3-Coder 最可靠**，其他模型可能拒绝」。
8. **用户侧放大器**：连续发「继续」，每轮都在压平历史里追加一条相同的计划文本，越重复越像少样本示范。

## 四、解决方案

### A. 立刻可用（不改代码，今天就能验证）

1. **这个窗口换模型**：`dashscope_qwen3_coder`（上游点名最可靠）或 `dashscope_qmodel`（实测两个会话都能自我纠正拿到真实结果）。Kimi 在「多轮工具编排 + 环境契约」上确实弱。
2. **别用「继续」**：改成明确指令（例如「用 run_code 调 tools.web_search 查 GitHub 上的 C++ 状态机库，把结果贴出来」）。「继续」在压平通路下等于把同一句话再喂一遍。
3. **run_code 环境契约**（与该问题无关但同窗口反复出错）：`code` 是 AsyncFunction 体，**不能用 `require`、不能用静态 `import`**；取模块要写 `const fs = await import('node:fs')`；要调工具写 `const r = await tools.web_search({...})`，不要写 `require('dsh-sdk')`。

### B. 代理侧修复（推荐，4 处小改，收益最大）

| 优先级 | 位置 | 改法 |
| --- | --- | --- |
| P1 | `internal/service/service.go:1007-1052` `applyToolEmulation` | **保留未知工具名**：解析出的工具名不在 `req.Tools` 时，若唯一暴露的是代码执行类工具（`run_code`，即 PTC 风格），把该 action block 改写成一次 `run_code` 调用：`const result = await tools.<name>(<json args>); console.log(JSON.stringify(result))`。这正是 Qwen 会话走通的写法，能直接把「模型想搜索」变成真实工具结果。 |
| P2 | `internal/service/service.go:704-733` `shouldRetryRemoteNativeTool` | 长度闸门改为**先剥离 `json action` 代码块再计算长度**；或加入判定「回复里出现 action block 代码块 / 以冒号结尾」→ 直接允许重试。当前 180 rune 闸门会让所有「带代码块但没被解析出来」的回复直接失去重试机会。 |
| P3 | `internal/toolemulation/toolemulation.go:511-576` `LooksLikeMissedToolUse` | 补中文 needle：让我先搜索 / 让我搜索 / 我将搜索 / 我来搜索 / 让我调研 / 我将调研 / 我将为您调研 / 让我先查看 / 我先查看 / 让我先读取 / 接下来我将 / 现在我将 / 我需要先查询。或改用结构化判定（见 P2）替代关键词表。 |
| P4 | `internal/service/service.go:1539-1557` `filteredMessages` | 不要丢弃「只有 tool_calls、没有正文」的 assistant 消息，渲染成 `Assistant: [tool action: run_code {"code":"..."}]`，让模型看得见自己的调用历史，压平后不再把工具结果当成「用户说的话」。 |
| P5 | `internal/service/service.go:475-505` | 二选一，别两套并下：要么彻底不压平、走原生 tools（原生通道已被证明可用，`call_<24hex>` 就是上游原生 ID）；要么在注入的 tooling 文本里显式声明「只允许使用下列工具名，其它名字一律会被丢弃」，并同步把 DSH 系统提示里的其它工具名压掉。 |

改完后按惯例：`gofmt -w .` → `go build ./...` → `pkill` 重启代理 → 在该窗口用「用 run_code 调 tools.web_search 查…」复测。

### C. 客户端侧建议（DSH 与代理的契约）

- ptc 预设的系统提示列出了模型**不能直接调用**的工具名，而代理的 action-block 白名单只认 `run_code`，两者天然打架。长期应在 DSH 侧明确「pc 工具只能用 `tools.*` 调用」，或在代理侧做 P1 的名字映射，二者至少落一个。
- 建议给「同一 turn 内重复相同工具调用」「连续两轮零工具调用且正文高度相似」加保护（提示或强制换策略），可从根上打断本窗口的复读循环。

## 五、本次交付与证据文件

- 本文件：`C:\extern\externLib\lingma-proxy-patch\docs\lingma-proxy-tool-emulation-analysis.zh.md`
- 会话时间线（156 条记录摘要）：`C:\extern\externLib\lingma-proxy-patch\evidence\session76\timeline.txt`
- 真机回放请求体：`C:\extern\externLib\lingma-proxy-patch\evidence\lingma-run\replay.json`；SSE 原文：`.lingma-run\replay-sse.txt`；探针日志：`.lingma-run\probe2.err.log`
- 相关（token 速度 / 上下文占比问题，已交付）：`lingma-proxy-usage-fix.patch`、`lingma-proxy-fixed.exe`、`lingma-proxy-usage-fix.zh.md`

## 附：为什么「token 速度 / 上下文占比」那条线已经不缺东西

当前 8095 上跑的已经是打过补丁的 `lingma-proxy-fixed.exe`（PID 24580，10/09 12:35:36 启动），实测响应里已经带 `usage`（例：`{"prompt_tokens":24987,"completion_tokens":107,"cached_tokens":24985}`），所以显示问题已解决；本窗口暴露的是**工具执行**这条独立的问题线。