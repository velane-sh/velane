# Store

`velane.store` is a tenant-scoped JSON key-value store that persists between snippet invocations. Use it for sync cursors, deduplication markers, and simple workflow memory.

## Quick start

```python
from velane.store import store

store.set("last-run", {"completed_at": "2026-08-04T12:00:00Z"})
last_run = store.get("last-run")
```

> The default `store` namespace is shared tenant-wide. Every workflow in the tenant can read and write `store.set("last-run", ...)`. Use a namespace to isolate a workflow or purpose.

```python
from velane.store import store

github_sync = store.namespace("github-sync")
github_sync.set("cursor", {"page": 42})
```

## Imports

Use either of these forms:

```python
from velane.store import store  # the Store singleton
import velane.store             # the store module; use velane.store.store
```

`from velane import store` also works, but it binds the `velane.store` **module**, not the singleton. Call `store.store.get(...)` in that form, or prefer `from velane.store import store`.

## API

### `store.namespace(namespace)`

Returns another `Store` client scoped to `namespace`. It does not change the default singleton.

### `store.get(key)`

Returns the stored JSON value, or `None` when the key is missing. A stored JSON `null` is intentionally indistinguishable from a missing key through this API.

### `store.set(key, value, ttl=None)`

Stores any JSON value: dictionaries, lists, strings, numbers, booleans, and `None`. `ttl` is optional and measured in seconds. Setting a key without `ttl` clears any previous expiration, so the value persists until it is replaced or deleted.

### `store.delete(key)`

Deletes a key and returns `True` when it existed or `False` when it was already missing.

## Errors and timeouts

The client needs the runtime-provided `VELANE_PROXY_URL` and `VELANE_TENANT_ID` environment variables. It raises a descriptive error if either is absent. API failures include the HTTP method, key, and status in the error message. Each request times out after 10 seconds.

Values are stored as plaintext JSON. Do not use this store for secrets; use Velane Variables or Credentials instead.

## Cursor example

```python
from velane.store import store

sync = store.namespace("customer-sync")
cursor = sync.get("cursor") or {}
page = fetch_customers(cursor.get("next_page"))

process_customers(page["items"])
sync.set("cursor", {"next_page": page["next_page"]}, ttl=86_400)
```
