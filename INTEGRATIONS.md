# Velane — Integrations Design

This document covers the full design for Velane's integration system: replacing the hand-built platform library model with a Nango-powered OAuth connection layer, an internal proxy, and a built-in `@velane/integrations` client that snippets use to call any of 800+ connected APIs without ever seeing a credential.

Delivered across three phases (10, 11, 12) that build on the complete Phase 1–9 foundation.

---

## Motivation

The current platform library model has three problems:

1. **Auth is per-library** — each Salesforce library implements its own OAuth flow. Adding a new integration means writing auth from scratch again.
2. **Token management is manual** — users paste raw API keys as Variables. Tokens expire silently. OAuth refresh is the user's problem.
3. **Coverage is tiny** — only Salesforce libraries exist. Getting to 50 integrations means 50× the auth code.

The replacement model:
- **Nango** manages OAuth for 800+ providers. Users click Connect, tokens are stored and refreshed automatically.
- **An internal proxy** on the control plane forwards snippet API calls to Nango, which forwards them to the provider. No token ever reaches snippet code.
- **`@velane/integrations`** is a built-in (one tiny class, always available) that wraps the proxy. Snippets call `integration('github').get('/user/repos')` and that's it.
- **MCP tools** teach coding agents which providers are connected and what endpoints to call, so agents can write correct snippet code without human intervention.

Variables (raw key-value env vars) remain unchanged — they cover API keys that don't use OAuth.

---

## Architecture

```
┌──────────────────────────────────────────────────────────────────────┐
│                         PUBLIC INTERNET                               │
│                                                                      │
│  User browser    Coding agent (Cursor / Claude Code)                 │
│       │                      │                                       │
│       │ HTTPS                │ MCP over stdio / HTTP                 │
└───────┼──────────────────────┼───────────────────────────────────────┘
        │                      │
        ▼                      ▼
┌──────────────────────────────────────────────────────────────────────┐
│  DOCKER INTERNAL NETWORK                                              │
│                                                                      │
│  ┌─────────────────┐   ┌──────────────────┐   ┌──────────────────┐  │
│  │   Admin UI      │   │   Control Plane   │   │   MCP Server     │  │
│  │  :8092          │   │   :8080           │   │   :8090          │  │
│  │                 │   │                   │   │                  │  │
│  │ Integrations tab│──►│ /v1/connections/* │   │ list_connections │  │
│  │ (connect OAuth) │   │ /v1/integrations/*│◄──│ get_integration_ │  │
│  │                 │   │ /v1/proxy/*       │   │   docs           │  │
│  └─────────────────┘   └────────┬──────────┘   └──────────────────┘  │
│                                 │                                     │
│                    ┌────────────▼────────────┐                       │
│                    │       Nango :3003        │                       │
│                    │   (no host port binding) │                       │
│                    │                         │                       │
│                    │  OAuth token storage     │                       │
│                    │  Token refresh           │                       │
│                    │  API proxy               │                       │
│                    └────────────┬────────────┘                       │
│                                 │                                     │
│  ┌──────────────────────────────┼──────────────────────────────────┐ │
│  │  Executor containers         │                                   │ │
│  │                              │                                   │ │
│  │  Snippet code                │  (egress goes out via Nango,      │ │
│  │    → @velane/integrations    │   not directly to provider)       │ │
│  │    → HTTP to control-plane   │                                   │ │
│  │      /v1/proxy/github/...    │                                   │ │
│  └──────────────────────────────┼──────────────────────────────────┘ │
└──────────────────────────────────┼───────────────────────────────────┘
                                   │ HTTPS
                                   ▼
                        GitHub / Salesforce / Slack / ...
```

**Key security property:** Nango has no `ports:` mapping in docker-compose — it is never reachable from outside the Docker network. The control plane proxy endpoint is registered on the public control-plane router without public authentication and is reachable in shipped topologies. Snippet code calls the control plane (always whitelisted) and never directly calls Nango or provider APIs.

---

## What Gets Removed

