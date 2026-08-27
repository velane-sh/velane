---
name: testing-admin-ui
description: How to bring up the Velane local stack and test the admin UI (auth, tenant members, integrations, user groups/RBAC) end-to-end in a browser.
---

# Testing the Velane admin UI locally

## Bring up the stack
- Build from the current branch (do NOT use `make up`, which pulls published images and will not contain
  branch changes): `docker compose -f docker-compose.dev.yml up --build -d` (equivalently `make dev`).
- Run `make copy-platform-libs` first; control-plane builds fail without it.
- A container named `velane-test-pg` (Postgres on :5432 for Go tests) may already be running and will
  conflict with the compose Postgres port. Stop it before starting the stack: `docker stop velane-test-pg`.
- Ports: admin UI http://localhost:8092, control-plane http://localhost:8080, MCP http://localhost:8090.
- `curl http://localhost:8080/health` returns 404 — that is normal, it is not a health endpoint. Check
  readiness with `docker compose -f docker-compose.dev.yml logs control-plane` and look for the
  "listening on :8080" line plus completed migrations.

## Logging in
- The first admin user is seeded from compose env `BOOTSTRAP_EMAIL` / `BOOTSTRAP_PASSWORD` /
  `BOOTSTRAP_TENANT` (defaults in docker-compose.dev.yml, e.g. admin@example.com / changeme123 / myorg).

## Creating a second, non-admin user (needed for any permission/visibility test)
- Settings → Team → Invite Team Member (pick a non-admin role such as `manage`).
- Invites are not emailed locally: grab the invite link/token from the control-plane logs or the
  `invites` table, open it in an **incognito window**, and register a password there.
- Keep admin in the normal window and the second user in incognito; switch between them with the two
  Chrome taskbar icons. This gives two concurrent sessions without logging out.

## Integration profiles without OAuth
- Integrations → Add Integration → pick a provider with User OAuth (e.g. GitHub) and save with dummy
  client id/secret. The profile appears under "All Profiles" with status "Ready" and is usable for
  visibility/RBAC tests. Do NOT attempt the "Connect" button: Nango is deliberately not reachable from
  the browser, so real third-party OAuth flows cannot be completed locally — report them as untestable.

## Group RBAC (user groups) testing notes
- Page: Settings → User Groups (`/dashboard/settings/groups`).
- Visibility rule: a credential profile with zero group grants stays visible tenant-wide; once granted to
  at least one group only admin/owner and members of a granted group see it in Integrations → All Profiles.
- Changes take effect on the other user's next page load — reload the incognito Integrations page (F5)
  after each grant/revoke/member change instead of expecting live updates.
- Admin/owner always bypass the filter, so always verify the negative case with the non-admin session.
- The runtime proxy caller-token (`VELANE_CALLER_TOKEN` / `X-Velane-Caller`) enforcement path needs a live
  third-party connection, so it cannot be exercised locally; report it as untested rather than faking it.

## Devin Secrets Needed
- None. Bootstrap credentials come from the local compose file.
