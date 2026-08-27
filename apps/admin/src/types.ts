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

export interface UserGroupMember {
  group_id: string
  user_id: string
  email?: string
  added_at: string
}

export interface IntegrationGroupGrant {
  group_id: string
  group_name?: string
  credential_profile_id: string
  granted_at: string
}

export interface UserGroup {
  id: string
  tenant_id: string
  name: string
  description: string
  created_at: string
  updated_at: string
  members: UserGroupMember[] | null
  integration_grants: IntegrationGroupGrant[] | null
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

export interface WorkflowTrigger {
  id: string
  workflow_id: string
  connection_id: string
  provider_config_key: string
  model: string
  change_types: Array<'added' | 'updated' | 'deleted'>
  environment: 'dev' | 'staging' | 'prod'
  enabled: boolean
  activated_at?: string
  last_delivery_at?: string
  last_error?: string
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

/** KV metadata as returned by list endpoints. Never carries `value` — see KVEntry to read one. */
export interface KVEntryMeta {
  id: string
  namespace: string
  key: string
  size_bytes: number
  expires_at?: string | null
  created_at: string
  updated_at: string
}

/** A single entry including its plaintext value. Requires admin scope via the reveal endpoint. */
export interface KVEntry extends KVEntryMeta {
  value: unknown
  /** Exact JSON text returned by the reveal endpoint, retained for lossless display. */
  value_raw: string
}

export interface KVNamespace {
  namespace: string
  keys: number
  size_bytes: number
}

export interface KVEntryList {
  items: KVEntryMeta[]
  total: number
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

export interface InstanceCapabilities {
  sandboxes?: boolean
  sandbox_profiles?: boolean
  sandbox_image_recipes?: boolean
  sandbox_operations?: boolean
  sandbox_snapshots?: boolean
  sandbox_events?: boolean
  sandbox_logs?: boolean
}

export interface InstanceInfo {
  cloud: boolean
  plan: string
  license_valid: boolean
  features: string[]
  capabilities?: InstanceCapabilities
}

export type SandboxDesiredState = 'running' | 'stopped' | 'deleted'
export type SandboxObservedState =
  | 'pending'
  | 'awaiting_capacity'
  | 'provisioning'
  | 'bootstrapping'
  | 'restoring'
  | 'running'
  | 'snapshotting'
  | 'stopping'
  | 'stopped'
  | 'recovering'
  | 'deleting'
  | 'failed'

export type SandboxOperationKind =
  | 'recipe_build'
  | 'create'
  | 'start'
  | 'stop'
  | 'restart'
  | 'snapshot'
  | 'restore'
  | 'recover'
  | 'delete'
  | 'snapshot_delete'

export type SandboxOperationState = 'queued' | 'claimed' | 'dispatched' | 'waiting' | 'succeeded' | 'failed' | 'cancelled'
export interface SandboxOperation {
  id: string
  sandbox_id?: string
  recipe_version_id?: string
  snapshot_id?: string
  retry_of_operation_id?: string
  kind: SandboxOperationKind
  state: SandboxOperationState
  requested_generation?: number
  attempt?: number
  max_attempts?: number
  deadline_at?: string
  failure_code?: string
  failure_message?: string
  result?: unknown
  created_at: string
  updated_at: string
}

export type SandboxSnapshotState = 'requested' | 'uploading' | 'verifying' | 'ready' | 'failed' | 'deleting' | 'deleted'

export interface SandboxSnapshot {
  id: string
  sandbox_id: string
  operation_id: string
  generation: number
  kind: 'periodic' | 'manual' | 'stop' | 'drain' | 'recovery'
  state: SandboxSnapshotState
  manifest_version: string
  total_bytes: number
  failure_code?: string
  failure_message?: string
  created_at: string
}

export interface SandboxProfile {
  id: string
  profile_family: string
  name: string
  version: string
  status: 'active' | 'unavailable' | 'retired'
  vcpu: number
  memory_mb: number
}

export interface SandboxRecipe {
  id: string
  name: string
  slug: string
  description: string
  created_at: string
  updated_at: string
}

export type SandboxRecipeStatus = 'queued' | 'building' | 'ready' | 'failed' | 'retired'

export interface SandboxRecipeVersion {
  id: string
  recipe_id: string
  version_number: number
  schema_version: string
  status: SandboxRecipeStatus
  failure_code?: string
  failure_message?: string
  document?: SandboxRecipeDocument
  created_at: string
}

export interface SandboxRecipeDocument {
  schema_version: '1'
  platform: 'linux'
  architecture: string
  base_image: string
  environment?: Record<string, string>
  install_groups?: SandboxRecipeInstallGroup[]
  profile_version_ids: string[]
  bootstrap?: { script: string; timeout_seconds: number }
  external_inputs?: SandboxExternalInput[]
  guest_protocol: string
}

export interface SandboxRecipeInstallGroup {
  repository_snapshot: string
  index_digest: string
  lock_digest: string
  packages: Array<{ name: string; version: string; digest: string }>
}

export interface SandboxExternalInput {
  url: string
  sha256: string
  size: number
}

export interface SandboxEvent {
  id: string
  type: string
  level: 'info' | 'warning' | 'error'
  message: string
  details?: unknown
  created_at: string
}

export interface SandboxLog {
  id: string
  source: string
  stream: string
  message: string
  created_at: string
}

export interface Sandbox {
  id: string
  name: string
  desired_state: SandboxDesiredState
  observed_state: SandboxObservedState
  recipe_version_id: string
  profile_version_id: string
  generation: number
  latest_snapshot_id?: string | null
  failure_code?: string
  failure_message?: string
  created_at: string
  updated_at: string
  available_actions?: SandboxOperationKind[]
}

export interface SandboxListResponse {
  items: Sandbox[]
  total: number
}

export interface SandboxDetailResponse {
  sandbox: Sandbox
  available_actions: SandboxOperationKind[]
}

export interface SandboxMutationResponse {
  sandbox?: Sandbox
  operation: SandboxOperation
  replayed: boolean
}

export interface SandboxCursorResponse<T> {
  items: T[]
  next_cursor?: string
}