| Item | Location | Reason |
|---|---|---|
| All Salesforce platform libraries | `platform-libraries/bun/salesforce-*/` | Replaced by `@velane/integrations` proxy |
| All Salesforce Python libraries | `platform-libraries/python/salesforce-*/` | Same |
| Libraries tab (nav + route) | `DashboardLayout.tsx`, `App.tsx` | Replaced by Integrations tab |
| LibrariesPage | `apps/admin/src/pages/LibrariesPage.tsx` | Replaced |
| LibraryEditorPage | `apps/admin/src/pages/LibraryEditorPage.tsx` | Replaced |
| LibrariesHandler | `services/control-plane/internal/api/handlers/libraries.go` | Replaced by ConnectionsHandler + IntegrationsHandler |
| Library routes in router | `internal/api/router.go` `/v1/libraries/*` | Replaced |
| Library store methods | `internal/store/postgres/` | Replaced |
| Migration 010_libraries.sql table | Postgres | Drop `libraries` + `library_versions` tables in new migration |

---

## Data Model

### Migration 012 — `connections`

```sql
CREATE TABLE connections (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider            TEXT NOT NULL,   -- nango provider slug: "github", "salesforce", "slack"
  nango_connection_id TEXT NOT NULL,   -- ID passed to Nango (we use tenant_id as the value)
  display_name        TEXT,            -- e.g. "GitHub (acme-corp)"
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(tenant_id, provider)
);

CREATE INDEX idx_connections_tenant ON connections(tenant_id);
```

**Connection ID convention:** `nango_connection_id = tenant_id`. One connection per provider per tenant. All snippets in the tenant share the same connected account for a given provider.

**No token storage in Velane's DB** — Nango owns that entirely. Velane only stores which providers a tenant has connected, so the UI can show status without calling Nango for every page load.

### Migration 012 also drops library tables

```sql
DROP TABLE IF EXISTS library_versions;
DROP TABLE IF EXISTS libraries;
```

---

## Component Design

### 1. Nango (self-hosted)

Nango runs as a Docker service on the internal network. Its Postgres database is the existing Velane Postgres (separate schema) or a dedicated database depending on operator preference.

**docker-compose addition:**

```yaml
nango:
  image: nangohq/nango:latest
  restart: unless-stopped
  environment:
    NANGO_DATABASE_URL: ${NANGO_DATABASE_URL:-postgresql://velane:velane@postgres:5432/nango}
    SERVER_PORT: 3003
    NANGO_SECRET_KEY: ${NANGO_SECRET_KEY}
    NANGO_PUBLIC_KEY: ${NANGO_PUBLIC_KEY}
  depends_on:
    - postgres
  # No ports: — intentionally not exposed to host or internet
  networks:
    - internal

control-plane:
  environment:
    NANGO_INTERNAL_URL: http://nango:3003
    NANGO_SECRET_KEY: ${NANGO_SECRET_KEY}
    NANGO_PUBLIC_KEY: ${NANGO_PUBLIC_KEY}
```

**Provider configuration:** Nango requires each OAuth provider to be configured with client ID + secret (your Velane platform's OAuth app credentials, not the user's). These are set once by the Velane operator via Nango's dashboard or API. Users never touch OAuth app credentials — they just click Connect.

---

### 2. Connections API

New handler: `internal/api/handlers/connections.go`

```
POST   /v1/tenant/connections/session
         → create a Nango Connect session token for the OAuth popup
         → body: { provider: "github" }
         → returns: { session_token: "...", expires_at: "..." }
         → scope: manage

GET    /v1/tenant/connections
         → list all connections for the tenant (from local DB)
         → returns: [{ provider, display_name, connected, created_at }]
         → scope: invoke

DELETE /v1/tenant/connections/{provider}
         → disconnect: deletes from local DB + calls Nango to delete the connection
         → scope: manage
         → audited: connection_disconnect

GET    /v1/integrations
         → list all 800+ available providers from Nango /providers (cached 1h)
         → no auth required (public catalog)
         → returns: [{ unique_key, name, auth_mode, categories, logo }]

GET    /v1/integrations/{provider}/docs
         → returns merged metadata: bundled docs (top 60) or Nango fallback + generated example
         → scope: invoke
```

**Session token flow:**

