import assert from 'node:assert/strict'
import { readFile } from 'node:fs/promises'
import { resolve } from 'node:path'
import ts from 'typescript'

const modulePath = resolve(import.meta.dirname, '../src/lib/api.ts')
const rawEntry = '{"id":"entry_1","namespace":"default","key":"counter","value":9007199254740993,"size_bytes":16,"created_at":"2026-08-04T00:00:00Z","updated_at":"2026-08-04T00:00:00Z"}'

function installBrowserGlobals(apiKey = '') {
  const values = new Map(apiKey ? [['apiKey', apiKey]] : [])
  globalThis.localStorage = {
    getItem: key => values.get(key) ?? null,
    setItem: (key, value) => values.set(key, value),
    removeItem: key => values.delete(key),
    clear: () => values.clear(),
  }
  globalThis.window = { location: { href: '' } }
}

async function loadAPI() {
  const source = await readFile(modulePath, 'utf8')
  const output = ts.transpileModule(`${source}\n// test load ${Math.random()}`, {
    compilerOptions: { target: ts.ScriptTarget.ES2020, module: ts.ModuleKind.ESNext },
  }).outputText
  return import(`data:text/javascript;base64,${Buffer.from(output).toString('base64')}`)
}

function response(status, body = '') {
  return new Response(body, { status, headers: { 'Content-Type': 'application/json' } })
}

async function testRefreshRetryPreservesRawInteger() {
  installBrowserGlobals()
  const calls = []
  globalThis.fetch = async (input, init = {}) => {
    const url = String(input)
    calls.push({ url, init })
    if (url.endsWith('/v1/kv/reveal') && calls.filter(call => call.url.endsWith('/v1/kv/reveal')).length === 1) {
      return response(401)
    }
    if (url.endsWith('/v1/admin/auth/refresh')) return response(200)
    if (url.endsWith('/v1/kv/reveal')) return response(200, rawEntry)
    throw new Error(`Unexpected request: ${url}`)
  }

  const { api } = await loadAPI()
  const entry = await api.revealKVEntry('default', 'counter')
  assert.equal(entry.value_raw, '9007199254740993')
  assert.equal(calls.length, 3)
  assert.equal(calls[0].init.headers.Authorization, undefined)
  assert.deepEqual(JSON.parse(calls[0].init.body), { namespace: 'default', key: 'counter' })
}

async function testTerminalSession401RedirectsToLogin() {
  installBrowserGlobals()
  globalThis.fetch = async input => String(input).endsWith('/v1/admin/auth/refresh')
    ? response(401)
    : response(401)

  const { api } = await loadAPI()
  await assert.rejects(() => api.revealKVEntry('default', 'counter'), /Unauthenticated/)
  assert.equal(globalThis.window.location.href, '/login')
}

async function testAPIKeyUsesSharedAuthorizationBehavior() {
  installBrowserGlobals('vl_test_key')
  const calls = []
  globalThis.fetch = async (input, init = {}) => {
    calls.push({ url: String(input), init })
    return response(200, rawEntry)
  }

  const { api } = await loadAPI()
  const entry = await api.revealKVEntry('default', 'counter')
  assert.equal(entry.value_raw, '9007199254740993')
  assert.equal(calls.length, 1)
  assert.equal(calls[0].init.headers.Authorization, 'Bearer vl_test_key')
}

async function testTerminalAPIKey401KeepsStandardInvalidKeyBehavior() {
  installBrowserGlobals('vl_test_key')
  const calls = []
  globalThis.fetch = async (input, init = {}) => {
    calls.push({ url: String(input), init })
    return response(401)
  }

  const { api } = await loadAPI()
  await assert.rejects(() => api.revealKVEntry('default', 'counter'), /Invalid API key/)
  assert.equal(calls.length, 1)
  assert.equal(globalThis.window.location.href, '')
}

await testRefreshRetryPreservesRawInteger()
await testTerminalSession401RedirectsToLogin()
await testAPIKeyUsesSharedAuthorizationBehavior()
await testTerminalAPIKey401KeepsStandardInvalidKeyBehavior()
console.log('api reveal shared request checks passed')
