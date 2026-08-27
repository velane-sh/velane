import type {
  User,
  OrgMembership,
  Branding,
  TenantMember,
  UserGroup,
  IntegrationGroupGrant,
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
  WorkflowTrigger,
  SSOConnection,
  KVEntry,
  KVEntryList,
  KVNamespace,
  SandboxDetailResponse,
  SandboxCursorResponse,
  SandboxEvent,
  SandboxListResponse,
  SandboxLog,
  SandboxOperationKind,
  SandboxMutationResponse,
  SandboxOperation,
  SandboxProfile,
  SandboxRecipe,
  SandboxRecipeDocument,
  SandboxRecipeVersion,
  SandboxSnapshot,
  InstanceInfo,
} from '../types'

const BASE = '/api'
const SESSION_REFRESH_INTERVAL_MS = 14 * 60 * 1000

export class APIError extends Error {
  constructor(message: string, readonly status: number, readonly code?: string, readonly details?: unknown) {
    super(message)
    this.name = 'APIError'
  }
}

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

type RequestAuth = 'session' | 'apikey' | 'none'

interface RequestOptions {
  body?: unknown
  headers?: Record<string, string>
  authType?: RequestAuth
}

async function request<T>(
  method: string,
  path: string,
  body?: unknown,
  authType: RequestAuth = 'session',
  allowRefresh = true,
  decoder?: (response: Response) => Promise<T>,
  additionalHeaders?: Record<string, string>,
): Promise<T> {
  const headers: Record<string, string> = {
    'Content-Type': 'application/json',
    ...additionalHeaders,
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
        return request(method, path, body, authType, false, decoder, additionalHeaders)
      }
      window.location.href = '/login'
      throw new Error('Unauthenticated')
    }

    const err = await res.json().catch(() => ({ error: res.statusText }))
    throw new APIError(err.error ?? err.message ?? res.statusText, res.status, err.code, err.details)
  }

  if (res.status === 204) return undefined as T
  if (decoder) return decoder(res)
  return res.json()
}

function requestWithOptions<T>(
  method: string,
  path: string,
  { body, headers, authType = 'session' }: RequestOptions,
): Promise<T> {
  return request(method, path, body, authType, true, undefined, headers)
}

function mutationHeaders(idempotencyKey: string, generation?: number): Record<string, string> {
  return {
    'Idempotency-Key': idempotencyKey,
    ...(generation === undefined ? {} : { 'If-Match': String(generation) }),
  }
}

function listQuery(filters: { limit?: number; offset?: number }): string {
  const params = new URLSearchParams()
  if (filters.limit !== undefined) params.set('limit', String(filters.limit))
  if (filters.offset !== undefined) params.set('offset', String(filters.offset))
  const query = params.toString()
  return query ? `?${query}` : ''
}

function sandboxCursorPath(id: string, resource: 'events' | 'logs', after?: string): string {
  const params = new URLSearchParams({ limit: '50' })
  if (after) params.set('after', after)
  return `/v1/sandboxes/${id}/${resource}?${params.toString()}`
}

function recipeCursorPath(id: string, version: number, resource: 'events' | 'logs', after?: string): string {
  const params = new URLSearchParams({ limit: '50' })
  if (after) params.set('after', after)
  return `/v1/sandbox-image-recipes/${id}/versions/${version}/${resource}?${params.toString()}`
}

// Extract a top-level JSON field without parsing its value. KV values may contain integers
// beyond Number.MAX_SAFE_INTEGER, which JSON.parse would silently round before the drawer
// can render them.
export function rawJSONField(payload: string, field: string): string {
  let i = payload.search(/\S/)
  if (payload[i] !== '{') throw new Error(`Response is missing ${field}`)
  i++
  while (i < payload.length) {
    while (/\s/.test(payload[i] ?? '')) i++
    if (payload[i] === '}') break
    if (payload[i] !== '"') throw new Error(`Response has an invalid ${field}`)

    const keyStart = i
    i = endOfJSONString(payload, i)
    let key: string
    try {
      key = JSON.parse(payload.slice(keyStart, i))
    } catch {
      throw new Error(`Response has an invalid ${field}`)
    }
    while (/\s/.test(payload[i] ?? '')) i++
    if (payload[i++] !== ':') throw new Error(`Response has an invalid ${field}`)
    while (/\s/.test(payload[i] ?? '')) i++
    const valueStart = i
    i = endOfJSONValue(payload, i)
    if (key === field) return payload.slice(valueStart, i)
    while (/\s/.test(payload[i] ?? '')) i++
    if (payload[i] !== ',') break
    i++
  }
  throw new Error(`Response has an incomplete ${field}`)
}

