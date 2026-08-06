# Velane — Build Phases

This document tracks the phased implementation plan for Velane. Each phase delivers a working, shippable increment. Phases build on each other — complete Phase N before starting Phase N+1.

---

## Phase 1 — Core Runtime (complete)

**Goal:** A fully runnable local stack that can execute code snippets synchronously.

### Delivered

- Go control plane HTTP API (chi router)
- Postgres schema with auto-applied migrations
- API key authentication (prefix lookup + bcrypt, `invoke` / `manage` / `admin` scopes)
- Snippet CRUD — create, list, get, delete
- Versioned deployments — each publish creates an immutable numbered version
- `dev` and `prod` environments per snippet
- Sync invocation: `POST /v1/invoke/{snippet}` → blocks until result
- ProcessExecutor — HTTP bridge to per-language executor containers (Bun, Python)
- Executor runtimes — Bun (`runner.ts`) and Python (`runner.py`) as persistent HTTP servers
- Tenant isolation enforced on every read and invocation
- Full test suite: 28 unit tests (zero deps) + integration tests (require `TEST_DATABASE_URL`)
- `docker-compose.yml` for local development

### Stack

- Control plane: Go 1.26, chi, pgx/v5, zap
- Runtimes: Bun 1.1, Python 3.12 + FastAPI
- Storage: Postgres 16
- Infrastructure: Docker Compose (local), Kubernetes + Terraform (production path)

---

## Phase 2 — Async, Streaming & Warm Pool (complete)

**Goal:** Support long-running and streaming snippets at scale; eliminate cold-start penalty for hot snippets.

### Delivered

- **Async invocation** — `X-Invoke-Mode: async` returns `202 { invocation_id }` immediately; snippet runs in background
- **Async polling** — `GET /v1/invocations/{id}` already exists; Phase 2 adds webhook delivery on completion (`callback_url` in invoke body)
- **Streaming invocation** — `X-Invoke-Mode: stream` returns `text/event-stream`; snippet yields chunks via `yield` (Python) or async generator (Bun)
- **Redis job queue** — async jobs enqueued to Redis, worker pool dequeues and dispatches to executor
- **Warm pool manager** — K8s Deployment per language; tenant-configurable `min_instances` per snippet; slot claim/release via Redis atomic ops
- **Version pinning** — callers can specify `?version=v3` to invoke a pinned version instead of the active one

### New API surface

```
POST /v1/invoke/{snippet}
  X-Invoke-Mode: async    → 202 { invocation_id, status_url }
  X-Invoke-Mode: stream   → 200 text/event-stream
  Body: { ..., callback_url?: string }  (async only)
```

### New infrastructure

- Redis (added to docker-compose and Terraform)
- Worker service (Go) — pulls from Redis queue, dispatches to executor, updates invocation record, fires webhook
- `SnippetEnvironment.min_instances` — warm pool manager watches this and maintains ready slots

---

## Phase 3 — Staging, Canary & Secrets (complete)

**Goal:** Full three-environment promotion flow with safe traffic-splitting and secret injection.

### Delivered

- **Staging environment** — third env (`dev` → `staging` → `prod`); `POST /v1/snippets/{id}/versions/{num}/publish?env=staging`
- **Canary traffic splitting** — `POST /v1/snippets/{id}/canary` sets `{ version_id, percent }` on the prod environment; Traffic Router sends X% to canary version, 100-X% to stable
- **Rollback** — re-publish any archived version to instantly swap active version
- **Secrets manager** — `POST /v1/secrets` to store encrypted key-value pairs; injected as env vars at invocation time; never returned in API responses
- **Egress policy engine** — per-tenant IP/CIDR blocklist enforced via iptables inside executor net namespace; default blocks `169.254.0.0/16`, RFC1918 ranges, and configurable domain sinkhole

### New API surface

```
POST   /v1/secrets                    → create secret (manage scope)
GET    /v1/secrets                    → list secret names (never values)
DELETE /v1/secrets/{id}              → delete secret

POST   /v1/snippets/{id}/canary       → set canary { version_id, percent }
DELETE /v1/snippets/{id}/canary       → remove canary (full traffic to active)

GET    /v1/tenant/egress              → get egress policy
PUT    /v1/tenant/egress              → update egress policy (admin scope)
```

### New data model

```sql
secrets (id, tenant_id, snippet_id nullable, name, value_encrypted, environments[])
snippet_environments.canary_version_id
snippet_environments.canary_pct
```

---

## Phase 4 — Developer Surfaces (complete)

**Goal:** Give engineers three ways to write and deploy snippets: Web IDE, CLI, and Git push-to-deploy.

### Delivered