```
Admin UI                    Control Plane               Nango
   │                             │                        │
   │  POST /connections/session  │                        │
   │  { provider: "github" }     │                        │
   │ ─────────────────────────► │                        │
   │                             │  POST /connect/sessions│
   │                             │  { end_user_id: tenantID, allowed_integrations: ["github"] }
   │                             │ ──────────────────────►│
   │                             │  { token: "nango_..." }│
   │                             │ ◄──────────────────────│
   │  { session_token }          │                        │
   │ ◄───────────────────────── │                        │
   │                             │                        │
   │  (frontend opens Nango Connect UI with session_token)
   │  (user completes OAuth in popup)
   │  (Nango stores token, fires webhook or frontend callback)
   │                             │                        │
   │  POST /connections          │                        │
   │  { provider: "github" }     │                        │
   │ ─────────────────────────► │                        │
   │  (control plane writes      │                        │
   │   connection row to DB)     │                        │
```

---

### 3. Integration Proxy

New route group on the control plane, without public authentication middleware.
It trusts `X-Velane-Tenant`; callers that can reach the control-plane port can
choose that header in shipped topologies.

```
POST/GET/PATCH/PUT/DELETE  /v1/proxy/{provider}/*
```

**Handler logic:**

```go
func (h *ConnectionsHandler) Proxy(w http.ResponseWriter, r *http.Request) {
    tenantID := r.Header.Get("X-Velane-Tenant")
    provider := chi.URLParam(r, "provider")
    path     := "/" + chi.URLParam(r, "*")

    // verify this tenant has an active connection for this provider
    conn, err := h.store.GetConnection(r.Context(), tenantID, provider)
    if err != nil {
        writeError(w, http.StatusBadRequest, "no connection found for "+provider)
        return
    }

    // forward to Nango proxy — Nango handles token injection + refresh
    h.nango.Proxy(w, r, NangoProxyRequest{
        ProviderConfigKey: provider,
        ConnectionID:      conn.NangoConnectionID,
        Method:            r.Method,
        Endpoint:          path,
        Body:              r.Body,
        ContentType:       r.Header.Get("Content-Type"),
    })
}
```

**Routing strategy:** The proxy route is registered on the control plane's
single `chi.Router` at `/v1/proxy` without public auth middleware. It trusts
`X-Velane-Tenant` and is reachable from outside in shipped topologies; this is
an accepted exposure. Moving the trust-header route families to a separate,
never-published listener is tracked separately.

For self-hosted operators running without Kubernetes, Docker Compose still
publishes the control-plane port. Its `networks:` configuration does not prevent
an outside caller that reaches that port from setting `X-Velane-Tenant`.

**Env vars injected at invocation time:**

```go
// in invocationsH.Invoke, before dispatching to executor:
envVars["VELANE_PROXY_URL"] = "http://control-plane:8080"
envVars["VELANE_TENANT_ID"] = tenant.ID
// existing secrets continue to be injected as before
```

---

### 4. `@velane/integrations` Built-in Library

Replaces all Salesforce library classes. One file per language, always available as a platform library, no user install.

**Bun — `platform-libraries/bun/integrations/index.ts`:**

```typescript
const PROXY_URL = process.env.VELANE_PROXY_URL!
const TENANT_ID = process.env.VELANE_TENANT_ID!

class IntegrationClient {
  constructor(private provider: string) {}

  private async req(method: string, endpoint: string, body?: unknown): Promise<unknown> {
    const res = await fetch(`${PROXY_URL}/v1/proxy/${this.provider}${endpoint}`, {
      method,
      headers: {
        'Content-Type':    'application/json',
        'X-Velane-Tenant': TENANT_ID,
      },
      body: body !== undefined ? JSON.stringify(body) : undefined,
    })
    if (!res.ok) {
      const text = await res.text().catch(() => res.statusText)
      throw new Error(`[${this.provider}] ${method} ${endpoint} → ${res.status}: ${text}`)
    }
    const ct = res.headers.get('content-type') ?? ''
    if (ct.includes('application/json')) return res.json()
    return res.text()
  }

  get(endpoint: string)                    { return this.req('GET',    endpoint) }
  post(endpoint: string, body?: unknown)   { return this.req('POST',   endpoint, body) }
  patch(endpoint: string, body?: unknown)  { return this.req('PATCH',  endpoint, body) }
  put(endpoint: string, body?: unknown)    { return this.req('PUT',    endpoint, body) }
  delete(endpoint: string)                 { return this.req('DELETE', endpoint) }
}

export function integration(provider: string): IntegrationClient {
  return new IntegrationClient(provider)
}
```

**meta.json:**

