---
title: Request Lifecycle
description: End-to-end flow from invoke request to runtime execution.
sidebar_position: 2
---

# Request Lifecycle

This page explains what happens after you invoke a snippet.

## End-to-end lifecycle

```mermaid
flowchart LR
  A[Client or MCP invoke] --> B[Control-plane API]
  B --> C[Auth + tenant + scope checks]
  C --> D[Scheduler resolves snippet version and environment]
  D --> E[Dispatch to Bun or Python executor]
  E --> F[Snippet runs with injected context and libraries]
  F --> G[Result returned sync, async, or stream]
```

## Why this architecture matters

- policy and tenancy are centralized in control-plane
- execution stays isolated in runtime services
- one invoke API supports multiple execution modes

## Mode-specific behavior

- sync: waits for result
- async: queues work and returns early
- stream: returns incremental events

## Related docs

- [Invocation Modes](./invocation-modes.md)
- [KV Store](./kv-store.md)
- [Auth and Request Flow](../auth-tenancy/auth-and-request-flow.md)
- [Deployment Topology](../operations/deployment-topology.md)