- **Web IDE** — Monaco editor in React; syntax highlighting for Bun/Python; inline error display; test-invoke panel; version history sidebar; publish button with env selector
- **CLI tool** (`velane` binary, distributed via npm and Homebrew)
  ```
  velane login               # authenticate, store key in system keychain
  velane snippets list
  velane snippets push <file>  # create/update draft, optionally publish
  velane invoke <slug> [--env prod] [--input '{}']
  velane logs <slug>
  ```
- **Git webhook integration** — connect a GitHub/GitLab repo; push to `main` → deploy to `staging`; push a tag (`v*`) → deploy to `prod`; PR branch → deploy to `dev` (preview env)

### New services

- `web-ide/` — Vite + React SPA, deployed to `app.velane.io`
- `cli/` — Go CLI (cobra), distributed as single binary
- Webhook receiver endpoint on control plane: `POST /v1/webhooks/git`

---

## Phase 5 — Observability (complete)

**Goal:** Give engineers full visibility into every invocation — logs, metrics, and replay.

### Delivered

- **Invocation schema upgrades** — added `input_ref`, `output_ref`, `stderr_ref`, and `cpu_ms` to invocation model/migrations
- **Log query API** — `GET /v1/logs/snippets/{id}` implemented with filters (`limit`, `env`, `status`, `start_time`, `end_time`)
- **Metrics API** — `GET /v1/metrics/snippets/{id}?window=1h|24h|7d` implemented with aggregates (`count`, `avg`, `p50`, `p95`, `p99`) + time series
- **Replay API** — `POST /v1/invocations/{id}/replay` implemented with manage-scope enforcement + tenant-level replay opt-in
- **Replay privacy toggle** — tenant-level `replay_enabled` gate enforced before replay
- **Post-invocation observability hooks** — scheduler + worker now emit completion events into pluggable observability pipeline interfaces
- **Multi-tenant namespace provider** — added pluggable `TenantProvider` interface with `SharedTenantProvider` and `NamespacedTenantProvider`
- **Infra placeholders wired** — ClickHouse and MinIO services + related env vars added to local compose for Phase 5 runtime plumbing

### New infrastructure

- ClickHouse (added to docker-compose and Terraform)
- MinIO / S3 bucket (log and replay payload storage)
- `ObservabilityWorker` — async goroutine pool that ships log lines and metrics rows after each invocation

### Notes

- Current implementation uses Postgres-backed query paths for logs and metrics APIs, with external-store interfaces in place for phased rollout.
- ClickHouse/MinIO are wired in local infrastructure and can be enabled incrementally behind the observability interfaces.

---

## Phase 6 — MCP Server (Cursor / Claude Code Integration) (complete)

**Goal:** Let engineers connect Cursor, Claude Code, or any MCP-compatible AI agent directly to Velane to generate and deploy snippets without leaving their IDE.

### Delivered

- **MCP server service** — standalone Go service at `services/mcp-server`
- **Two transports implemented**:
  - HTTP/SSE hosted at `/mcp` (zero install — add URL to IDE config)
  - stdio mode via `cmd/stdio` (for IDEs that only support stdio)
- **JSON-RPC methods implemented**:
  - `initialize`
  - `ping`
  - `tools/list`
  - `tools/call`
- **14 MCP tools exposed** and wired to control-plane APIs:

| Tool              | Scope needed |
| ----------------- | ------------ |
| `list_snippets`   | invoke       |
| `get_snippet`     | invoke       |
| `create_snippet`  | manage       |
| `update_draft`    | manage       |
| `publish_snippet` | manage       |
| `invoke_snippet`  | invoke       |
| `get_logs`        | invoke       |
| `list_secrets`    | manage       |
| `set_secret`      | manage       |
| `get_metrics`     | invoke       |
| `kv_get`          | invoke       |
| `kv_set`          | manage       |
| `kv_delete`       | manage       |
| `kv_list`         | invoke       |

- **Auth passthrough** — Bearer token forwarded to control-plane for scope/tenant enforcement
- **Docker Compose integration** — `mcp-server` service added, listening on `:8090`, configured with `CONTROL_PLANE_URL=http://control-plane:8080`
- **MCP usage docs** — root README includes compose startup and JSON-RPC usage examples

### Developer setup (after this phase ships)

```json
// .cursor/mcp.json or ~/.claude/mcp.json
{
  "mcpServers": {
    "velane": {
      "url": "https://api.velane.io/mcp",
      "headers": { "Authorization": "Bearer vl_xxxx" }
    }
  }
}
```

### New services

- `services/mcp-server/` — Go service, thin wrapper over control plane API

---

## Phase 7 — Embeddable Dashboard (complete)

**Goal:** Let any org embed a white-label snippet browser directly into their own portal via a single `<iframe>` tag.