```json
{
  "name": "Integrations",
  "integration": "Velane",
  "description": "Built-in proxy client for all OAuth-connected integrations. Call integration('provider').get('/endpoint') in any snippet."
}
```

**Python — `platform-libraries/python/integrations/module.py`:**

```python
import os, json
from urllib.request import urlopen, Request
from urllib.error import HTTPError

_PROXY_URL  = os.environ.get("VELANE_PROXY_URL", "")
_TENANT_ID  = os.environ.get("VELANE_TENANT_ID", "")

class IntegrationClient:
    def __init__(self, provider: str):
        self._provider = provider

    def _req(self, method: str, endpoint: str, body=None):
        url  = f"{_PROXY_URL}/v1/proxy/{self._provider}{endpoint}"
        data = json.dumps(body).encode() if body is not None else None
        req  = Request(url, data=data, method=method, headers={
            "Content-Type":    "application/json",
            "X-Velane-Tenant": _TENANT_ID,
        })
        try:
            with urlopen(req) as r:
                raw = r.read()
                ct  = r.headers.get("Content-Type", "")
                return json.loads(raw) if "application/json" in ct else raw.decode()
        except HTTPError as e:
            raise RuntimeError(f"[{self._provider}] {method} {endpoint} → {e.code}: {e.read().decode()}") from e

    def get(self, endpoint: str):               return self._req("GET",    endpoint)
    def post(self, endpoint: str, body=None):   return self._req("POST",   endpoint, body)
    def patch(self, endpoint: str, body=None):  return self._req("PATCH",  endpoint, body)
    def put(self, endpoint: str, body=None):    return self._req("PUT",    endpoint, body)
    def delete(self, endpoint: str):            return self._req("DELETE", endpoint)

def integration(provider: str) -> IntegrationClient:
    return IntegrationClient(provider)
```

**Import paths:**

```typescript
// Bun
import { integration } from '@velane/integrations'

// Python
from velane.integrations import integration
```

---

### 5. Admin UI — Integrations Tab

Replaces `LibrariesPage.tsx` and `LibraryEditorPage.tsx`. New file: `apps/admin/src/pages/IntegrationsPage.tsx`.

**Nav change in `DashboardLayout.tsx`:**

```typescript
// Remove:
{ to: '/dashboard/libraries', label: 'Libraries', icon: BookOpen, embedHidden: false }

// Add:
{ to: '/dashboard/integrations', label: 'Integrations', icon: Plug, embedHidden: false }
```

**Route changes in `App.tsx`:**

```typescript
// Remove:
<Route path="libraries" element={<LibrariesPage />} />
<Route path="libraries/:id" element={<LibraryEditorPage />} />

// Add:
<Route path="integrations" element={<IntegrationsPage />} />
```

**IntegrationsPage layout:**

```
┌────────────────────────────────────────────────────────────┐
│  Integrations                                               │
│                                                            │
│  [Search providers...]      [All ▾] [CRM] [Dev Tools]     │
│                             [Email] [Calendar] [Storage]  │
├────────────────────────────────────────────────────────────┤
│  Connected (3)                                             │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐                 │
│  │ ● GitHub │  │ ●Salesf. │  │ ● Slack  │                 │
│  │          │  │          │  │          │                 │
│  │ Connected│  │ Connected│  │ Connected│                 │
│  │[Disconnect│  │[Disconnect│  │[Disconnect│               │
│  └──────────┘  └──────────┘  └──────────┘                 │
│                                                            │
│  Available (797)                                           │
│  ┌──────────┐  ┌──────────┐  ┌──────────┐  ┌──────────┐  │
│  │  HubSpot │  │  Notion  │  │  Linear  │  │  Stripe  │  │
│  │          │  │          │  │          │  │          │  │
│  │  CRM     │  │  Docs    │  │  PM      │  │  Billing │  │
│  │[Connect →│  │[Connect →│  │[Connect →│  │[Connect →│  │
│  └──────────┘  └──────────┘  └──────────┘  └──────────┘  │
│  ...                                                       │
└────────────────────────────────────────────────────────────┘
```

**Connect flow:**

```typescript
async function handleConnect(provider: string) {
  // 1. get session token from backend
  const { session_token } = await api.post('/v1/tenant/connections/session', { provider })

  // 2. open Nango Connect popup
  const nango = new Nango({ connectSessionToken: session_token })
  await nango.openConnectUI()

  // 3. record connection in DB
  await api.post('/v1/tenant/connections', { provider })

  // 4. refresh connection list
  refetch()
}
```

