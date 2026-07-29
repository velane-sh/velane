---
title: Your First Snippet
description: Create, publish, and invoke your first Velane snippet.
sidebar_position: 2
---

# Your First Snippet

This guide walks you from creating a snippet to invoking it.

## Create a snippet in the admin portal

1. Open `http://localhost:8092`
2. Sign in
3. Go to **Snippets**
4. Create a new snippet
5. Choose language: Bun or Python
6. Add your handler code
7. Publish to `dev`

## Invoke from the UI

Use the built-in test panel in the snippet editor to run your snippet immediately.

## Invoke from the API

Use an API key. The tenant is resolved from the key. It is not part of the invoke URL.

```bash
KEY=vl_xxxx
SNIPPET=<snippet-id-or-slug>

curl -s -X POST "http://localhost:8080/v1/invoke/$SNIPPET" \
  -H "Authorization: Bearer $KEY" \
  -H "Content-Type: application/json" \
  -d '{"name":"world"}'
```

## Environments and versions

- `dev`: fast iteration
- `staging`: pre-production checks
- `prod`: stable traffic

Each publish creates an immutable version, so rollback is straightforward.

## Runtime settings

Open a workflow’s **Settings** tab to configure timeout, memory, and CPU limits for the next version. Saving creates a new version (even if code did not change). Tenant caps are shown read-only at the top of the page.

## What to do next

- Read [Invocation Modes](../invoke/invocation-modes.md) for sync/async/stream behavior
- Read [Integrations Overview](../integrations/overview.md) to call third-party APIs from snippets
- Read [Agent Workflows](./agent-workflows.md) for LangGraph (Python) and Mastra (Bun)