### Scope

- **Embed token API** — `POST /v1/embed/tokens` issues short-lived, read-only tokens scoped to a tenant (optionally to specific snippet IDs)
- **Embed app** — React SPA served from `embed.velane.io`:
  - Snippet list with search, language filter, env filter
  - Snippet detail: code viewer (Monaco, read-only), version sidebar, env status badges, recent invocations summary, p95 latency badge
  - Environment switcher (dev / staging / prod)
- **White-label theming** — theme via URL params (`?theme=dark&accent=6366f1`) or persisted branding config per tenant (`logo_url`, `accent_color`, `font_family`)
- **iframe security** — embed subdomain uses `Content-Security-Policy: frame-ancestors *`; main app keeps `frame-ancestors 'none'`

### Integration (one line for orgs)

```html
<iframe
  src="https://embed.velane.io?token=et_xxxx"
  width="100%"
  height="700"
  frameborder="0"
/>
```

### New services

- `services/embed-dashboard/` — Vite + React, deployed to `embed.velane.io`

---

## Phase 8 — Tenant Admin Dashboard & White-Label (complete)

**Goal:** Give each tenant org a self-serve admin dashboard to manage their Velane account, configure white-label branding for the embedded dashboard, and govern their engineers' access.

### Delivered

- **Email/password auth** — `AuthProvider` interface + `PasswordProvider` (bcrypt + Postgres sessions); designed for future OIDC/SAML swap via interface
- **Session auth middleware** — `SessionAuth` middleware validates Bearer session tokens; chainable with API key auth
- **Admin auth API** — `POST /v1/admin/auth/register`, `POST /v1/admin/auth/login`, `POST /v1/admin/auth/logout`, `GET /v1/admin/auth/me`
- **Invite flow** — admins generate a signed invite token (72h TTL) and share a `/register?invite=xxx` link; invitee registers and is auto-added as tenant member
- **Team member management API** — list, invite, remove members; list pending invites
- **Branding API** — GET/PUT branding config per tenant (logo, accent color, font family, custom domain, hide-branding toggle); extends existing `branding` JSONB column on tenants
- **Usage aggregation API** — `GET /v1/tenant/usage?window=24h|7d|30d` aggregates invocations across all tenant snippets
- **API key management API** — `GET` and `DELETE` endpoints for API keys (create already existed)
- **Migration 007** — `users`, `user_sessions`, `tenant_members`, `invite_tokens` tables
- **Admin SPA** (`apps/admin/`) — Vite + React + TypeScript + Tailwind:
  - Login / Register pages with invite token pre-fill from URL
  - Dashboard layout with sidebar navigation
  - Overview (stats cards: API keys, members, 24h invocations)
  - API Keys (create with scope checkboxes, one-time raw key display, revoke)
  - Team (invite with role selector, member list with remove, pending invites)
  - Branding (logo URL + preview, color picker, font, custom domain, hide-branding toggle, live preview panel)
  - Usage (window selector, stats cards, top-snippets table)
  - Egress Policy (add/remove CIDRs and domains with tag chips)

### New data model

```sql
-- Migration 007
CREATE TABLE users (id, email UNIQUE, password_hash, created_at, updated_at);
CREATE TABLE user_sessions (id, user_id REFERENCES users, token_hash UNIQUE, expires_at, created_at);
CREATE TABLE tenant_members (tenant_id, user_id, role CHECK IN ('invoke','manage','admin'), invited_at, PRIMARY KEY(tenant_id, user_id));
CREATE TABLE invite_tokens (id, tenant_id, email, role, token_hash UNIQUE, expires_at, accepted_at, created_at);
-- branding column was already added in migration 006 — extended with custom_domain, hide_branding fields
```

### New API surface

```
POST /v1/admin/auth/register          → create user (+ accept invite if token provided)
POST /v1/admin/auth/login             → email/password login → session token
POST /v1/admin/auth/logout            → invalidate session
GET  /v1/admin/auth/me                → current user (session auth)

GET  /v1/tenant/branding              → get branding config (invoke scope)
PUT  /v1/tenant/branding              → update branding (admin scope)

GET    /v1/tenant/members                      → list members (admin scope)
POST   /v1/tenant/members/invite               → invite by email (admin scope)
DELETE /v1/tenant/members/{userID}             → remove member (admin scope)
GET    /v1/tenant/members/invites              → list pending invites (admin scope)

GET /v1/tenant/usage                    → usage summary (admin scope)

GET    /v1/tenant/api-keys              → list keys without raw values (admin scope)
DELETE /v1/tenant/api-keys/{id}         → revoke key (admin scope)
```

### New services

- `apps/admin/` — Vite + React SPA, deployable to `admin.velane.io`

