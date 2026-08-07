# Store

`@velane/store` is a tenant-scoped JSON key-value store that persists between snippet invocations. It is suitable for sync cursors, deduplication markers, and simple workflow memory.

## Quick start

```ts
import { store } from '@velane/store'

await store.set('last-run', { completedAt: new Date().toISOString() })
const lastRun = await store.get<{ completedAt: string }>('last-run')
```

> The default `store` namespace is shared tenant-wide. Every workflow in the tenant can read and write `store.set('last-run', ...)`. Use a namespace to isolate a workflow or purpose.

```ts
const githubSync = store.namespace('github-sync')
await githubSync.set('cursor', { page: 42 })
```

## API

### `store.namespace(namespace)`

Returns another `Store` client scoped to `namespace`. It does not change the default singleton.

### `await store.get<T>(key)`

Returns the stored JSON value, or `null` when the key is missing. A stored JSON `null` is intentionally indistinguishable from a missing key through this API.

### `await store.set(key, value, { ttl? })`

Stores any JSON value: objects, arrays, strings, numbers, booleans, and `null`. `undefined` is rejected before a request is made because JSON cannot represent it safely.

`ttl` is optional and measured in seconds. Setting a key without `ttl` clears any previous expiration, so the value persists until it is replaced or deleted.

### `await store.delete(key)`

Deletes a key and returns `true` when it existed or `false` when it was already missing.

## Errors and timeouts

The client needs the runtime-provided `VELANE_PROXY_URL` and `VELANE_TENANT_ID` environment variables. It throws a descriptive error if either is absent. API failures include the HTTP method, key, and status in the error message. Each request times out after 10 seconds.

Values are stored as plaintext JSON. Do not use this store for secrets; use Velane Variables or Credentials instead.

## Cursor example

```ts
import { store } from '@velane/store'

const sync = store.namespace('customer-sync')
const cursor = await sync.get<{ nextPage?: string }>('cursor')
const page = await fetchCustomers(cursor?.nextPage)

await processCustomers(page.items)
await sync.set('cursor', { nextPage: page.nextPage }, { ttl: 86_400 })
```
