---
title: Queued + Streamed Sync Invocation
description: Route all invocations through Redis to a worker, stream live output back to the caller, and gate debug logs to dev.
sidebar_position: 3
---

# Design: Queued + Streamed Sync Invocation

## 1. Goal

Route **all** invocations (including today's "sync") through Redis to a separate
worker. The worker executes via the streaming path and publishes output events;
the open HTTP request relays them live and ends with the final result. Debug
logs (`print` / `console.log`) appear **only in `dev`**.

## 2. Locked decisions

| Decision | Choice |
|---|---|
| Client contract | SSE stream; **content negotiation:** `Accept: text/event-stream` → SSE, else buffered final JSON |
| Live transport | Redis, per-invocation channel (implemented as a **Redis Stream**, see §6) |
| Worker topology | **Separate process** from the API; no in-memory hub |
| Output types | debug logs → **dev only**; generator chunks + final result → **all envs** |
| Dev gating | resolved invocation **`env == "dev"`** |

## 3. Current state (recap)

- Sync (`scheduler.Invoke`) calls `executor.Run()` inline and writes the DB once. Single JSON body.
- Async (`InvokeAsync`) enqueues `redisstore.Job` on `velane:jobs`; worker `BRPOP`s and runs `exec.Run()` (buffered), writes DB once, optional webhook.
- Stream (`InvokeStream`) calls `exec.RunStream()` directly in the API process (no worker), relays SSE; **stderr is dropped** in stream mode, and stdout carries both logs and the return value with no separation.
- `StreamChunk{Data, Error, Done}` has no type tag and no log/result distinction.
- Worker runs in-process today but is structured to be a separate deployment.

## 4. Target architecture

```text
client ──(SSE or JSON)──► control-plane: InvocationsHandler
        │ 1. resolve snippet/version/env, create invocation (pending)
        │ 2. open Redis Stream reader at "$" for inv:<id>:events
        │ 3. enqueue job on velane:jobs
        ▼
      Redis (jobs list + per-invocation event stream)
        ▲ BRPOP
        │
     worker (separate replica)
        │ status→running
        │ RunStream(spec) → executor runtime (Bun/Python)
        │ for each typed event: XADD inv:<id>:events
        │ env != dev → drop "log" events
        │ on finish: XADD {result}/{error} + {done}; finalize DB row
        ▼
handler XREADs events:
   - SSE caller: forward each as an SSE event; close on {done}
   - JSON caller: accumulate; on {done} return final JSON body
```

Postgres stays the durable record (final write, optional checkpoints). Redis
carries the live feed. No in-memory hub, since the worker is a separate process.

## 5. Event protocol (the core of this change)

A single typed envelope flows harness → runner → worker → Redis → handler.
NDJSON between subprocess and runner; JSON objects on the Redis Stream.

```json
{"type":"log","stream":"stdout","text":"fetching user…","ts":169...}
{"type":"log","stream":"stderr","text":"warn: retrying","ts":169...}
{"type":"chunk","data":"partial output","seq":3}
{"type":"result","output":{...},"duration_ms":812,"exit_code":0}
{"type":"error","message":"handler raised: …","exit_code":1}
{"type":"done"}
```

Rules:

- `log` = debug output (`print` / `console.log` / stderr). **Dropped by the worker when `env != "dev"`.**
- `chunk` = intentional generator/streamed output. Always forwarded.
- `result` = the handler return value. Always forwarded (terminal-ish).
- `error` = terminal failure.
- `done` = stream end sentinel; always last.

This is the piece that didn't exist before: it lets us separate "the answer"
from "debug noise" and gate the noise by env without losing the result.

## 6. Why Redis Streams over raw pub/sub

The conceptual choice was pub/sub; we implement it as a **Redis Stream**
(`XADD` / `XREAD`) per invocation because it's a superset that removes real
footguns:

- **No lost-message race.** Subscribe-before-enqueue makes raw pub/sub *mostly* safe, but any reconnect loses everything. Streams retain events.
- **Resume / reconnect.** UI can reconnect with `Last-Event-ID` and replay from offset (SSE `id:` field maps cleanly to stream IDs).
- **JSON (buffered) callers** can `XREAD` the whole thing after completion using the same data path with no special casing.
- **TTL / trim** with `XADD MAXLEN` + key expiry bounds memory.

Key: `velane:inv:<invocationID>:events`, set short TTL (e.g. a few minutes past
completion). For prod (no logs) this stream is tiny, with just `chunk` (maybe),
`result`, `done`.

## 7. Component-by-component changes

**Harness (Bun `runner.ts` + Python `runner.py`)**

- Intercept `print` / `console.log` / `console.error` → emit `{"type":"log",...}` lines instead of raw writes.
- Emit handler return as `{"type":"result",...}`; generator yields as `{"type":"chunk",...}`.
- Read subprocess **stderr concurrently** (fixes the dropped-stderr bug) and tag as `log` / `error`.
- Streaming harness emits the same envelope; non-stream `/run` can keep returning a buffered result but should also adopt the envelope for consistency.

**Executor interface (`interface.go`)**

- Extend `StreamChunk` with `Type` and `Stream` (and `Output any` for result). The remote executor's SSE parser passes typed events straight through.

**Remote executor (`remote/executor.go`)**

- `RunStream` already parses `data:` SSE lines into `StreamChunk`; update to carry the new fields. (Firecracker stub mirrors it.)

**Redis store (`store/redis/`)**

- New file: per-invocation event stream helpers, including `PublishEvent(ctx, invID, event)`, `ReadEvents(ctx, invID, lastID) (events, nextID)`, plus key TTL / trim. Keep `queue.go` (jobs list) as-is.

**Worker (`worker/worker.go`)**

- For queued-streaming jobs, switch `process()` from `exec.Run()` to `exec.RunStream()`.
- For each event: **if `env != "dev"` and `type == "log"`, skip**; else `XADD` to the invocation stream.
- Set status `running` at start; on terminal event, **finalize DB** (`UpdateInvocationResult`) with output / stderr / status, the same as today. Optionally checkpoint periodically.
- Async + webhook behavior unchanged for async-mode jobs.

**Job model (`redisstore.Job`)**

- Add a mode / flag (e.g. `Stream bool` or reuse `InvokeMode`) so the worker knows to stream-and-publish vs. the legacy buffered async path.

**Scheduler (`scheduler.go`)**

- New method (e.g. `InvokeQueued`) that: resolves version, creates invocation (pending), enqueues the job, and returns the invocation ID **without** running the executor. The handler owns the Redis read loop.
- Old `Invoke` (inline) can be kept temporarily behind a flag for rollback, or removed once callers move.

**Handler (`invocations.go`)**

- Content negotiation on `Accept`.
- **Order: open the stream reader (at `$`) → enqueue → read loop.** (Even with Streams, opening first avoids a cold-start gap.)
- SSE path: forward events, set SSE `id:` to the stream ID for resume, close on `done`.
- JSON path: accumulate, return one body on `done` (logs included only if dev).
- Timeouts: if no worker emits `running` / first event within N seconds → `503` / timeout event. Respect client disconnect (cancel read; worker keeps running and finalizes DB).

**DB**

- Final record unchanged (`output`, `stderr`, `error`, `status`, `duration_ms`).
- **Logs are ephemeral (Redis only).** Open question: do we want dev logs persisted for the Logs tab, or live-only? (see §10).

## 8. Dev-gating logic

- Single point of enforcement: the **worker**, keyed on the resolved `job.Env == "dev"`. `log` events are dropped before `XADD` in non-dev. This guarantees prod debug output never reaches Redis, the client, or storage, creating a clean security boundary.
- `chunk` and `result` are never gated.

## 9. Caller migration

| Caller | Today | After |
|---|---|---|
| Admin UI "Run" | sync JSON | SSE: render `chunk` / `result` in Output panel, `log` in Logs terminal (dev only) |
| Admin Logs tab | placeholder | live SSE feed + historical via `get_logs` |
| MCP `invoke_snippet` | buffered JSON | unchanged contract; server buffers stream and returns final JSON (logs field only in dev) |
| CLI `invoke` | JSON | default JSON (buffered); `--stream` already exists → SSE |
| curl | JSON | JSON unless `Accept: text/event-stream` |

Net: with content negotiation, **MCP / CLI / curl keep working unchanged**; only
the UI opts into SSE.

## 10. Resolved decisions (as implemented)

1. **Dev logs are live-only** (Redis Streams, ephemeral, ~5 min TTL). No DB column added; the Logs tab shows the current run's stream. A durable log table can be added later.
2. **No-worker / stall handling:** the handler uses a 90s idle timeout and a 5 min hard cap, after which it emits a terminal `done` with `error:"timeout"`. A background reaper (`FailStaleInvocations`, 1 min tick, 10 min staleness) marks rows stuck in `pending`/`running` as `timeout`.
3. **Client disconnect:** the worker keeps running and finalizes the DB regardless; the JSON path reads the final row with a background context.
4. **Stream bounds:** event stream is `XADD MAXLEN ~10000` with a 5 min key TTL.
5. **Inline `Invoke` retained as a fallback:** when Redis is unavailable (`HasEventStream()` is false), sync uses the inline executor path and stream uses the legacy direct SSE path. With Redis present, both route through the queue.
6. **Scopes unchanged:** the invoke endpoint keeps its existing auth (`invoke` scope / membership role) and tenant resolution.

### Transport selection (as implemented)

SSE is returned only when the caller sends `Accept: text/event-stream`; otherwise a buffered JSON body is returned once the worker finalizes. This applies to both `sync` and `stream` modes, so MCP (which sends no such header) always receives aggregated JSON, while the CLI `--stream` and the admin UI opt into SSE.

## 11. Edge cases / failure modes

- **No worker available** → timeout event / 503; invocation stays `pending` then marked `failed` by a reaper (need to define) or left for retry.
- **Worker crash mid-job** → no `done`; handler hits read timeout; invocation needs a stale-`running` reaper to mark `failed`.
- **Redis down** → no queued path; either hard-fail or fall back to inline `Invoke` (decision tied to Q5).
- **Stream race** → solved by open-reader-before-enqueue + Streams retention.
- **Slow client** → backpressure on the HTTP write; worker is decoupled (writes to Redis), so a slow client can't stall execution.
- **Duplicate delivery / at-least-once** → events carry `seq` / stream IDs; consumers are idempotent (last `result` / `done` wins).

## 12. Phasing (completed)

- **Phase 1, protocol + plumbing (done):** typed `StreamChunk` envelope; Bun + Python streaming harnesses rewritten to emit `log`/`chunk`/`result`/`error`/`done` and redirect user stdout/stderr to typed `log` events; Redis per-invocation event-stream helpers (`PublishEvent`/`ReadEvents`, `events.go`); `Job.Stream` flag; worker streaming path with dev-gating and DB finalize-before-done ordering.
- **Phase 2, handler + callers (done):** `Scheduler.InvokeQueued` + `HasEventStream`/`ReadEvents`; content-negotiating SSE / JSON handler (`invokeQueuedMode`); CLI `--stream` sends the SSE `Accept` header; MCP unchanged (buffered JSON); admin UI streams the run and splits Output vs. Logs; stale-invocation reaper in `main.go`.

### Tests

- `go test ./...` green across control-plane, mcp-server; CLI builds. `go vet` clean.
- New unit tests cover dev-gating: `TestWorker_StreamJob_DevForwardsLogs` and `TestWorker_StreamJob_ProdDropsLogs`.
- `npx tsc --noEmit` clean for the admin app.
- DB/Redis-backed integration tests skip without `TEST_DATABASE_URL` / `TEST_REDIS_URL`; the buffered SSE round-trip is best validated end-to-end via `make up`.

## Related docs

- [Invocation Modes](./invocation-modes.md)
- [Request Lifecycle](./request-lifecycle.md)
- [KV Store](./kv-store.md)
- [MCP Overview](../mcp/overview.md)
