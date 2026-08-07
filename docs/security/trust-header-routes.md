---
title: Trust-Header Routes
description: Routes that trust a caller-supplied tenant header, and their current exposure.
sidebar_position: 3
---

# Trust-Header Routes

`/v1/proxy/*` and `/v1/internal/kv/*` do not use the public authentication
stack. They trust the `X-Velane-Tenant` header, so the caller names the tenant
and is believed. This is not authentication.

The network precondition for this design is not enforced in any shipped
topology. The exposure is accepted for now; hardening both route families onto
a separate, never-published listener is tracked separately.

## Measured reachability

These probes reached the trusting handler rather than being rejected before it:

| Probe | Result |
|---|---|
| `curl localhost:8080/v1/proxy/github/user -H 'X-Velane-Tenant: bogus'` | `400 X-Velane-Tenant header required` — the **handler itself** answered, so the request reached the trusting code from the host |
| `curl localhost:8092/api/v1/proxy/github/user -H 'X-Velane-Tenant: bogus'` | same 400 — reachable through the admin nginx proxy too |

Every path that reaches the trusting handlers today:

| Path | Evidence | Edge to filter? |
|---|---|---|
| Compose published port | `docker-compose.yml:117-122`, `docker-compose.dev.yml:117-122` publish `8080:8080`; Docker binds all host interfaces by default | **none** |
| Admin portal proxy | `apps/admin/nginx.conf:8-14` proxies every `/api/` path | yes |
| Embed dashboard proxy | `apps/embed-dashboard/nginx.conf:8-15` — identical broad rule | yes |
| Azure VM production | `infra/terraform/azure-vm/Caddyfile:5-7` — `{$API_HOST} { reverse_proxy control-plane:8080 }`, ports 80/443 public | yes |
| k8s with ingress | `infra/terraform/k8s/main.tf:999-1007` routes the api subdomain at `path = "/"`, `path_type = "Prefix"` | yes, but the module supports nginx, ALB, GCE and Azure App Gateway, each needing a different mechanism |
| k8s without ingress | `control_plane_service_type` defaults to `LoadBalancer` (`variables.tf:92-96`, `main.tf:549-568`); the no-custom-domain setup in `README.md:283-295` tells operators to use it | **none** |

Two paths have no edge at all. Anyone who can reach the control-plane port can
set an arbitrary `X-Velane-Tenant` and read or write any tenant's KV.
`/v1/proxy/*` has carried this exposure since it shipped; the KV surface widens
it and, unlike the proxy, returns stored data directly rather than brokering a
third-party call.

## Affected routes

The public and trust-header route families have different security properties.

- **Public `/v1/kv/*`:** the tenant is derived from an authenticated
  credential, and every query includes a tenant predicate. This surface is
  tenant-isolated.
- **`/v1/proxy/*` and `/v1/internal/kv/*`:** the tenant comes from a
  caller-supplied header. Tenant predicates prevent accidental cross-tenant
  queries, but do not authenticate the tenant selected by that caller.

For the trust-header family, the affected KV routes are:

| Method | Path | Tenant source | Purpose |
|---|---|---|---|
| GET | `/v1/internal/kv/entries` | `X-Velane-Tenant` | List metadata |
| GET | `/v1/internal/kv/entry` | `X-Velane-Tenant` | Read one entry |
| PUT | `/v1/internal/kv/entry` | `X-Velane-Tenant` | Upsert one entry |
| DELETE | `/v1/internal/kv/entry` | `X-Velane-Tenant` | Delete one entry |

The separate-listener hardening remains a follow-up. Until it lands, do not
treat the trust-header routes as an authenticated tenant boundary.

## Related docs

- [Tenant Isolation Model](./tenant-isolation.md)
- [Security Non-Negotiables](./non-negotiables.md)
- [Known Limitations](../operations/known-limitations.md)
