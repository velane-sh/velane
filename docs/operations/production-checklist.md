---
title: Production Checklist
description: Go-live checklist for secure and reliable Velane production deployments.
sidebar_position: 3
---

# Production Checklist

Use this checklist before go-live.

## Security and identity

- Set stable `ENCRYPTION_KEY`
- Set stable `JWT_PRIVATE_KEY`
- Set a stable `SSO_SAML_PRIVATE_KEY` / `SSO_SAML_CERTIFICATE` pair and document its rotation owner
- Remove bootstrap credentials after first setup
- Use scoped API keys (least privilege)

## Network boundaries

- Keep Nango internal-only (no public host port)
- Expose only intended public interfaces
- Ensure internal proxy paths are not internet-routable

## Data and durability

- Use persistent volumes for Postgres and object storage
- Configure backups and restore drill cadence
- Verify migration behavior in staging before release

## Runtime and scaling

- Right-size executor services for expected load
- Configure `WORKER_COUNT` based on async volume
- Validate timeout and memory settings for real workloads

## Observability

- Ensure logs, metrics, and alerts are enabled
- Define alert thresholds for error rate, latency, and queue delays
- Add a synthetic health check for invoke path

## Release safety

- Promote through `dev` then `staging` before `prod`
- Keep rollback playbook ready (version rollback + traffic control)
- Verify MCP and CLI flows against production-like staging

## Related docs

- [Environment Variables](./environment-variables.md)
- [Deployment Topology](./deployment-topology.md)
- [Known Limitations](./known-limitations.md)
