---
title: Integrations Overview
description: Connect providers and call external APIs through Velane integrations.
sidebar_position: 1
---

# Integrations Overview

Velane integrations are designed so snippet code can call provider APIs without handling OAuth tokens directly.

## How it works

1. You configure and connect a provider in the admin portal
2. Velane stores connection metadata for your tenant
3. Snippets call the built-in integrations client
4. Velane proxies requests to the provider through its internal integration path

This keeps credentials out of snippet code and centralizes OAuth lifecycle handling.

## Built-in libraries

Velane provides `@velane/integrations` / `velane.integrations` for connected
provider APIs and `@velane/store` / `velane.store` for tenant-scoped JSON state
that persists across invocations. See [KV Store](../invoke/kv-store.md) for
store usage and its plaintext-value constraints.

## Connect a provider

In the admin portal:

1. Go to **Integrations**
2. Choose a provider (for example, GitHub or Salesforce)
3. Configure OAuth app details if required
4. Complete the Connect flow

After connection, snippets in your tenant can call that provider.

## Use integrations in Bun snippets

```typescript
import { integration } from '@velane/integrations'

export default async function handler() {
  const github = integration('github')
  return await github.get('/user')
}
```

## Use integrations in Python snippets

```python
from velane.integrations import integration

def handler(_input):
    github = integration("github")
    return github.get("/user")
```

## Why this model is useful

- no token refresh logic in snippets
- one usage pattern across many providers
- easier agent-generated code with fewer auth mistakes

## Next step

If you are using Cursor or Claude Code, continue to [MCP Overview](../mcp/overview.md) for the MCP-first workflow.

## Related docs

- [Embed Mode vs Embed Dashboard](./embed-mode-vs-dashboard.md)
- [Credentials and Scopes](../auth-tenancy/credentials-and-scopes.md)
- [KV Store](../invoke/kv-store.md)
