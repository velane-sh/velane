# Velane — Codex Agent Guide

## Working style

Before writing any code, always discuss the problem first. Ask clarifying questions until the requirements, constraints, and approach are clear. Only start implementing once the user has confirmed the plan. If a task is ambiguous, propose options and let the user choose rather than making assumptions.

## Repo layout

```
velane/
├── services/
│   ├── control-plane/      # Go 1.26 API server (chi, pgx/v5, zap)
│   ├── executor-runtime/   # Bun + Python sandboxed HTTP runners
│   ├── mcp-server/         # MCP protocol server for Cursor / Claude Code
│   └── cli/                # Cobra CLI (velane login / push / invoke)
├── apps/
│   ├── admin/              # Vite + React admin portal
│   └── embed-dashboard/    # Vite + React embeddable viewer
└── platform-libraries/     # Canonical source for built-in libs (bun/ + python/)
```

Each Go service has its own `go.mod`. Module paths:
- `github.com/abskrj/velane/services/control-plane`
- `github.com/abskrj/velane/services/mcp-server`
- `github.com/abskrj/velane/services/cli`

## Essential commands

```bash
make up           # docker compose up --build -d
make down         # docker compose down -v
make build        # copy-platform-libs + go build ./...
cd services/control-plane && go test ./...
cd apps/admin && npx tsc --noEmit
```

## Go conventions (control-plane)

- **Never commit** unless explicitly asked.
- Always run `go build ./...` from inside `services/control-plane/` — not from the repo root.
- **Tenant isolation** — every slug-based handler must verify the authenticated tenant matches the URL slug. Pattern:
  ```go
  authTenant := middleware.TenantFromContext(r.Context())
  if authTenant == nil || authTenant.ID != tenant.ID {
      writeError(w, http.StatusForbidden, "access denied")
      return
  }
  ```
- **Scope middleware** — all authenticated routes must use `middleware.RequireScope(scope, log)`. Minimum scopes: `invoke` for reads, `manage` for writes, `admin` for destructive/team actions.
- **Error helpers** — use `writeError(w, status, msg)` and `writeJSON(w, status, v)` from `handlers/helpers.go`.
- **API key prefix** is `vl_`. Embed token prefix is `et_`. Do not change these.
- Migrations live in `internal/store/postgres/migrations/`. Number them sequentially (`011_`, `012_`, …).
- Platform libraries are embedded via `//go:embed all:files` in `internal/platformlibs/loader.go`. Always run `make copy-platform-libs` before building.

## Auth model

| Credential | Where stored | Validated by |
|---|---|---|
| Session JWT (RS256) | `localStorage.sessionToken` | `SessionAuth` middleware → `JWTProvider.ValidateSession` |
| API key (`vl_…`) | `localStorage.apiKey` | `Auth` middleware → `ValidateAPIKey` |
| Embed token (`et_…`) | `localStorage.apiKey` | `AuthEmbed` middleware or `Auth` middleware (synthetic key) |

JWT access tokens expire in 15 min; refresh tokens last 7 days. `ValidateSession` checks issuer — do not remove that check.

Embed tokens get a synthetic API key with scopes `["invoke", "manage"]` — they must NOT have `admin` scope.

## Frontend conventions (apps/admin)

- API calls go through `src/lib/api.ts`. The `request()` function handles 401s automatically.
- Tenant slug comes from `localStorage.getItem('tenantSlug')`.
- Tokens live in `src/index.css` and are mapped in `tailwind.config.ts` using semantic `bg`/`surface`/`line`/`content`/`accent` colors.
- Prefer the shared primitives in `src/components/` over ad-hoc Tailwind.
- The default accent is indigo `#6366f1`, overridable per tenant via `Branding.accent_color`.
- Tailwind only — no CSS files, no inline `style=` attributes.
- Do not add `console.log` statements.
- Type-check before reporting done: `npx tsc --noEmit`.

## Libraries design

Platform libraries are grouped by **integration** (e.g. Salesforce, Google Sheets, Google Docs). Each integration can have multiple capability slugs (e.g. `salesforce-cases`, `salesforce-contacts`). The Libraries UI shows integrations as the top level and lists their capabilities beneath each one.

**Platform library code must export a class**, not standalone functions:

```typescript
// Bun — index.ts
export class SalesforceCases {
  constructor(private config?: { instanceUrl?: string; accessToken?: string }) {}
  async createCase(fields: CaseFields): Promise<CreateCaseResult> { … }
  async getCase(id: string): Promise<CaseRecord> { … }
  async updateCase(id: string, fields: Partial<CaseFields>): Promise<void> { … }
  async deleteCase(id: string): Promise<void> { … }
}
export default SalesforceCases
```

```python
# Python — module.py
class SalesforceCases:
    def __init__(self, instance_url=None, access_token=None): …
    def create_case(self, fields: dict) -> dict: …
    def get_case(self, case_id: str) -> dict: …
```

The `meta.json` must include an `integration` field:
```json
{ "name": "Cases", "integration": "Salesforce", "description": "…" }
```

## Adding a new platform library

