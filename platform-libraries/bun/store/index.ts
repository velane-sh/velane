/**
 * @velane/store — tenant-scoped state that persists across invocations.
 */

export interface StoreSetOptions {
  /** Expiration in seconds. Omitting it clears a previous expiration. */
  ttl?: number
}

interface StoreEntry<T> {
  value: T
}

export class Store {
  constructor(private readonly ns: string = 'default') {}

  /** Returns a client isolated to the given namespace. */
  namespace(ns: string): Store {
    return new Store(ns)
  }

  /** Reads a JSON value, or null when the key does not exist. */
  async get<T = unknown>(key: string): Promise<T | null> {
    const response = await this.request('GET', key)
    if (response.status === 404) return null
    await this.throwForError(response, 'GET', key)
    return (await response.json() as StoreEntry<T>).value
  }

  /** Stores any JSON value. TTL, when supplied, is measured in seconds. */
  async set(key: string, value: unknown, opts?: StoreSetOptions): Promise<void> {
    if (value === undefined) {
      throw new Error('@velane/store: value must not be undefined')
    }

    const body: { value: unknown; ttl_seconds?: number } = { value }
    if (opts?.ttl !== undefined) body.ttl_seconds = opts.ttl

    const response = await this.request('PUT', key, body)
    await this.throwForError(response, 'PUT', key)
  }

  /** Deletes a key and reports whether it existed. */
  async delete(key: string): Promise<boolean> {
    const response = await this.request('DELETE', key)
    if (response.status === 404) return false
    await this.throwForError(response, 'DELETE', key)
    return true
  }

  private async request(method: string, key: string, body?: unknown): Promise<Response> {
    const proxyURL = process.env.VELANE_PROXY_URL ?? ''
    const tenantID = process.env.VELANE_TENANT_ID ?? ''
    if (!proxyURL) throw new Error('@velane/store: VELANE_PROXY_URL is not set')
    if (!tenantID) throw new Error('@velane/store: VELANE_TENANT_ID is not set')

    const url = `${proxyURL}/v1/internal/kv/entry?namespace=${encodeURIComponent(this.ns)}&key=${encodeURIComponent(key)}`
    return fetch(url, {
      method,
      headers: {
        'X-Velane-Tenant': tenantID,
        ...(body === undefined ? {} : { 'Content-Type': 'application/json' }),
      },
      body: body === undefined ? undefined : JSON.stringify(body),
      signal: AbortSignal.timeout(10_000),
    })
  }

  private async throwForError(response: Response, method: string, key: string): Promise<void> {
    if (response.ok) return
    const text = await response.text().catch(() => response.statusText)
    throw new Error(`[store] ${method} ${key} → HTTP ${response.status}: ${text}`)
  }
}

/** Default tenant-wide namespace. Prefer namespace() to isolate workflow state. */
export const store = new Store()

export default Store
