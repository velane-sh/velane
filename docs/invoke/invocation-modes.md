---
title: Invocation Modes
description: Use sync, async, and stream invocation modes in Velane.
sidebar_position: 1
---

# Invocation Modes

Velane supports three invocation modes for snippets: sync, async, and stream.

## 1) Sync (default)

Use sync when you want a direct request-response call.

```bash
curl -X POST "http://localhost:8080/v1/invoke/<snippet>" \
  -H "Authorization: Bearer vl_xxxx" \
  -H "Content-Type: application/json" \
  -d '{"hello":"world"}'
```

Best for:

- short tasks
- request/response APIs
- quick checks in staging

## 2) Async

Use async when a job can take longer and you do not want the caller to block.

```bash
curl -X POST "http://localhost:8080/v1/invoke/<snippet>" \
  -H "Authorization: Bearer vl_xxxx" \
  -H "X-Invoke-Mode: async" \
  -H "Content-Type: application/json" \
  -d '{"task":"long-job","callback_url":"https://example.com/webhook"}'
```

Best for:

- longer-running jobs
- webhook-driven workflows
- background processing

## 3) Stream

Use stream when you want incremental events rather than one final payload.

```bash
curl -X POST "http://localhost:8080/v1/invoke/<snippet>" \
  -H "Authorization: Bearer vl_xxxx" \
  -H "X-Invoke-Mode: stream" \
  -H "Content-Type: application/json" \
  -d '{}'
```

Best for:

- live status updates
- token or chunked output
- interactive agent-facing flows

## Environment and version targeting

- Environment examples: `dev`, `staging`, `prod`
- You can invoke an explicit version when needed (for testing or rollback validation)

## Lifecycle at a glance

1. Request hits control-plane
2. Auth + tenant checks are applied
3. Scheduler resolves snippet/version/environment
4. Code runs in executor runtime (Bun or Python)
5. Response is returned in sync/async/stream mode

## Related docs

- [KV Store](./kv-store.md)
- [Request Lifecycle](./request-lifecycle.md)
- [Auth and Request Flow](../auth-tenancy/auth-and-request-flow.md)
