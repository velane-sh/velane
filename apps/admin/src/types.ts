export interface User {
  id: string
  email: string
  created_at: string
  updated_at: string
}

export interface SSOConnection {
  id: string
  protocol: 'oidc' | 'saml'
  display_name: string
  default_role: 'invoke' | 'manage'
  enabled: boolean
  enforced: boolean
  tested_at?: string
  break_glass_user_id?: string
  oidc_callback_url: string
  saml_metadata_url: string
  saml_acs_url: string
  config: { issuer_url?: string; client_id?: string; client_secret?: string; scopes?: string[]; idp_metadata_xml?: string }
}

export interface Session {
  session_token: string
  expires_at: string
}

export interface OrgMembership {
  tenant_id: string
  slug: string
  name: string
  role: string
}

export interface Branding {
  logo_url?: string
  accent_color?: string
  font_family?: string
  custom_domain?: string
  hide_branding?: boolean
}

export interface TenantMember {
  tenant_id: string
  user_id: string
  email: string
  role: string
  invited_at: string
}

export interface InviteToken {
  id: string
  tenant_id: string
  email: string
  role: string
  expires_at: string
  accepted_at?: string
  created_at: string
}

export interface UsageTopSnippet {
  snippet_id: string
  name: string
  invocations: number
  p95_ms: number
}

export interface UsageSummary {
  tenant_id: string
  window: string
  total_invocations: number
  error_rate: number
  avg_duration_ms: number
  top_snippets: UsageTopSnippet[]
}

export interface APIKey {
  id: string
  name: string
  scopes: string[]
  key_prefix: string
  key?: string // only present on creation
  last_used_at?: string
  created_at: string
}

export interface EgressPolicy {
  blocked_cidrs: string[]
  blocked_domains: string[]
}

export interface RuntimeLimits {
  max_timeout_ms: number
  max_memory_mb: number
  max_cpu_percent: number
}

export interface RuntimeSettings {
  timeout_ms: number
  max_memory_mb: number
  max_cpu_percent: number
}

export interface Snippet {
  id: string
  name: string
  slug: string
  language: string
  description: string
  created_at: string
}

export interface SnippetVersion {
  id: string
  snippet_id: string
  version_number: number
  code: string
  status: 'draft' | 'published' | 'archived'
  created_at: string
  timeout_ms: number
  max_memory_mb: number
  max_cpu_percent: number
}

export interface SnippetEnvironment {
  snippet_id: string
  env: string
  active_version_number: number | null
}

export interface Connection {
  id: string
  tenant_id: string
  provider: string
  alias: string
  provider_config_key: string
  credential_profile_id?: string
  nango_connection_id: string
  display_name: string
  created_at: string
  updated_at: string
}

export interface ConnectionField {
  type: string
  title: string
  description?: string
  example?: string
  optional?: boolean
  automated?: boolean
  prefix?: string
}

export interface NangoProvider {
  unique_key: string
  name: string
  auth_mode: string
  categories?: string[]
  default_scopes?: string[]
  docs?: string
  logo_url?: string
  connection_config?: Record<string, ConnectionField>
  credentials?: Record<string, ConnectionField>
}

export interface IntegrationConfig {
  id: string
  tenant_id: string
  alias: string
  name: string
  nango_provider_config_key: string
  credentials_type: string
  is_default: boolean
  provider: string
  oauth_scopes?: string
  connected?: boolean
  created_at: string
  updated_at: string
}

export interface MCPInfo {
  mcp_url: string
}

export interface Secret {
  id: string
  tenant_id: string
  snippet_id?: string
  name: string
  is_secret: boolean
  value?: string // present for variables (is_secret=false), absent for credentials
  environments: string[]
  created_at: string
  updated_at: string
}

export interface EmbedToken {
  id: string
  tenant_id: string
  allowed_snippet_ids: string[]
  expires_at: string
  created_by: string
  last_used_at: string | null
  created_at: string
}

export interface LogLine {
  stream: string
  text: string
}

export interface InvocationResult {
  output: unknown
  error: string
  stderr: string
  duration_ms: number
  exit_code: number
  invocation_id?: string
  status?: string
  logs?: LogLine[]
}

export type InvocationStatus =
  | 'pending'
  | 'running'
  | 'completed'
  | 'failed'
  | 'timeout'
  | 'oom_killed'

export interface InvocationSummary {
  id: string
  snippet_id: string
  version_id: string
  environment: string
  tenant_id: string
  status: InvocationStatus
  duration_ms: number
  peak_memory_mb: number
  cpu_ms: number
  created_at: string
  completed_at?: string
  invoke_mode: string
  payload_state: string
}

export interface Invocation extends InvocationSummary {
  input_payload: string
  input_ref?: string
  output: string
  output_ref?: string
  error?: string
  stderr: string
  stderr_ref?: string
  callback_url?: string
}

export interface InvocationLogResponse {
  snippet_id: string
  items: InvocationSummary[]
}
