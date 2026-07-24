import type {
  User,
  OrgMembership,
  Branding,
  TenantMember,
  InviteToken,
  UsageSummary,
  APIKey,
  EgressPolicy,
  RuntimeLimits,
  RuntimeSettings,
  Snippet,
  SnippetVersion,
  SnippetEnvironment,
  InvocationResult,
  Invocation,
  InvocationLogResponse,
  InvocationStatus,
  LogLine,
  EmbedToken,
  Secret,
  Connection,
  NangoProvider,
  IntegrationConfig,
  MCPInfo,
} from '../types'

const BASE = '/api'
const SESSION_REFRESH_INTERVAL_MS = 14 * 60 * 1000

function getStoredAPIKey(): string {
  return localStorage.getItem('apiKey') ?? ''
}

let refreshInFlight: Promise<boolean> | null = null

export async function refreshSession(): Promise<boolean> {
  if (getStoredAPIKey()) return true
  if (refreshInFlight) return refreshInFlight

  refreshInFlight = (async () => {
    try {
      const res = await fetch(`${BASE}/v1/admin/auth/refresh`, {
        method: 'POST',
        headers: { 'Content-Type': 'application/json' },
        credentials: 'include',
      })
      return res.ok
    } catch {
      return false
    } finally {
      refreshInFlight = null
    }
  })()

  return refreshInFlight
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  authType: 'session' | 'apikey' | 'none' = 'session',
  allowRefresh = true,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
  }
  if (authType === 'apikey') {
    const key = getStoredAPIKey()
    if (key) headers['Authorization'] = `Bearer ${key}`
  }

  const res = await fetch(`${BASE}${path}`, {
    method,
    headers,
    body: body !== undefined ? JSON.stringify(body) : undefined,
    credentials: 'include',
  })

  if (!res.ok) {
    if (res.status === 401 && authType !== 'none') {
      const credential = authType === 'apikey' ? getStoredAPIKey() : ''

      if (credential.startsWith('vl_')) {
        throw new Error('Invalid API key')
      }
      if (credential.startsWith('et_')) {
        throw new Error('Unauthenticated')
      }
      if (allowRefresh && await refreshSession()) {
        return request(method, path, body, authType, false)
      }
      window.location.href = '/login'
      throw new Error('Unauthenticated')
    }

    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new Error(err.error ?? res.statusText)
  }

  if (res.status === 204) return undefined as T
  return res.json()
}

