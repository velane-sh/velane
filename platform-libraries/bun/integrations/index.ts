/**
 * @velane/integrations — built-in integration proxy client
 *
 * Lets snippets call any OAuth-connected provider API without handling
 * credentials. Auth is managed by the platform; tokens are never exposed
 * to snippet code.
 *
 * Usage:
 *   import { integration } from '@velane/integrations'
 *
 *   const github = integration('github')
 *   const user   = await github.get('/user')
 *   const issue  = await github.post('/repos/owner/repo/issues', { title: 'Bug' })
 *   await github.patch('/repos/owner/repo/issues/1', { state: 'closed' })
 *   await github.delete('/repos/owner/repo/labels/old-label')
 *
 * The provider slug must match a connected integration in your Velane dashboard.
 * Paths are the provider's native API paths — see the Integrations tab for docs.
 */

const PROXY_URL = process.env.VELANE_PROXY_URL ?? ''
const TENANT_ID = process.env.VELANE_TENANT_ID ?? ''
// Signed by the control plane; carries the invoking user's group access so the
// proxy can enforce integration grants. Opaque and unforgeable from here.
const CALLER_TOKEN = process.env.VELANE_CALLER_TOKEN ?? ''

export class IntegrationClient {
  constructor(
    private readonly provider: string,
    private readonly opts?: { alias?: string },
  ) {}

  private async req(method: string, endpoint: string, body?: unknown): Promise<unknown> {
    if (!PROXY_URL) throw new Error('@velane/integrations: VELANE_PROXY_URL is not set')
    if (!TENANT_ID) throw new Error('@velane/integrations: VELANE_TENANT_ID is not set')

    const url = `${PROXY_URL}/v1/proxy/${this.provider}${endpoint}`
    const res = await fetch(url, {
      method,
      headers: {
        'Content-Type':    'application/json',
        'X-Velane-Tenant': TENANT_ID,
        ...(CALLER_TOKEN ? { 'X-Velane-Caller': CALLER_TOKEN } : {}),
        ...(this.opts?.alias ? { 'X-Velane-Integration-Alias': this.opts.alias } : {}),
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })

    if (!res.ok) {
      const text = await res.text().catch(() => res.statusText)
      throw new Error(`[${this.provider}] ${method} ${endpoint} → HTTP ${res.status}: ${text}`)
    }

    const ct = res.headers.get('content-type') ?? ''
    if (res.status === 204 || !ct) return null
    if (ct.includes('application/json')) return res.json()
    return res.text()
  }

  /** GET request to the provider API. */
  get(endpoint: string): Promise<unknown> {
    return this.req('GET', endpoint)
  }

  /** POST request to the provider API. */
  post(endpoint: string, body?: unknown): Promise<unknown> {
    return this.req('POST', endpoint, body)
  }

  /** PATCH request to the provider API. */
  patch(endpoint: string, body?: unknown): Promise<unknown> {
    return this.req('PATCH', endpoint, body)
  }

  /** PUT request to the provider API. */
  put(endpoint: string, body?: unknown): Promise<unknown> {
    return this.req('PUT', endpoint, body)
  }

  /** DELETE request to the provider API. */
  delete(endpoint: string): Promise<unknown> {
    return this.req('DELETE', endpoint)
  }
}

/**
 * Returns a client for the given connected integration provider.
 * @param provider - Provider slug (e.g. "github", "salesforce", "slack")
 */
export function integration(provider: string, opts?: { alias?: string }): IntegrationClient {
  return new IntegrationClient(provider, opts)
}

export default integration