**Data sources:**
- Provider catalog: `GET /v1/integrations` (from Nango `/providers`, cached 1h) — 800+ entries
- Connected status: `GET /v1/tenant/connections` (from Velane DB) — fast, no Nango call
- Search + category filter: client-side against the cached catalog

---

### 6. MCP Tools

New tools added to `services/mcp-server/internal/tools/defaults.go`.

**`list_connections`**

```go
r.add(Tool{
    Name: "list_connections",
    Description: `List OAuth integrations connected for this tenant.

IMPORTANT — how to use integrations in snippet code:

Bun/TypeScript:
  import { integration } from '@velane/integrations'
  const client = integration('github')                          // provider slug
  const user   = await client.get('/user')                     // GET
  const issue  = await client.post('/repos/org/repo/issues',   // POST
                   { title: 'Bug', body: 'Details' })
  await client.patch('/repos/org/repo/issues/1', { state: 'closed' })
  await client.delete('/repos/org/repo/labels/old')

Python:
  from velane.integrations import integration
  client = integration("salesforce")
  cases  = client.get("/services/data/v60.0/sobjects/Case/describe")
  result = client.post("/services/data/v60.0/sobjects/Case",
             {"Subject": "Login issue", "Status": "New"})

Methods: .get(path)  .post(path, body)  .patch(path, body)  .put(path, body)  .delete(path)
Paths are the provider's native API paths — use get_integration_docs to find them.
@velane/integrations is always available. No install, no credentials, no imports beyond the line above.`,
    InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
    Handle: func(ctx context.Context, authHeader string, _ map[string]any) (any, error) {
        var out []map[string]any
        if err := r.client.Get(ctx, authHeader, "/v1/connections", &out); err != nil {
            return nil, err
        }
        return out, nil
    },
})
```

**`get_integration_docs`**

```go
r.add(Tool{
    Name:        "get_integration_docs",
    Description: "Get API endpoints, base URL, and a working code example for a specific integration provider. Call this before writing snippet code that uses an integration.",
    InputSchema: map[string]any{
        "type":     "object",
        "required": []string{"provider"},
        "properties": map[string]any{
            "provider": map[string]any{
                "type":        "string",
                "description": "Provider slug, e.g. github, salesforce, slack, hubspot, notion, linear, stripe, shopify",
            },
        },
    },
    Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
        provider, err := toString(args, "provider", true)
        if err != nil {
            return nil, err
        }
        var out map[string]any
        path := "/v1/integrations/" + url.PathEscape(provider) + "/docs"
        if err := r.client.Get(ctx, authHeader, path, &out); err != nil {
            return nil, err
        }
        return out, nil
    },
})
```

**Response shape from `/v1/integrations/{provider}/docs`:**

```json
{
  "provider":  "github",
  "name":      "GitHub",
  "base_url":  "https://api.github.com",
  "docs_url":  "https://docs.github.com/en/rest",
  "auth_mode": "OAUTH2",
  "common_endpoints": [
    { "method": "GET",  "path": "/user",                             "description": "Authenticated user profile" },
    { "method": "GET",  "path": "/user/repos",                       "description": "List repos" },
    { "method": "POST", "path": "/repos/{owner}/{repo}/issues",       "description": "Create issue",
      "body": { "title": "string (required)", "body": "string", "labels": ["string"] } },
    { "method": "GET",  "path": "/repos/{owner}/{repo}/pulls",        "description": "List pull requests" },
    { "method": "POST", "path": "/repos/{owner}/{repo}/pulls",        "description": "Create pull request",
      "body": { "title": "string", "head": "string", "base": "string" } }
  ],
  "bun_example":    "import { integration } from '@velane/integrations'\nconst gh = integration('github')\nconst user = await gh.get('/user')\nconst issue = await gh.post('/repos/owner/repo/issues', { title: 'Bug', body: 'Details' })",
  "python_example": "from velane.integrations import integration\ngh = integration('github')\nuser = gh.get('/user')\nissue = gh.post('/repos/owner/repo/issues', {'title': 'Bug'})"
}
```