function endOfJSONString(text: string, start: number): number {
  let escaped = false
  for (let i = start + 1; i < text.length; i++) {
    if (escaped) escaped = false
    else if (text[i] === '\\') escaped = true
    else if (text[i] === '"') return i + 1
  }
  return text.length
}

function endOfJSONValue(text: string, start: number): number {
  if (text[start] === '"') return endOfJSONString(text, start)
  let depth = 0
  let inString = false
  let escaped = false
  for (let i = start; i < text.length; i++) {
    const char = text[i]
    if (inString) {
      if (escaped) escaped = false
      else if (char === '\\') escaped = true
      else if (char === '"') inString = false
      continue
    }
    if (char === '"') inString = true
    else if (char === '{' || char === '[') depth++
    else if (char === '}' || char === ']') {
      if (depth === 0) return i
      depth--
    } else if (char === ',' && depth === 0) return i
  }
  return text.length
}

export const api = {
  // Sandboxes
  async listSandboxes(filters: { limit?: number; offset?: number } = {}): Promise<SandboxListResponse> {
    const params = new URLSearchParams()
    if (filters.limit) params.set('limit', String(filters.limit))
    if (filters.offset !== undefined) params.set('offset', String(filters.offset))
    const query = params.toString()
    return request('GET', `/v1/sandboxes${query ? `?${query}` : ''}`, undefined, 'apikey')
  },

  async getSandbox(id: string): Promise<SandboxDetailResponse> {
    return request('GET', `/v1/sandboxes/${id}`, undefined, 'apikey')
  },

  async createSandbox(data: { name: string; recipe_version_id: string; profile_version_id: string }, idempotencyKey: string): Promise<SandboxMutationResponse> {
    return requestWithOptions('POST', '/v1/sandboxes', {
      body: data,
      headers: mutationHeaders(idempotencyKey),
      authType: 'apikey',
    })
  },

  async sandboxAction(id: string, action: Extract<SandboxOperationKind, 'start' | 'stop' | 'restart'>, idempotencyKey: string, generation?: number): Promise<SandboxMutationResponse> {
    return requestWithOptions('POST', `/v1/sandboxes/${id}/${action}`, {
      headers: mutationHeaders(idempotencyKey, generation),
      authType: 'apikey',
    })
  },

  async createSandboxSnapshot(id: string, idempotencyKey: string, generation?: number): Promise<SandboxMutationResponse> {
    return requestWithOptions('POST', `/v1/sandboxes/${id}/snapshots`, {
      headers: mutationHeaders(idempotencyKey, generation),
      authType: 'apikey',
    })
  },

  async deleteSandbox(id: string, deleteSnapshots: boolean, idempotencyKey: string, generation?: number): Promise<SandboxMutationResponse> {
    return requestWithOptions('DELETE', `/v1/sandboxes/${id}`, {
      body: { delete_snapshots: deleteSnapshots },
      headers: mutationHeaders(idempotencyKey, generation),
      authType: 'apikey',
    })
  },

  async retrySandboxOperation(id: string, operationID: string, idempotencyKey: string, generation?: number): Promise<SandboxMutationResponse> {
    return requestWithOptions('POST', `/v1/sandboxes/${id}/retry`, {
      body: { operation_id: operationID },
      headers: mutationHeaders(idempotencyKey, generation),
      authType: 'apikey',
    })
  },

  async listSandboxSnapshots(id: string, filters: { limit?: number; offset?: number } = {}): Promise<{ items: SandboxSnapshot[]; total: number }> {
    const query = listQuery(filters)
    return request('GET', `/v1/sandboxes/${id}/snapshots${query}`, undefined, 'apikey')
  },

  async getSandboxSnapshot(id: string, snapshotID: string): Promise<SandboxSnapshot> {
    return request('GET', `/v1/sandboxes/${id}/snapshots/${snapshotID}`, undefined, 'apikey')
  },

  async restoreSandboxSnapshot(id: string, snapshotID: string, idempotencyKey: string, generation?: number): Promise<SandboxMutationResponse> {
    return requestWithOptions('POST', `/v1/sandboxes/${id}/snapshots/${snapshotID}/restore`, {
      headers: mutationHeaders(idempotencyKey, generation),
      authType: 'apikey',
    })
  },

  async deleteSandboxSnapshot(id: string, snapshotID: string, idempotencyKey: string, generation?: number): Promise<SandboxMutationResponse> {
    return requestWithOptions('DELETE', `/v1/sandboxes/${id}/snapshots/${snapshotID}`, {
      headers: mutationHeaders(idempotencyKey, generation),
      authType: 'apikey',
    })
  },

  async getSandboxOperation(id: string): Promise<SandboxOperation> {
    return request('GET', `/v1/sandbox-operations/${id}`, undefined, 'apikey')
  },

  async listSandboxProfiles(): Promise<{ items: SandboxProfile[]; total: number }> {
    return request('GET', '/v1/sandbox-profiles', undefined, 'apikey')
  },

  async listSandboxEvents(id: string, after?: string): Promise<SandboxCursorResponse<SandboxEvent>> {
    return request('GET', sandboxCursorPath(id, 'events', after), undefined, 'apikey')
  },

  async listSandboxLogs(id: string, after?: string): Promise<SandboxCursorResponse<SandboxLog>> {
    return request('GET', sandboxCursorPath(id, 'logs', after), undefined, 'apikey')
  },

  async listSandboxImageRecipes(filters: { limit?: number; offset?: number } = {}): Promise<{ items: SandboxRecipe[]; total: number }> {
    return request('GET', `/v1/sandbox-image-recipes${listQuery(filters)}`, undefined, 'apikey')
  },

  async getSandboxImageRecipe(id: string): Promise<SandboxRecipe> {
    return request('GET', `/v1/sandbox-image-recipes/${id}`, undefined, 'apikey')
  },

  async createSandboxImageRecipe(data: { name: string; slug?: string; description?: string }, idempotencyKey: string): Promise<{ recipe: SandboxRecipe; replayed: boolean }> {
    return requestWithOptions('POST', '/v1/sandbox-image-recipes', {
      body: data,
      headers: mutationHeaders(idempotencyKey),
      authType: 'apikey',
    })
  },

  async deleteSandboxImageRecipe(id: string, idempotencyKey: string): Promise<void> {
    return requestWithOptions('DELETE', `/v1/sandbox-image-recipes/${id}`, {
      headers: mutationHeaders(idempotencyKey),
      authType: 'apikey',
    })
  },

  async listSandboxImageRecipeVersions(id: string, filters: { limit?: number; offset?: number } = {}): Promise<{ items: SandboxRecipeVersion[]; total: number }> {
    return request('GET', `/v1/sandbox-image-recipes/${id}/versions${listQuery(filters)}`, undefined, 'apikey')
  },

  async getSandboxImageRecipeVersion(id: string, version: number): Promise<SandboxRecipeVersion> {
    return request('GET', `/v1/sandbox-image-recipes/${id}/versions/${version}`, undefined, 'apikey')
  },

  async createSandboxImageRecipeVersion(id: string, document: SandboxRecipeDocument, idempotencyKey: string): Promise<{ version: SandboxRecipeVersion; operation: SandboxOperation; replayed: boolean }> {
    return requestWithOptions('POST', `/v1/sandbox-image-recipes/${id}/versions`, {
      body: document,
      headers: mutationHeaders(idempotencyKey),
      authType: 'apikey',
    })
  },

  async listSandboxImageRecipeVersionEvents(id: string, version: number, after?: string): Promise<SandboxCursorResponse<SandboxEvent>> {
    return request('GET', recipeCursorPath(id, version, 'events', after), undefined, 'apikey')
  },

  async listSandboxImageRecipeVersionLogs(id: string, version: number, after?: string): Promise<SandboxCursorResponse<SandboxLog>> {
    return request('GET', recipeCursorPath(id, version, 'logs', after), undefined, 'apikey')
  },
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

  // User groups
  async listUserGroups(): Promise<UserGroup[]> {
    return request('GET', '/v1/tenant/groups', undefined, 'apikey')
  },

  async createUserGroup(name: string, description: string): Promise<UserGroup> {
    return request('POST', '/v1/tenant/groups', { name, description }, 'apikey')
  },

  async deleteUserGroup(groupID: string): Promise<void> {
    return request('DELETE', `/v1/tenant/groups/${groupID}`, undefined, 'apikey')
  },

  async addUserGroupMember(groupID: string, userID: string): Promise<void> {
    return request('POST', `/v1/tenant/groups/${groupID}/members`, { user_id: userID }, 'apikey')
  },

  async removeUserGroupMember(groupID: string, userID: string): Promise<void> {
    return request('DELETE', `/v1/tenant/groups/${groupID}/members`, { user_id: userID }, 'apikey')
  },

  async grantIntegrationToGroup(groupID: string, credentialProfileID: string): Promise<void> {
    return request(
      'POST',
      `/v1/tenant/groups/${groupID}/integrations`,
      { credential_profile_id: credentialProfileID },
      'apikey',
    )
  },

  async revokeIntegrationFromGroup(groupID: string, credentialProfileID: string): Promise<void> {
    return request(
      'DELETE',
      `/v1/tenant/groups/${groupID}/integrations`,
      { credential_profile_id: credentialProfileID },
      'apikey',
    )
  },

  async listIntegrationGrants(credentialProfileID: string): Promise<IntegrationGroupGrant[]> {
    return request('GET', `/v1/integrations/configured/${credentialProfileID}/groups`, undefined, 'apikey')
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

  async listWorkflowTriggers(workflowId: string): Promise<WorkflowTrigger[]> {
    return request('GET', `/v1/snippets/${workflowId}/triggers`, undefined, 'apikey')
  },
  async listIntegrationEventModels(connectionId: string): Promise<{ models: string[]; manual_entry: boolean }> {
    return request('GET', `/v1/connections/${connectionId}/sync-models`, undefined, 'apikey')
  },
  async createWorkflowTrigger(workflowId: string, input: Pick<WorkflowTrigger, 'connection_id' | 'model' | 'change_types' | 'environment'>): Promise<WorkflowTrigger> {
    return request('POST', `/v1/snippets/${workflowId}/triggers`, input, 'apikey')
  },
  async updateWorkflowTrigger(workflowId: string, trigger: WorkflowTrigger): Promise<WorkflowTrigger> {
    return request('PATCH', `/v1/snippets/${workflowId}/triggers/${trigger.id}`, trigger, 'apikey')
  },
  async deleteWorkflowTrigger(workflowId: string, triggerId: string): Promise<void> {
    return request('DELETE', `/v1/snippets/${workflowId}/triggers/${triggerId}`, undefined, 'apikey')
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

  // KV store
  // Omitting namespace lists every namespace (the "All namespaces" filter); passing
  // 'default' lists only the default one.
  async listKVEntries(
    filters: { namespace?: string; prefix?: string; limit?: number; offset?: number } = {},
  ): Promise<KVEntryList> {
    const params = new URLSearchParams()
    if (filters.namespace) params.set('namespace', filters.namespace)
    if (filters.prefix) params.set('prefix', filters.prefix)
    if (filters.limit && filters.limit > 0) params.set('limit', String(Math.floor(filters.limit)))
    if (filters.offset !== undefined && filters.offset >= 0) params.set('offset', String(Math.floor(filters.offset)))
    const qs = params.toString()
    return request('GET', `/v1/kv/entries${qs ? `?${qs}` : ''}`, undefined, 'apikey')
  },

  async listKVNamespaces(): Promise<KVNamespace[]> {
    return request('GET', '/v1/kv/namespaces', undefined, 'apikey')
  },

  /** Reveals one plaintext value. Requires admin scope; throws on 403. */
  async revealKVEntry(namespace: string, key: string): Promise<KVEntry> {
    return request('POST', '/v1/kv/reveal', { namespace, key }, 'apikey', true, async response => {
      const raw = await response.text()
      return { ...JSON.parse(raw), value_raw: rawJSONField(raw, 'value') } as KVEntry
    })
  },

  async deleteKVEntry(namespace: string, key: string): Promise<void> {
    const params = new URLSearchParams({ namespace, key })
    return request('DELETE', `/v1/kv/entry?${params.toString()}`, undefined, 'apikey')
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
  async getInstanceInfo(): Promise<InstanceInfo> {
    return request('GET', '/v1/instance/info', undefined, 'none')
  },

  // Billing
  async getTenantPlan(): Promise<{ plan: string; valid: boolean; features: string[] }> {
    return request('GET', '/v1/tenant/plan', undefined, 'session')
  },

  async discoverSSO(org: string): Promise<{ available: boolean; protocol: string; display_name: string; start_url: string }> {
    return request('GET', `/v1/admin/auth/sso/discover?org=${encodeURIComponent(org)}`, undefined, 'none')
  },
  async getSSO(): Promise<SSOConnection | undefined> { return request('GET', '/v1/tenant/sso') },
  async saveSSO(value: Partial<SSOConnection>): Promise<SSOConnection> { return request('PUT', '/v1/tenant/sso', value) },
  async deleteSSO(): Promise<void> { return request('DELETE', '/v1/tenant/sso') },
  async testSSO(): Promise<SSOConnection> { return request('POST', '/v1/tenant/sso/test') },
  async activateSSO(): Promise<SSOConnection> { return request('POST', '/v1/tenant/sso/activate') },
  async setSSOEnforcement(enforced: boolean, breakGlassUserId: string): Promise<SSOConnection> {
    return request('POST', '/v1/tenant/sso/enforcement', { enforced, break_glass_user_id: breakGlassUserId })
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
