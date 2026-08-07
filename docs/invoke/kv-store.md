---
title: KV Store
description: Persist state across invocations with the tenant-scoped KV store.
sidebar_position: 4
---

# KV Store

The KV store persists small JSON values between snippet invocations. Use it for
incremental-sync cursors, deduplication markers, and simple workflow memory.

It is not a cache, blob storage, or a secret store. Use object storage for
large or binary data, and use Variables/Credentials for secrets.

## Store APIs

Both runtimes provide a store singleton. The default namespace is the literal
`default`.

| Bun | Python |
|---|---|
| `import { store } from '@velane/store'` | `from velane.store import store` |
| `store.get<T>(key)` | `store.get(key)` |
| `store.set(key, value, { ttl })` | `store.set(key, value, ttl=ttl)` |
| `store.delete(key)` | `store.delete(key)` |
| `store.namespace(name)` | `store.namespace(name)` |

In Bun, `store` is a singleton instance. In Python, `from velane import store`
binds the `velane.store` **module**, not the singleton; use
`from velane.store import store` when calling the store methods.

### Default namespace is tenant-wide

The default namespace is shared by every workflow in the same tenant. It is
not scoped to one snippet or one invocation. Choose an explicit namespace when
workflows need separate state.

```typescript
// In a nightly sync workflow
import { store } from '@velane/store'

await store.set('last_cursor', { cursor: 'cursor-42' })

// A different workflow in the same tenant reads the same value.
const cursor = await store.get<{ cursor: string }>('last_cursor')

// Isolate state deliberately when that sharing is not wanted.
const billingStore = store.namespace('billing-sync')
await billingStore.set('last_cursor', { cursor: 'billing-7' })
```

```python
# In either Python workflow in the same tenant
from velane.store import store

store.set("last_cursor", {"cursor": "cursor-42"})
cursor = store.get("last_cursor")

billing_store = store.namespace("billing-sync")
billing_store.set("last_cursor", {"cursor": "billing-7"})
```

## TTL and expiry

Pass a TTL in seconds to expire a value. The maximum TTL is one year
(`31536000` seconds). Expired entries return as missing during reads even
before the background reaper removes their rows; the reaper performs that
space cleanup separately.

```typescript
await store.set('recent-run', { id: 'run-123' }, { ttl: 3600 })
```

```python
store.set("recent-run", {"id": "run-123"}, ttl=3600)
```

A `set` without a TTL clears an existing expiry and makes the value durable:

```typescript
await store.set('recent-run', { id: 'run-123', retained: true })
```

`get` returns `null`/`None` for a missing or expired key. `delete` returns
`false` when the key is already absent, so cleanup can be idempotent.

## Limits and errors

Limits are enforced per tenant and can be raised by a platform operator.
Defaults are 10,000 keys, 128 KiB per value, and 64 MiB total stored values.

| Status | Condition |
|---|---|
| `413` | The JSONB value exceeds the tenant's per-value limit. |
| `413` | The raw request body exceeds the request-body limit, independently of the stored-value limit. |
| `409` | Writing the entry would exceed the tenant key quota. |
| `409` | Writing the entry would exceed the tenant total-byte quota. |
| `404` | The requested key is missing or expired. |

## Key and namespace rules

Namespaces use lowercase letters, numbers, `_`, and `-`, begin with an
alphanumeric character, and are at most 64 characters. Names beginning with
`velane` are reserved.

Keys are 1–512 bytes, cannot contain ASCII control characters or leading or
trailing whitespace, and travel as query parameters. `.` and `/` are legal in
keys, including keys such as `sync/run.2026-08-04` and `user/42/profile`.
Path-like dot segments are rejected: `a/../b`, `a/./b`, `../b`, and `.` are
not valid keys.

## Examples

### Incremental sync cursor

Read the last cursor, process the next page, and save the new cursor only after
the page succeeds. The next invocation resumes where the previous one stopped.

```typescript
import { integration } from '@velane/integrations'
import { store } from '@velane/store'

export default async function handler() {
  const syncStore = store.namespace('github-issues')
  const previous = await syncStore.get<{ cursor: string }>('cursor')
  const github = integration('github')
  const page = await github.get('/issues', { params: { after: previous?.cursor } })

  // Process page.items before advancing the cursor.
  await syncStore.set('cursor', { cursor: page.next_cursor })
  return { processed: page.items.length, next_cursor: page.next_cursor }
}
```

### Deduplication marker

Use a short TTL to avoid processing the same event twice during a retry window.

```python
from velane.store import store

def handler(input):
    events = store.namespace("webhook-dedup")
    key = f"event/{input['event_id']}"
    if events.get(key) is not None:
        return {"duplicate": True}

    events.set(key, {"seen": True}, ttl=24 * 60 * 60)
    # Process the event after recording the marker for this example's policy.
    return {"duplicate": False}
```

## Plaintext values

**KV values are plaintext JSONB and are not encrypted.** The admin UI redacts
values in list views, but that is a UX and auditing boundary, not a
confidentiality boundary. Do not put API keys, tokens, passwords, or other
secrets in the KV store; put them in Variables/Credentials instead.

## Related docs

- [Invocation Modes](./invocation-modes.md)
- [Request Lifecycle](./request-lifecycle.md)
- [Queued + Streamed Sync Invocation](./queued-streaming-sync.md)
- [Credentials and Scopes](../auth-tenancy/credentials-and-scopes.md)
- [MCP Overview](../mcp/overview.md)
