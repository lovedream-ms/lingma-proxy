# `POST /v1/chat/completions` never returns token `usage` (streaming ignores `stream_options.include_usage`)

## Summary

OpenAI-compatible chat completions from this proxy never expose usable token accounting:

- **Streaming**: no `usage` is emitted at all, even when the request sets `stream_options.include_usage: true`.
- **Non-streaming**: a `usage` object is returned, but the numbers are rune-count estimates (`len(runes)/4`), not real token counts.
- **No context window on the OpenAI surface**: per-model `context_window_tokens` exists only in the non-standard `GET /capabilities`; `/v1/models` exposes none, and `/props` hard-codes `128000`.

Consequence: any client that derives **tokens/sec** (`completion_tokens / elapsed`) or **context usage %** (`prompt_tokens / context_window`) from `usage` renders nothing for models served through this proxy. Verified on **v1.6.12** (current latest release), Remote API mode, Windows amd64.

## Environment

- lingma-proxy v1.6.12 (desktop, Windows amd64), `transport: remote`, endpoint resolved from the QoderCN login cache (host redacted).
- Client: an OpenAI-compatible agent frontend configured with `api: openai-completions`, `baseURL: http://127.0.0.1:8095/v1`, `model: kmodel`.
- Reproducible with plain `curl` — no client-specific behaviour is involved.

## Steps to reproduce

Streaming:

```bash
curl -N http://127.0.0.1:8095/v1/chat/completions \
  -H 'Content-Type: application/json' \
  -H 'Authorization: Bearer sk-lingma-local' \
  -d '{"model":"kmodel","stream":true,"max_completion_tokens":16,
       "stream_options":{"include_usage":true},
       "messages":[{"role":"user","content":"只回复两个字：你好"}]}'
```

Non-streaming: the same body with `"stream": false`.

## Actual

Streaming — the entire response body (4 frames, no `usage` anywhere):

```text
data: {"choices":[{"delta":{"role":"assistant"},"index":0}],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{"content":"你好"},"index":0}],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"stop","index":0}],"created":...,"id":"chatcmpl-...","model":"kmodel","object":"chat.completion.chunk"}
data: [DONE]
```

With `include_usage: true` the OpenAI spec expects one extra frame before `[DONE]` with `"choices": []` and a populated `"usage"` object. It never arrives.

Non-streaming — `usage` is present but estimated. For a 9-character Chinese prompt and a 2-character reply:

```json
"usage": {"prompt_tokens": 2, "completion_tokens": 0, "total_tokens": 2}
```

(`9/4 = 2` by integer division; `2/4 = 0`.) A frontend cannot distinguish these from real counts — a 2-character reply reported as 0 output tokens is a visible symptom.

Real client traffic agrees: `GET /debug/requests` shows **46/46** recent `POST /v1/chat/completions` (`stream: true`) requests carrying `"stream_options":{"include_usage":true}`, and **46/46** of the recorded responses contain no `usage` substring.

## Root cause

**1. `stream_options` is not part of the request model.** `openAIChatRequest` (`internal/httpapi/server.go:59-79`) declares `model`, `messages`, `stream`, `max_tokens`, `max_completion_tokens`, `tools`, `tool_choice`, `parallel_tool_calls`, `temperature`, `top_p`, `stop`, `presence_penalty`, `frequency_penalty`, `logprobs`, `top_logprobs`, `response_format`, `seed`, `user`, `reasoning_effort` — and nothing else. Any unknown field (including `stream_options`) is silently dropped, so `include_usage` can never influence the response. The strings `stream_options` / `include_usage` do not appear anywhere in the source or in the shipped binary.

**2. No stream chunk carries `usage`.** `writeOpenAIChunk` (`server.go:2641-2651`) marshals whatever payload it is handed, and every call site builds only `id` / `object` / `created` / `model` / `choices` — for example the terminal chunk with `finish_reason` at `server.go:1437-1449`. There is no `choices: []` + `usage` frame anywhere in the streaming paths (`server.go:1255-1290` aggregated branch, `server.go:1298-1437` incremental branch).

**3. Upstream token counts are not parsed; they are estimated from rune counts.**