export const api = {
  // Auth
  async login(email: string, password: string): Promise<{ session_token: string; expires_at: string }> {
    return request('POST', '/v1/admin/auth/login', { email, password }, 'none')
  },

  async register(
    email: string,
    password: string,
    inviteToken?: string,
  ): Promise<{ user: User; session_token: string }> {
    return request('POST', '/v1/admin/auth/register', { email, password, invite_token: inviteToken }, 'none')
  },

  async logout(): Promise<void> {
    return request('POST', '/v1/admin/auth/logout', undefined, 'session')
  },

  // Returns the social login providers the server has configured (e.g. ['google', 'github']).
  async listOAuthProviders(): Promise<string[]> {
    try {
      const res = await request<{ providers: string[] }>('GET', '/v1/admin/auth/oauth/providers', undefined, 'none')
      return res.providers ?? []
    } catch {
      return []
    }
  },

  // Full-page redirect into the provider's OAuth flow.
  oauthStartUrl(provider: string): string {
    return `${BASE}/v1/admin/auth/oauth/${provider}/start`
  },

  async refreshSession(): Promise<boolean> {
    return refreshSession()
  },

  sessionRefreshIntervalMs: SESSION_REFRESH_INTERVAL_MS,

  async me(): Promise<User> {
    return request('GET', '/v1/admin/auth/me', undefined, 'session')
  },

  async listMyOrgs(): Promise<OrgMembership[]> {
    return request('GET', '/v1/admin/auth/orgs', undefined, 'session')
  },

  async getActiveOrg(): Promise<OrgMembership> {
    return request('GET', '/v1/admin/auth/orgs/active', undefined, 'session')
  },

  async setActiveOrg(slug: string): Promise<OrgMembership> {
    return request('POST', '/v1/admin/auth/orgs/active', { slug }, 'session')
  },

  async createOrg(name: string, slug: string): Promise<OrgMembership> {
    return request('POST', '/v1/admin/auth/orgs', { name, slug }, 'session')
  },

  // Branding
  async getBranding(): Promise<Branding> {
    return request('GET', '/v1/tenant/branding', undefined, 'apikey')
  },

  async updateBranding(b: Branding): Promise<Branding> {
    return request('PUT', '/v1/tenant/branding', b, 'apikey')
  },

  // Members
  async listMembers(): Promise<TenantMember[]> {
    return request('GET', '/v1/tenant/members', undefined, 'apikey')
  },

  async inviteMember(email: string, role: string): Promise<{ invite_token: string; expires_at: string }> {
    return request('POST', '/v1/tenant/members/invite', { email, role }, 'apikey')
  },

  async removeMember(userID: string): Promise<void> {
    return request('DELETE', `/v1/tenant/members/${userID}`, undefined, 'apikey')
  },

  async listInvites(): Promise<InviteToken[]> {
    return request('GET', '/v1/tenant/members/invites', undefined, 'apikey')
  },

  // Usage
  async getUsage(window: string): Promise<UsageSummary> {
    return request('GET', `/v1/tenant/usage?window=${window}`, undefined, 'apikey')
  },

  // API Keys
  async listAPIKeys(): Promise<APIKey[]> {
    return request('GET', '/v1/tenant/api-keys', undefined, 'apikey')
  },

  async createAPIKey(name: string, scopes: string[]): Promise<APIKey> {
    return request('POST', '/v1/tenant/api-keys', { name, scopes }, 'apikey')
  },

  async deleteAPIKey(id: string): Promise<void> {
    return request('DELETE', `/v1/tenant/api-keys/${id}`, undefined, 'apikey')
  },

  // Egress
  async getEgressPolicy(): Promise<EgressPolicy> {
    return request('GET', '/v1/tenant/egress', undefined, 'apikey')
  },

  async updateEgressPolicy(p: EgressPolicy): Promise<EgressPolicy> {
    return request('PUT', '/v1/tenant/egress', p, 'apikey')
  },

  // Snippets
  async listSnippets(): Promise<Snippet[]> {
    return request('GET', `/v1/snippets`, undefined, 'apikey')
  },

  async getSnippet(id: string): Promise<Snippet> {
    return request('GET', `/v1/snippets/${id}`, undefined, 'apikey')
  },

  async createSnippet(data: { name: string; language: string; description?: string }): Promise<Snippet> {
    return request('POST', `/v1/snippets`, data, 'apikey')
  },

  async updateSnippet(id: string, data: Partial<{ name: string; description: string }>): Promise<Snippet> {
    return request('PATCH', `/v1/snippets/${id}`, data, 'apikey')
  },

  async deleteSnippet(id: string): Promise<void> {
    return request('DELETE', `/v1/snippets/${id}`, undefined, 'apikey')
  },

  // Versions
  async listVersions(snippetId: string): Promise<SnippetVersion[]> {
    return request('GET', `/v1/snippets/${snippetId}/versions`, undefined, 'apikey')
  },

  async listEnvironments(snippetId: string): Promise<SnippetEnvironment[]> {
    return request('GET', `/v1/snippets/${snippetId}/environments`, undefined, 'apikey')
  },

  async getRuntimeLimits(): Promise<RuntimeLimits> {
    return request('GET', '/v1/tenant/runtime-limits', undefined, 'apikey')
  },

  async createVersion(
    snippetId: string,
    code: string,
    runtime?: RuntimeSettings,
  ): Promise<SnippetVersion> {
    const body: Record<string, unknown> = { code }
    if (runtime) {
      body.timeout_ms = runtime.timeout_ms
      body.max_memory_mb = runtime.max_memory_mb
      body.max_cpu_percent = runtime.max_cpu_percent
    }
    return request('POST', `/v1/snippets/${snippetId}/versions`, body, 'apikey')
  },

  async publishVersion(snippetId: string, versionNum: number, env: string): Promise<void> {
    return request('POST', `/v1/snippets/${snippetId}/versions/${versionNum}/publish?env=${env}`, undefined, 'apikey')
  },

  // Returns a cleanup function. Calls onVersion whenever a new draft is created for snippetId.
  watchSnippet(snippetId: string, onVersion: (v: SnippetVersion) => void): () => void {
    const apiKey = getStoredAPIKey()
    const headers: Record<string, string> = {}
    if (apiKey) headers['Authorization'] = `Bearer ${apiKey}`

    const controller = new AbortController()

    async function connect() {
      try {
        const res = await fetch(`${BASE}/v1/snippets/${snippetId}/watch`, {
          headers,
          signal: controller.signal,
          credentials: 'include',
        })
        if (!res.ok || !res.body) return

        const reader = res.body.getReader()
        const decoder = new TextDecoder()
        let buffer = ''
        let eventType = ''
        let data = ''

        while (true) {
          const { done, value } = await reader.read()
          if (done) break
          buffer += decoder.decode(value, { stream: true })
          const lines = buffer.split('\n')
          buffer = lines.pop() ?? ''
          for (const line of lines) {
            if (line.startsWith('event: ')) {
              eventType = line.slice(7).trim()
            } else if (line.startsWith('data: ')) {
              data = line.slice(6).trim()
            } else if (line === '') {
              if (eventType === 'version' && data) {
                try { onVersion(JSON.parse(data)) } catch { /* ignore */ }
              }
              eventType = ''
              data = ''
            }
          }
        }
      } catch {
        // aborted on cleanup — expected
      }
    }

    connect()
    return () => controller.abort()
  },

  // Integrations (Nango-backed OAuth connections)
  async listProviders(query?: string, limit?: number, offset?: number): Promise<NangoProvider[]> {
    const params = new URLSearchParams()
    const trimmedQuery = query?.trim()
    if (trimmedQuery) params.set('q', trimmedQuery)
    if (limit && limit > 0) params.set('limit', String(Math.floor(limit)))
    if (offset !== undefined && offset >= 0) params.set('offset', String(Math.floor(offset)))
    const qs = params.toString()
    return request('GET', `/v1/integrations${qs ? `?${qs}` : ''}`, undefined, 'none')
  },

  async getConnectInfo(): Promise<{ oauth_callback_url: string }> {
    return request('GET', '/v1/connect/info', undefined, 'none')
  },

  async getMCPInfo(): Promise<MCPInfo> {
    return request('GET', '/v1/mcp/info', undefined, 'none')
  },

  async listConfigured(
    query?: string,
    limit?: number,
    offset?: number,
    status?: 'connected' | 'configured' | 'ready' | 'all',
  ): Promise<IntegrationConfig[]> {
    const params = new URLSearchParams()
    const trimmedQuery = query?.trim()
    if (trimmedQuery) params.set('q', trimmedQuery)
    if (status && status !== 'all') params.set('status', status)
    if (limit && limit > 0) params.set('limit', String(Math.floor(limit)))
    if (offset !== undefined && offset >= 0) params.set('offset', String(Math.floor(offset)))
    const qs = params.toString()
    return request('GET', `/v1/integrations/configured${qs ? `?${qs}` : ''}`, undefined, 'apikey')
  },

  async configureIntegration(data: {
    provider: string
    alias?: string
    name?: string
    credentials_type?: string
    credentials?: Record<string, string>
    oauth_client_id?: string
    oauth_client_secret?: string
    oauth_scopes?: string
    is_default?: boolean
  }): Promise<void> {
    return request('POST', '/v1/integrations/configured', data, 'apikey')
  },

  async deleteIntegrationConfig(providerConfigKey: string): Promise<void> {
    return request('DELETE', `/v1/integrations/configured/${providerConfigKey}`, undefined, 'apikey')
  },

  async listConnections(query?: string, limit?: number, offset?: number): Promise<Connection[]> {
    const params = new URLSearchParams()
    const trimmedQuery = query?.trim()
    if (trimmedQuery) params.set('q', trimmedQuery)
    if (limit && limit > 0) params.set('limit', String(Math.floor(limit)))
    if (offset !== undefined && offset >= 0) params.set('offset', String(Math.floor(offset)))
    const qs = params.toString()
    return request('GET', `/v1/tenant/connections${qs ? `?${qs}` : ''}`, undefined, 'apikey')
  },

  async createConnectionSession(
    provider: string,
    alias = 'default',
    credentialProfileID?: string,
  ): Promise<{ session_token: string; connect_url: string; api_url: string; credential_profile_id: string; alias: string }> {
    return request(
      'POST',
      '/v1/tenant/connections/session',
      { provider, alias, credential_profile_id: credentialProfileID },
      'apikey',
    )
  },

  async recordConnection(
    provider: string,
    displayName = '',
    alias = 'default',
    credentialProfileID?: string,
  ): Promise<Connection> {
    return request(
      'POST',
      '/v1/tenant/connections',
      { provider, display_name: displayName, alias, credential_profile_id: credentialProfileID },
      'apikey',
    )
  },

  async disconnectProvider(provider: string): Promise<void> {
    return request('DELETE', `/v1/tenant/connections/${provider}`, undefined, 'apikey')
  },

  // Variables & Credentials (secrets)
  async listSecrets(): Promise<Secret[]> {
    return request('GET', '/v1/secrets', undefined, 'apikey')
  },

  async createSecret(data: { name: string; value: string; is_secret: boolean; environments?: string[] }): Promise<Secret> {
    return request('POST', '/v1/secrets', data, 'apikey')
  },

  async updateSecret(id: string, data: { name?: string; value?: string }): Promise<Secret> {
    return request('PATCH', `/v1/secrets/${id}`, data, 'apikey')
  },

  async deleteSecret(id: string): Promise<void> {
    return request('DELETE', `/v1/secrets/${id}`, undefined, 'apikey')
  },

  // Embed tokens
  async listEmbedTokens(): Promise<EmbedToken[]> {
    return request('GET', '/v1/embed/tokens', undefined, 'apikey')
  },

  async createEmbedToken(snippetIds: string[], ttlSeconds = 3600): Promise<{ id: string; token: string; expires_at: string }> {
    return request('POST', '/v1/embed/tokens', { snippet_ids: snippetIds, ttl_seconds: ttlSeconds }, 'apikey')
  },

  async revokeEmbedToken(id: string): Promise<void> {
    return request('DELETE', `/v1/embed/tokens/${id}`, undefined, 'apikey')
  },

  // Instance
  async getInstanceInfo(): Promise<{ cloud: boolean; plan: string; license_valid: boolean; features: string[] }> {
    return request('GET', '/v1/instance/info', undefined, 'none')
  },

  // Billing
  async getTenantPlan(): Promise<{ plan: string; valid: boolean; features: string[] }> {
    return request('GET', '/v1/tenant/plan', undefined, 'session')
  },

  // Invocation
  async listSnippetInvocations(
    snippetId: string,
    filters: { environment?: string; status?: InvocationStatus; limit?: number } = {},
  ): Promise<InvocationLogResponse> {
    const params = new URLSearchParams()
    if (filters.environment) params.set('env', filters.environment)
    if (filters.status) params.set('status', filters.status)
    params.set('limit', String(filters.limit ?? 50))
    return request('GET', `/v1/logs/snippets/${snippetId}?${params.toString()}`)
  },

  async getInvocation(id: string): Promise<Invocation> {
    return request('GET', `/v1/invocations/${id}`)
  },

  async invokeSnippet(snippetSlug: string, input: string, env = 'dev'): Promise<InvocationResult> {
    return request('POST', `/v1/invoke/${snippetSlug}?env=${env}`, JSON.parse(input || '{}'), 'apikey')
  },

  // Streaming invocation: opens an SSE stream and dispatches typed events.
  // Resolves once the stream completes. Debug logs are only emitted in dev.
  async invokeSnippetStream(
    snippetSlug: string,
    input: string,
    env: string,
    handlers: StreamHandlers,
  ): Promise<void> {
    const key = getStoredAPIKey()
    const res = await fetch(`${BASE}/v1/invoke/${snippetSlug}?env=${env}`, {
      method: 'POST',
      headers: {
        'Content-Type': 'application/json',
        Accept: 'text/event-stream',
        ...(key ? { Authorization: `Bearer ${key}` } : {}),
      },
      body: JSON.stringify(JSON.parse(input || '{}')),
      credentials: 'include',
    })

    if (!res.ok || !res.body) {
      const err = await res.json().catch(() => ({ error: res.statusText }))
      throw new Error(err.error ?? res.statusText)
    }

    const contentType = res.headers.get('Content-Type')?.toLowerCase() ?? ''
    if (!contentType.includes('text/event-stream')) {
      const result = await res.json() as InvocationResult
      result.logs?.forEach((line) => handlers.onLog?.(line))
      if (result.error) handlers.onError?.(result.error)
      handlers.onResult?.(result.output)
      handlers.onDone?.()
      return
    }

    const reader = res.body.getReader()
    const decoder = new TextDecoder()
    let buffer = ''

    const dispatch = (raw: string) => {
      const dataLines = raw
        .split('\n')
        .filter((l) => l.startsWith('data:'))
        .map((l) => l.slice(5).trim())
      if (dataLines.length === 0) return
      let ev: StreamEvent
      try {
        ev = JSON.parse(dataLines.join('\n'))
      } catch {
        return
      }
      switch (ev.type) {
        case 'log':
          handlers.onLog?.({ stream: ev.stream ?? 'stdout', text: ev.text ?? '' })
          break
        case 'chunk':
          handlers.onChunk?.(ev.data ?? '')
          break
        case 'result':
          handlers.onResult?.(
            Object.prototype.hasOwnProperty.call(ev, 'output') ? ev.output : '',
          )
          break
        case 'error':
          handlers.onError?.(ev.message ?? ev.error ?? 'error')
          break
      }
    }

    for (;;) {
      const { done, value } = await reader.read()
      buffer += done ? decoder.decode() : decoder.decode(value, { stream: true })
      buffer = buffer.replace(/\r\n/g, '\n')
      let sep: number
      while ((sep = buffer.indexOf('\n\n')) !== -1) {
        const rawEvent = buffer.slice(0, sep)
        buffer = buffer.slice(sep + 2)
        dispatch(rawEvent)
      }
      if (done) break
    }
    if (buffer.trim()) dispatch(buffer)
    handlers.onDone?.()
  },
}

interface StreamEvent {
  type?: string
  stream?: string
  text?: string
  data?: string
  output?: unknown
  message?: string
  error?: string
  done?: boolean
}

export interface StreamHandlers {
  onLog?: (line: LogLine) => void
  onChunk?: (data: string) => void
  onResult?: (output: unknown) => void
  onError?: (message: string) => void
  onDone?: () => void
}