For providers not in the bundled metadata, `common_endpoints` is `[]` and a `note` field says: `"Full endpoint list not bundled. Refer to docs_url."` The code example still works because the proxy pattern is the same for all providers.

**Updated `update_draft` description** (add to the existing description):

```
Built-in always available in snippet code (no install):
  Bun:    import { integration } from '@velane/integrations'
  Python: from velane.integrations import integration
Call list_connections to see which OAuth providers are connected.
Call get_integration_docs(provider) for endpoint reference and code examples.
```

---

### 7. Provider Metadata Store

A JSON file embedded in the MCP server binary covering the 60 most-used providers. This file is the source for `get_integration_docs` responses for popular providers.

**Location:** `services/mcp-server/internal/providers/providers_meta.json`  
**Embedded:** `//go:embed providers_meta.json` in `internal/providers/loader.go`

**Structure:**

```json
{
  "github": {
    "name": "GitHub",
    "base_url": "https://api.github.com",
    "docs_url": "https://docs.github.com/en/rest",
    "auth_mode": "OAUTH2",
    "common_endpoints": [...],
    "bun_example": "...",
    "python_example": "..."
  },
  "salesforce": { ... },
  "slack": { ... },
  "hubspot": { ... }
}
```

**Bundled providers (60):**

| Category | Providers |
|---|---|
| CRM | salesforce, hubspot, pipedrive, zoho-crm, close |
| Dev Tools | github, gitlab, bitbucket, linear, jira, asana, clickup |
| Communication | slack, discord, microsoft-teams, intercom, zendesk, freshdesk |
| Email / Calendar | gmail, google-calendar, outlook, sendgrid, mailchimp |
| Storage / Docs | google-drive, google-sheets, dropbox, box, notion, confluence, sharepoint |
| Payments | stripe, shopify, chargebee, recurly |
| Marketing | marketo, braze, segment, mixpanel |
| Data | airtable, snowflake, bigquery |
| HR / Ops | workday, bamboohr, gusto, trello, monday |
| Other | twilio, pagerduty, datadog, okta |

For all other providers from Nango's catalog: base URL and docs URL are returned from Nango's `/providers` endpoint metadata; `common_endpoints` is empty; the code example uses the generic pattern.

---

## Security Model

| Boundary | Mechanism |
|---|---|
| Nango not internet-accessible | No `ports:` in docker-compose; no ingress rule |
| Proxy trust-header exposure | `/v1/proxy/*` is unauthenticated and reachable from outside in shipped topologies; a caller can choose `X-Velane-Tenant`. Separate-listener hardening is deferred. |
| Tenant header behavior | Runners set `X-Velane-Tenant` from injected `VELANE_TENANT_ID`, but the trusting handler does not authenticate that caller-supplied header. |
| No OAuth tokens in snippet env | Tokens live only in Nango; runner only receives `VELANE_PROXY_URL` and `VELANE_TENANT_ID` |
| Variables (raw keys) still work | Injected as env vars same as before; orthogonal to integration proxy |
| Nango secret key | Only on control plane; never in executor env |

**Residual concern for self-hosted operators:** If two tenants' snippets run concurrently on the same Docker network, a snippet could forge a different `X-Velane-Tenant` header and proxy as another tenant. Mitigations for operators who need hard isolation: put executor containers on a separate Docker network with iptables rules limiting outbound to `control-plane:8080` only; or bind the proxy endpoint to a separate internal port not exposed to the executor network, and inject the tenant resolution differently. This is documented but not implemented by default.

---

## Phase 10 — Nango + Connections Layer

**Goal:** Tenants can connect OAuth providers through the admin UI. Connections are stored and tracked. No snippet code changes yet.

### Deliverables

- **Nango in docker-compose** — `nango` service, no host port binding, `NANGO_SECRET_KEY` and `NANGO_PUBLIC_KEY` added to env
- **Migration 012** — `connections` table, drop `libraries` + `library_versions`
- **Nango client** — `internal/nango/client.go`, thin Go HTTP client wrapping Nango REST API (connect session, get connection, delete connection, list providers, proxy)
- **ConnectionsHandler** — `internal/api/handlers/connections.go`:
  - `POST /v1/tenant/connections/session`
  - `GET /v1/tenant/connections`
  - `POST /v1/tenant/connections` (record after OAuth completes)
  - `DELETE /v1/tenant/connections/{provider}`
