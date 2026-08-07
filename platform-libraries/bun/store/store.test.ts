import { afterEach, beforeEach, describe, expect, test } from 'bun:test'

import { Store } from './index'

const originalFetch = globalThis.fetch
const originalProxyURL = process.env.VELANE_PROXY_URL
const originalTenantID = process.env.VELANE_TENANT_ID

function restoreEnv(name: 'VELANE_PROXY_URL' | 'VELANE_TENANT_ID', value: string | undefined): void {
  if (value === undefined) delete process.env[name]
  else process.env[name] = value
}

describe('@velane/store', () => {
  beforeEach(() => {
    process.env.VELANE_PROXY_URL = 'http://control-plane:8080'
    process.env.VELANE_TENANT_ID = 'tenant-123'
  })

  afterEach(() => {
    globalThis.fetch = originalFetch
    restoreEnv('VELANE_PROXY_URL', originalProxyURL)
    restoreEnv('VELANE_TENANT_ID', originalTenantID)
  })

  test('encodes keys, routes namespaces, sends the tenant header, and passes TTL', async () => {
    let request: Request | undefined
    globalThis.fetch = (async (input: RequestInfo | URL, init?: RequestInit) => {
      request = new Request(input, init)
      return new Response(null, { status: 204 })
    }) as typeof fetch

    await new Store().namespace('sync/jobs').set('cursor/a.b', { page: 2 }, { ttl: 60 })

    expect(request?.url).toBe(
      'http://control-plane:8080/v1/internal/kv/entry?namespace=sync%2Fjobs&key=cursor%2Fa.b',
    )
    expect(request?.method).toBe('PUT')
    expect(request?.headers.get('X-Velane-Tenant')).toBe('tenant-123')
    expect(request?.headers.get('Content-Type')).toBe('application/json')
    expect(await request?.text()).toBe('{"value":{"page":2},"ttl_seconds":60}')
  })

  test('returns null for a missing key', async () => {
    globalThis.fetch = (async () => new Response('missing', { status: 404 })) as typeof fetch

    await expect(new Store().get('missing')).resolves.toBeNull()
  })

  test('returns false when deleting a missing key', async () => {
    globalThis.fetch = (async () => new Response('missing', { status: 404 })) as typeof fetch

    await expect(new Store().delete('missing')).resolves.toBeFalse()
  })

  test('returns stored JSON values', async () => {
    globalThis.fetch = (async () => Response.json({ value: [true, null, 3] })) as typeof fetch

    await expect(new Store().get<boolean[] | null[]>('result')).resolves.toEqual([true, null, 3])
  })

  test('rejects missing proxy or tenant configuration', async () => {
    delete process.env.VELANE_PROXY_URL
    await expect(new Store().get('key')).rejects.toThrow('VELANE_PROXY_URL is not set')

    process.env.VELANE_PROXY_URL = 'http://control-plane:8080'
    delete process.env.VELANE_TENANT_ID
    await expect(new Store().get('key')).rejects.toThrow('VELANE_TENANT_ID is not set')
  })

  test('rejects undefined values before requesting', async () => {
    let called = false
    globalThis.fetch = (async () => {
      called = true
      return new Response(null, { status: 204 })
    }) as typeof fetch

    await expect(new Store().set('key', undefined)).rejects.toThrow('value must not be undefined')
    expect(called).toBeFalse()
  })

  test('includes the method, key, and status in errors', async () => {
    globalThis.fetch = (async () => new Response('denied', { status: 403 })) as typeof fetch

    await expect(new Store().set('cursor', 1)).rejects.toThrow('[store] PUT cursor → HTTP 403: denied')
  })
})