### Relationship to Phase 7 embed

Phase 7 builds the embed app and accepts branding config as URL params. Phase 8 adds the admin UI where orgs configure that branding through a proper form — the embed app fetches it from `GET /v1/tenant/branding` on load. No changes to the embed app itself.

---

## Phase 9 — Hardening & Advanced Features (complete)

**Goal:** Production-grade security hardening, full schema-driven API docs, and enterprise auth.

### Delivered

- **JWT auth (RS256)** — replaces Postgres session tokens with stateless RS256 JWTs; access token 15min, refresh token 7d stored in Postgres with rotation; JWKS endpoint at `GET /.well-known/jwks.json`; ephemeral key generated with warning if `JWT_PRIVATE_KEY` not set
- **Firecracker executor plugin** — optional `Executor` interface implementation via `EXECUTOR_TYPE=firecracker`; requires `/dev/kvm`; full interface with documented VM lifecycle (jailer → kernel boot → vsock → result); stub implementation compiles and is interface-complete; real Firecracker binary + rootfs images needed for bare-metal deployment
- **Seccomp profile for executor containers** — `services/executor-runtime/seccomp-executor.json`; blocks `ptrace`, `mount`, `CLONE_NEWUSER`, `pevl_event_open`, `kexec`, `settimeofday`, kernel module ops, and more; applied to `bun-executor` and `python-executor` in `docker-compose.yml`
- **Audit log** — append-only Postgres table (`audit_log`); all management actions logged (publish, secret_create, secret_delete, egress_update, member_invite, member_remove, api_key_create, api_key_revoke, branding_update, canary_set, canary_clear); fire-and-forget logger; queryable via `GET /v1/tenant/audit-log` (admin scope)
- **OpenAPI generation**: deferred

### New API surface

```
GET  /.well-known/jwks.json              → JWKS public key (no auth)
POST /v1/admin/auth/refresh              → exchange refresh token for new pair (no auth)
GET  /v1/tenant/audit-log                → list audit log entries (admin scope)
```

### New data model (migration 008)

```sql
DROP TABLE user_sessions;  -- replaced by JWT
CREATE TABLE refresh_tokens (id, user_id, token_hash UNIQUE, expires_at, revoked_at, created_at);
CREATE TABLE audit_log (id, tenant_id, actor_id, actor_type, action, resource_id, metadata JSONB, created_at);
```

---

## Phase 10 — Cross-Invocation KV Store (complete)

**Goal:** Persist small JSON state between invocations without putting state in
snippet code, request payloads, or secrets.

### Delivered

- **Tenant-scoped KV entries** — Postgres-backed `kv_entries` with namespaces,
  TTL, lazy expiry, background reaping, and per-tenant quotas
- **Built-in libraries** — `@velane/store` for Bun and `velane.store` for
  Python; the default namespace is tenant-wide and `namespace()` opts into
  separate workflow state
- **Public KV API** — authenticated `/v1/kv/*` routes derive tenant context
  from the credential and enforce tenant predicates on each query
- **MCP tools** — `kv_get`, `kv_set`, `kv_delete`, and `kv_list`
- **Data Store UI** — browse and delete entries, with values structurally
  omitted from list responses and audited admin-scope reveal
- **Plaintext warning** — values are not encrypted; redaction is a UX and
  auditing boundary, not a confidentiality boundary

### Known exposure

`/v1/proxy/*` and `/v1/internal/kv/*` trust a caller-supplied
`X-Velane-Tenant` header and are reachable from outside in shipped topologies.
This accepted exposure remains until separate-listener hardening lands. See the
documentation's Trust-Header Routes page and known limitations before a
production rollout.

---

## Summary

| Phase | Theme          | Key deliverable                                                             |
| ----- | -------------- | --------------------------------------------------------------------------- |
| 1     | Core Runtime   | Sync invocation, API keys, Postgres, docker-compose                         |
| 2     | Scale          | Async + streaming, Redis queue, warm pool                                   |
| 3     | Safety         | Staging, canary, secrets, egress policy                                     |
| 4     | DX             | Web IDE (Monaco + test runner + log streaming), CLI, git push-to-deploy     |
| 5     | Visibility     | Logs, metrics, replay, multi-tenant K8s namespaces                          |
| 6     | AI Integration | MCP server (Cursor / Claude Code integration)                               |
| 7     | Embedding      | iframe embed dashboard, embed tokens, read-only snippet viewer              |
| 8     | Tenant Admin   | Admin portal, white-label branding config, team management, usage dashboard |
| 9     | Hardening      | Firecracker, OpenAPI gen, JWT, seccomp profiles, audit log                  |
| 10    | KV Store       | Cross-invocation state, KV API, built-in libraries, MCP and admin UI         |