- **IntegrationsHandler** — `internal/api/handlers/integrations.go`:
  - `GET /v1/integrations` — proxies Nango `/providers`, cached 1h in memory
- **Router** — wire new handlers, remove library routes
- **Audit logging** — `connection_connect` and `connection_disconnect` audit events
- **Admin UI** — `IntegrationsPage.tsx`:
  - Provider catalog grid (from `GET /v1/integrations`)
  - Connected/disconnected status (from `GET /v1/connections`)
  - Search input + category filter chips (client-side)
  - Connect button → session token → `@nangohq/frontend` `openConnectUI()` → record connection
  - Disconnect button → `DELETE /v1/connections/{provider}`
  - Remove Libraries nav item + routes
  - Add Integrations nav item + route

### New env vars

```
NANGO_INTERNAL_URL=http://nango:3003
NANGO_SECRET_KEY=<operator sets this>
NANGO_PUBLIC_KEY=<operator sets this>
```

### New data model

```sql
-- 012_connections.sql
DROP TABLE IF EXISTS library_versions;
DROP TABLE IF EXISTS libraries;

CREATE TABLE connections (
  id                  UUID PRIMARY KEY DEFAULT gen_random_uuid(),
  tenant_id           UUID NOT NULL REFERENCES tenants(id) ON DELETE CASCADE,
  provider            TEXT NOT NULL,
  nango_connection_id TEXT NOT NULL,
  display_name        TEXT,
  created_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  updated_at          TIMESTAMPTZ NOT NULL DEFAULT NOW(),
  UNIQUE(tenant_id, provider)
);

CREATE INDEX idx_connections_tenant ON connections(tenant_id);
```

### New API surface

```
POST   /v1/tenant/connections/session           manage scope
GET    /v1/tenant/connections                   invoke scope
POST   /v1/tenant/connections                   manage scope
DELETE /v1/tenant/connections/{provider}        manage scope
GET    /v1/integrations                         no auth (public catalog)
```

### What's removed

- `LibrariesPage.tsx`, `LibraryEditorPage.tsx`
- `/dashboard/libraries` and `/dashboard/libraries/:id` routes
- Libraries nav item
- `handlers/libraries.go`
- Library routes from `router.go`
- Library store methods (or keep stub to avoid breaking existing data during migration)

---

## Phase 11 — Integration Proxy + Client Library

**Goal:** Snippets can call connected integrations using `integration('provider').get('/endpoint')`. Platform libraries are fully replaced.

### Deliverables

- **Proxy endpoint** — `GET/POST/PATCH/PUT/DELETE /v1/proxy/{provider}/*` on control plane:
  - Validates `X-Velane-Tenant` against `connections` table
  - Forwards to `h.nango.Proxy(...)` which calls Nango's proxy API
  - Streams response back (handles both JSON and binary responses)
- **Invocation env injection** — in `InvocationsHandler.Invoke`, before dispatching to executor:
  - `VELANE_PROXY_URL = http://control-plane:8080`
  - `VELANE_TENANT_ID = <tenant UUID>`
- **Built-in libraries** — `@velane/integrations` / `velane.integrations` and `@velane/store` / `velane.store`, with runtime-specific library files and documentation
- **Remove all Salesforce platform libraries** — delete `platform-libraries/bun/salesforce-*/` and `platform-libraries/python/salesforce-*/` (10 bun + 10 python directories)
- **`make copy-platform-libs`** — run after library changes to copy built-in libraries into the runtime
- **Proxy router registration** — `/v1/proxy` is unauthenticated, trusts `X-Velane-Tenant`, and is reachable from outside in shipped topologies; separate-listener hardening is deferred

### Invocation change (pseudocode)

```go
// InvocationsHandler.Invoke — before building RunRequest
tenant := middleware.TenantFromContext(r.Context())
extraEnv := map[string]string{
    "VELANE_PROXY_URL": cfg.InternalBaseURL,   // "http://control-plane:8080"
    "VELANE_TENANT_ID": tenant.ID.String(),
}
// merge with existing secrets from store
```

### New internal routes

```
GET|POST|PATCH|PUT|DELETE /v1/proxy/{provider}/*   (no public auth; trusts X-Velane-Tenant and is reachable from outside in shipped topologies)
```

### What's removed

- All `platform-libraries/bun/salesforce-*/` directories
- All `platform-libraries/python/salesforce-*/` directories