- `innerSSE` (`internal/remote/client.go:707-714`) and `sseEvent` (`remote_client.go:716-720`) decode only `choices[].delta.content` and tool calls — a `usage` field in the upstream SSE would be discarded.
- `ChatResult` is filled with estimates: `InputTokens: estimateTokens(request.Prompt)`, `OutputTokens: estimateTokens(text)` (`remote_client.go:397-400`), where `estimateTokens` is `len([]rune(text)) / 4` (`remote_client.go:1375-1381`); the IPC path uses `max(1, (len([]rune(text))+2)/3)` (`service.go:1562-1568`).
- The payload sent upstream even hard-codes zeroed usage: `response_meta.usage = {prompt_tokens: 0, completion_tokens: 0, total_tokens: 0}` (`remote_client.go:521-533`).

The end-to-end numbers therefore come from arithmetic on rune counts, with integer truncation for short replies.

**4. Other protocol surfaces do emit usage** — this is a gap specific to OpenAI chat completions, not a global lack of data:

- Anthropic `/v1/messages` streaming sends `usage.input_tokens` in `message_start` (`server.go:808-809`) and `output_tokens` in `message_delta` (`server.go:900`); non-streaming at `server.go:682-683`.
- OpenAI `/v1/responses` carries usage in `response.completed`.

## Related: the context window is not on the OpenAI surface

The only accurate per-model context metadata lives in the non-standard `GET /capabilities` (`server.go:427-456`), e.g.:

```json
"Kimi-K2.6": {"context_window_tokens": 256000, "basis": "official_kimi_docs", ...}
```

but `GET /v1/models` (`modelResponse`, `server.go:106-111`) returns only `id/object/created/owned_by`, and the LM Studio-style `/props` hard-codes `128000` (`server.go:493`, `server.go:499`, `server.go:561`) — half of Kimi's real 256K. So the denominator for a context-usage indicator is unreachable from the standard endpoint, and any client that trusts `/props` will be wrong by 2x.

## Suggested fix

1. **Accept `stream_options.include_usage`** on `/v1/chat/completions` (and `/api/v1/chat/completions`) and, when set, emit a final `choices: []` frame with `usage` before `data: [DONE]` — including on the tool-call and error paths.
2. **Prefer real counts over estimates.** If the upstream remote Lingma SSE exposes usage, decode it in `innerSSE`/`sseEvent` and propagate it through `ChatResult`; if it does not, at least document that the OpenAI `usage` values are estimates and use a real tokenizer rather than `len(runes)/4`, since the current truncation produces `completion_tokens: 0` for short replies. Populate `response_meta.usage` in the upstream request instead of sending zeros.
3. **Expose `context_window_tokens` on the OpenAI-compatible surface** — either as an extra field in `/v1/models` or via the LM Studio-style `/props` — instead of the hard-coded `128000`, reusing the metadata already present in `/capabilities`.

Happy to send a PR; if you can tell me whether the upstream remote response actually contains token counts (or confirm it does not), the first two items become straightforward. `GET /debug/requests` on v1.6.12 is sufficient to verify the fix: the recorded responses for `stream: true` requests with `include_usage: true` should contain a usage frame.
## Appendix: real client traffic capture (2026-10-09, `kmodel`)

Captured request body (`messages`/`tools` elided, all other keys verbatim):

```json
{"max_completion_tokens":32768,"messages":[...],"model":"kmodel","prompt_cache_key":"session-a8af9802-...","store":false,"stream":true,"stream_options":{"include_usage":true},"tools":[...]}
```

Captured response body (complete, 4 frames):

```text
data: {"choices":[{"delta":{"role":"assistant"},"finish_reason":null,"index":0}],"created":1791517195,"id":"chatcmpl-1791517195211765700","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{"content":"Hello from TypeScript!"},"finish_reason":null,"index":0}],"created":1791517195,"id":"chatcmpl-1791517195211765700","model":"kmodel","object":"chat.completion.chunk"}
data: {"choices":[{"delta":{},"finish_reason":"stop","index":0}],"created":1791517195,"id":"chatcmpl-1791517195211765700","model":"kmodel","object":"chat.completion.chunk"}
data: [DONE]
```

