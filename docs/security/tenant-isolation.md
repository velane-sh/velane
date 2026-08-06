---
title: Tenant Isolation Model
description: How Velane enforces tenant boundaries across APIs and workflows.
sidebar_position: 2
---

# Tenant Isolation Model

Velane is multi-tenant by design. Isolation is a core platform behavior.

## Authenticated API isolation

- Public tenant-scoped APIs derive tenant context from an authenticated
  credential.
- Every public tenant-scoped query includes a tenant predicate, so tenant A
  cannot read or mutate tenant B resources through that authenticated surface.
- Snippet invocation and management on the public API are bound to tenant
  ownership.

## Where tenant context comes from

Authenticated API keys resolve their tenant directly from the key. Session
requests resolve the tenant from the active organization membership.

Trust-header routes are not equivalent. `/v1/proxy/*` and
`/v1/internal/kv/*` take `X-Velane-Tenant` from the caller without
authentication and are reachable from outside in shipped topologies. Their
tenant predicates prevent accidental cross-tenant queries, but do not
authenticate the tenant selected by the caller. See
[Trust-Header Routes](./trust-header-routes.md).

## Tenant-scoped data layer

Tenant predicates apply to tenant-scoped records, including `kv_entries`. On
the authenticated public KV API, the predicate uses the tenant derived from the
credential. On trust-header routes, the same predicate uses the unverified
caller-supplied header, so it is not a tenant-authentication boundary.

## Safe usage guidance

- always pass the intended tenant context in direct API usage
- avoid broad admin credentials in shared automation
- test access boundaries in staging with separate tenants

## Common mistakes to avoid

- reusing keys across unrelated tenants
- assuming slug-only checks are enough in custom integrations
- skipping scope and tenant validation in new endpoints

## Related docs

- [Trust-Header Routes](./trust-header-routes.md)
- [Security Non-Negotiables](./non-negotiables.md)
