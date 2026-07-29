---
title: Environment Variables
description: Core configuration values for running Velane in local and production environments.
sidebar_position: 1
---

# Environment Variables

This page highlights the most important configuration values for running Velane.

## Core service variables

- `DATABASE_URL`: Postgres DSN for control-plane state
- `REDIS_URL`: Redis address for async queueing
- `PORT`: control-plane listen port
- `WORKER_COUNT`: async worker concurrency

## Executor runtime variables

- `BUN_EXECUTOR_URL`: Bun executor endpoint
- `PYTHON_EXECUTOR_URL`: Python executor endpoint
- `EXECUTOR_TYPE`: executor mode (`process` or `firecracker`)

## Security-critical variables

- `ENCRYPTION_KEY`: key used for secret encryption
- `JWT_PRIVATE_KEY`: signing key for session JWTs

In production, these must be stable and persistent across restarts.

## Bootstrap variables (first-run convenience)

- `BOOTSTRAP_EMAIL`
- `BOOTSTRAP_PASSWORD`
- `BOOTSTRAP_TENANT`

Use these to create the first admin account. Remove or lock down after bootstrap.

## Integrations variables

- `NANGO_INTERNAL_URL`
- `NANGO_SECRET_KEY`
- `NANGO_PUBLIC_KEY`
- `NANGO_WEBHOOK_SECRET`

Keep Nango secrets server-side only.

## Workflow runtime limits

Per-version limits (`timeout_ms`, `max_memory_mb`, `max_cpu_percent`) are set when creating a workflow version via API or the workflow **Settings** tab.

Per-tenant caps are stored in `tenants.runtime_limits` (JSON). Tenants read caps via `GET /v1/tenant/runtime-limits`. Adjust caps with SQL or internal ops tooling. There is no tenant self-service PUT in v1.

Default tenant caps: 15 minute timeout, 2048 MB memory, 100% CPU.

## Practical recommendation

Start with `.env` for local development, then move to your secret manager in production.