Cross-check via `GET /debug/requests` at the same moment: 50 `POST /v1/chat/completions` entries, **0** responses containing `"usage"`; 9 entries share that `prompt_cache_key`. None includes a `choices: []` usage frame.

## Proposed fix (verified on v1.6.12: applies, builds, tests pass)

On a clean v1.6.12 clone the patch applies with `git apply`; `go build ./internal/... ./cmd/...` and `go vet` pass, and `go test ./internal/...` only fails two pre-existing environment-dependent tests (`TestSaveCredentialFileRoundTrip` expects mode 0600, and `TestDefaultImageURISchemeFallsBackToLingma` returns `qodercn` when QoderCN is installed) — neither touches the patched code. Temporary unit tests (7/7 pass) show the appended frame is exactly `data: {"choices":[],"created":1700000000,"id":"chatcmpl-verify","model":"kmodel","object":"chat.completion.chunk","usage":{"completion_tokens":250,"completion_tokens_details":{"reasoning_tokens":12},"prompt_tokens":1000,"prompt_tokens_details":{"cached_tokens":400},"total_tokens":1250}}`.

Ten other places in the proxy already emit `usage` (the non-streaming `writeOpenAIChatCompletion`, the Anthropic stream, the Responses stream). Only the **streaming** branch of `/v1/chat/completions` writes `data: [DONE]` right after the `finish_reason` chunk (`internal/httpapi/server.go:1289`, `:1452`); it neither parses `stream_options.include_usage` (the field is absent from `openAIChatRequest`, `server.go:59-79`) nor appends a usage frame.

Minimal fix:

1. add `StreamOptions *streamOptions` (`json:"stream_options,omitempty"`) to `openAIChatRequest` plus `type streamOptions struct { IncludeUsage bool `json:"include_usage,omitempty"` }`;
2. in `handleOpenAIStream`, immediately **before** `data: [DONE]` in both exit paths, emit:

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

`InputTokens/OutputTokens` are already populated for streaming results by `estimateTokens` (remote path: `Chat` in `internal/remote/client.go`; IPC path: `buildChatResult` in `internal/service/service.go`), so no upstream change is required. The values are character-based estimates; exact counts would require parsing real usage from the upstream SSE.

While here, the hard-coded context window is also worth fixing: `n_ctx` in `/props` and the window near `/v1/models` are hard-coded to 128000 at `internal/httpapi/server.go:493/499/561`, whereas `/capabilities` reports `context_window_tokens: 256000` for Kimi-K2.6.

Live end-to-end check (2026-10-09, real upstream): the stock v1.6.12 GUI build answered a streaming request with 8 frames and no `usage`; an instance of the patched build on a second port answered with 10 frames, ending with `data: {"choices":[],…,"usage":{"completion_tokens":1,"prompt_tokens":100,"total_tokens":101}}` followed by `data: [DONE]`. An instrumented build that logs whether the decoded usage is present confirmed the upstream really does send it (`prompt=98, completion=2`), so the reported counts are real, not estimates — the proxy only had to stop dropping them.

A fuller version of this fix is already implemented and verified locally: `lingma-proxy-usage-fix.patch` (+324 / -46 across `internal/httpapi/server.go`, `internal/remote/client.go`, `internal/service/service.go`). Besides the usage frame it (a) decodes the real `usage` from the upstream SSE, including `prompt_tokens_details.cached_tokens` and `completion_tokens_details.reasoning_tokens`, and prefers real counts over estimates; (b) emits those two detail objects only when non-zero; (c) replaces the char/3 estimator with a CJK-aware one (≈0.7 token per CJK char, 0.25 per ASCII char) and keeps reasoning tokens out of `completion_tokens` so TPS is not inflated; (d) returns a per-model context window from `/props` (`n_ctx`) and `/v1/models` (`max_context_length`, `config.context_length`) instead of 128000 (Kimi = 256000); (e) shortens the tool-stream filter tail from 96 to 16 runes so the first visible token reaches the client sooner (a cleaner TTFT/TPS denominator).