---

## Phase 12 — MCP Integration Context

**Goal:** Coding agents connected to Velane via MCP can discover connected providers, look up API endpoints, and write correct snippet code without human guidance.

### Deliverables

- **`list_connections` MCP tool** — calls `GET /v1/connections`, rich description teaches `@velane/integrations` usage
- **`get_integration_docs` MCP tool** — calls `GET /v1/integrations/{provider}/docs`, returns endpoints + code examples
- **`/v1/integrations/{provider}/docs` control plane endpoint** — merged response from bundled metadata + Nango fallback
- **Provider metadata bundle** — `services/mcp-server/internal/providers/providers_meta.json` (60 providers), embedded in binary via `go:embed`
- **`internal/providers/loader.go`** — loads and looks up provider metadata; falls back to Nango metadata for unknown providers
- **Updated `update_draft` description** — appended note about `@velane/integrations` being available and pointing to `list_connections` / `get_integration_docs`
- **Updated `create_snippet` description** — same note

### New control plane endpoint

```
GET /v1/integrations/{provider}/docs   invoke scope
```

**Response assembly logic:**

```go
func (h *IntegrationsHandler) GetProviderDocs(w http.ResponseWriter, r *http.Request) {
    provider := chi.URLParam(r, "provider")

    // 1. check bundled metadata first
    if meta, ok := h.providerMeta[provider]; ok {
        writeJSON(w, http.StatusOK, meta)
        return
    }

    // 2. fall back to Nango /providers for base metadata
    nangoMeta, err := h.nango.GetProvider(r.Context(), provider)
    if err != nil {
        writeError(w, http.StatusNotFound, "unknown provider: "+provider)
        return
    }

    // 3. synthesize minimal response with generic example
    writeJSON(w, http.StatusOK, map[string]any{
        "provider":         provider,
        "name":             nangoMeta.Name,
        "base_url":         nangoMeta.BaseURL,
        "docs_url":         nangoMeta.DocsURL,
        "auth_mode":        nangoMeta.AuthMode,
        "common_endpoints": []any{},
        "note":             "Full endpoint list not bundled. Refer to docs_url.",
        "bun_example":      fmt.Sprintf("import { integration } from '@velane/integrations'\nconst client = integration('%s')\n// See: %s", provider, nangoMeta.DocsURL),
        "python_example":   fmt.Sprintf("from velane.integrations import integration\nclient = integration(\"%s\")\n# See: %s", provider, nangoMeta.DocsURL),
    })
}
```

### Typical agent workflow after Phase 12

```
User prompt: "Write a snippet that creates a GitHub issue when a Salesforce case is closed"

Agent calls:
  1. list_connections
     → sees: github (connected), salesforce (connected)
  2. get_integration_docs("salesforce")
     → learns: POST /services/data/v60.0/sobjects/Case endpoint and fields
  3. get_integration_docs("github")
     → learns: POST /repos/{owner}/{repo}/issues with { title, body }
  4. create_snippet(name, slug, language="bun")
  5. update_draft(snippet_id, code)
     → writes:
       import { integration } from '@velane/integrations'
       const sf = integration('salesforce')
       const gh = integration('github')
       export default async function handler(input) {
         const closedCase = await sf.get(`/services/data/v60.0/sobjects/Case/${input.case_id}`)
         await gh.post('/repos/myorg/myrepo/issues', {
           title: `Case closed: ${closedCase.Subject}`,
           body:  closedCase.Description,
         })
         return { status: 'ok' }
       }
  6. invoke_snippet(..., input={ case_id: "500xx..." })
  7. get_logs(...)
  8. publish_snippet(..., env="staging")
```

---

## Summary

| Phase | Theme | Key deliverable |
|---|---|---|
| 10 | Nango + Connections | OAuth connect/disconnect UI for 800+ providers; connections stored in DB |
| 11 | Proxy + Client | `@velane/integrations` built-in; internal proxy; remove Salesforce libraries |
| 12 | MCP Context | `list_connections` + `get_integration_docs` tools; provider metadata bundle |

After Phase 12, a coding agent connected via MCP can:
- Discover which integrations are connected
- Get endpoint documentation for any provider
- Write snippet code that calls those integrations
- Test, publish, and monitor — all without leaving the IDE and without touching credentials.