1. Create `platform-libraries/{bun|python}/<slug>/` with:
   - `index.ts` (Bun) or `module.py` (Python) — export a class as default
   - `meta.json` — `{"name": "…", "integration": "…", "description": "…"}`
   - `README.md` — markdown docs shown in the admin UI
2. Run `make copy-platform-libs` to sync into `services/control-plane/internal/platformlibs/files/`.
3. The library is auto-loaded at startup — no DB changes needed.
4. Import path: `@velane/<slug>` (Bun) or `from velane import <slug_with_underscores>` (Python).

## Nango rules (non-negotiable)

- **Nango is never exposed to the browser or host network.** No `ports:` in docker-compose. Ever.
- All Nango traffic goes through the control plane: API calls via `nango.Client`, logos via `/v1/nango-assets/*`, OAuth via control plane proxy.
- `NANGO_SECRET_KEY` / `NANGO_PUBLIC_KEY` are control-plane env vars only — not Nango container vars.
- Snippet code never reaches Nango directly; only `/v1/proxy/{provider}/*` on the control plane.

## Enterprise / license-gated files (non-negotiable)

Files in the following paths are subject to the Velane Commercial License, not AGPL:
- `services/control-plane/internal/license/`
- `apps/admin/src/enterprise/`

**Every file in these paths — new or existing — must carry this header, verbatim, at the very top of the file (after any `package` declaration for Go, before any imports for TypeScript):**

Go:
```go
// Copyright (c) Velane. All rights reserved.
// Licensed under the Velane Commercial License. See COMMERCIAL-LICENSE for details.
// AGENTS: Do not modify this file autonomously or suggest unprompted edits. Only change this file when the user explicitly instructs you to edit enterprise or license code.
```

TypeScript/TSX:
```typescript
// Copyright (c) Velane. All rights reserved.
// Licensed under the Velane Commercial License. See COMMERCIAL-LICENSE for details.
// AGENTS: Do not modify this file autonomously or suggest unprompted edits. Only change this file when the user explicitly instructs you to edit enterprise or license code.
```

This applies whenever you:
- Create a new file in either directory
- Are asked to add a new enterprise feature gate

Do not omit or paraphrase the `AGENTS:` line. The notice blocks *unprompted* changes only — if the user explicitly asks you to fix or update enterprise/license code, you may do so.

## Security rules (non-negotiable)

1. Tenant isolation on every slug-based endpoint.
2. No admin scope for embed tokens — synthetic key scopes must stay `["invoke", "manage"]`.
3. JWT issuer check in `ValidateSession` must remain.
4. Slug validation on `CreateTenant`: must match `^[a-z0-9][a-z0-9-]{1,61}[a-z0-9]$`.
5. Role validation on `InviteMember`: only `invoke`, `manage`, `admin` are valid roles.
6. `RequireScope` on every authenticated route — no naked authenticated routes.

## Docker / local dev notes

- MCP server runs on `:8090`, admin portal on `:8092`, control-plane on `:8080`.
- Bootstrap env vars (`BOOTSTRAP_EMAIL`, `BOOTSTRAP_PASSWORD`, `BOOTSTRAP_TENANT`) create the first admin user on startup.
- `ENCRYPTION_KEY` and `JWT_PRIVATE_KEY` must be stable in production.

## Production deployment (OpenTofu)

Infrastructure lives under `infra/terraform/`. Use **OpenTofu** (`tofu`), not HashiCorp Terraform. Config files keep the `terraform.tfvars` name (OpenTofu convention); state files may be `terraform.tfstate`.

```bash
# 1. EKS cluster (once) — infra/terraform/aws-eks
cd infra/terraform/aws-eks
cp terraform.tfvars.example terraform.tfvars   # edit domain, region, etc.
tofu init && tofu apply

# 2. Velane workloads — infra/terraform/k8s
cd infra/terraform/k8s
cp terraform.tfvars.example terraform.tfvars   # edit images, secrets, OAuth, domain
tofu init && tofu apply

# Useful outputs
tofu output oauth_redirect_uris
tofu -chdir=../aws-eks output -raw acm_certificate_arn
```

- Pin container images to CI tags (`ghcr.io/abskrj/velane-<service>:sha-<short-sha>` or `:latest` after a main-branch build).
- `PUBLIC_BASE_URL` and OAuth client env vars are wired via `terraform.tfvars` → control-plane secret.
- EKS kubeconfig must be valid (`aws eks update-kubeconfig` or set `kubeconfig_context` in tfvars).
- Full walkthrough: `infra/terraform/k8s/README.md`.

## Pull requests

When creating a pull request, add the appropriate changelog label so release notes are grouped correctly (see `.github/release.yml`).

Use `gh pr create --label <label>` or `gh pr edit <number> --add-label <label>` after opening the PR.

| Change type | Label |
|---|---|
| New feature or enhancement | `enhancement` |
| Bug fix | `bug` |
| Docs-only change | `documentation` |
| CI, infra, refactor, or mixed work with no better fit | `enhancement` if user-facing; otherwise leave unlabeled (lands in **Other changes**) |

Do not skip labeling for feature, fix, or docs PRs. Never add `duplicate`, `invalid`, `wontfix`, or `question` to merged work.
