---
title: Security Non-Negotiables
description: Core security requirements and invariants in Velane.
sidebar_position: 1
---

# Security Non-Negotiables

This page summarizes the core security guarantees Velane is designed to keep.

## Authenticated tenant isolation

Public tenant-scoped routes, including `/v1/kv/*`, derive the tenant from an
authenticated credential and apply a tenant predicate to every query.

`/v1/proxy/*` and `/v1/internal/kv/*` are different: they trust a
caller-supplied tenant header and are reachable from outside in shipped
topologies. Their tenant predicates prevent accidental cross-tenant queries,
but do not authenticate the tenant a caller selected. See
[Trust-Header Routes](./trust-header-routes.md).

## No admin access for embed tokens

Embed tokens (`et_...`) are for embed use cases and must not gain admin privilege.

## Session validation must stay strict

Session token verification checks issuer and signature. Do not weaken these checks.

## Scope checks are mandatory

Authenticated routes should enforce minimum required scopes:

- `invoke` for read/invoke actions
- `manage` for write operations
- `admin` for sensitive administration

## Integration boundary

Integration credentials stay server-side. Snippet code should call Velane integration pathways, not raw credential-bearing endpoints.

## Key management

- keep signing/encryption keys stable in production
- rotate operational keys safely
- avoid exposing secrets in logs or client apps
