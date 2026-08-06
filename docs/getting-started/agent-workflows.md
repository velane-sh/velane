---
title: Agent Workflows
description: Run LangGraph and Mastra agents in Velane workflows with configurable runtime limits.
sidebar_position: 3
---

# Agent Workflows

Velane Bun and Python executors ship with **Mastra** (Bun) and **LangGraph** (Python) pre-installed. Use them directly in workflow code. No Velane wrapper packages are needed.

## Python: LangGraph

Store `OPENAI_API_KEY` (or other provider keys) as tenant secrets. They are injected as environment variables at invoke time.

```python
import os
from langchain_openai import ChatOpenAI
from langgraph.graph import StateGraph

def handler(input: dict) -> dict:
    model = ChatOpenAI(model="gpt-4o-mini", api_key=os.environ["OPENAI_API_KEY"])
    # Build and run your graph; return structured output.
    return {"message": "agent result"}
```

For streaming, use an async generator handler and `yield` chunks (see [Invocation Modes](../invoke/invocation-modes.md)).

## Bun: Mastra

```typescript
import { Agent } from '@mastra/core/agent'

export default async function handler(input: Record<string, unknown>) {
  const agent = new Agent({
    name: 'workflow-agent',
    instructions: 'You are a helpful assistant.',
    model: 'openai/gpt-4o-mini',
  })
  const result = await agent.generate('Summarize: ' + String(input.topic ?? ''))
  return { text: result.text }
}
```

Velane's built-in libraries include `@velane/integrations` for provider calls
and `@velane/store` for tenant-scoped state that persists across invocations.
Wire integration tools manually with `@velane/integrations` inside Mastra tool
`execute` functions (see [Integrations Overview](../integrations/overview.md)).

## Runtime limits

Each workflow **version** stores:

| Field | Default | Enforcement |
|---|---|---|
| `timeout_ms` | 60000 (1 min) | Hard kill → `timeout` |
| `max_memory_mb` | 200 | Hard kill → `oom_killed` |
| `max_cpu_percent` | 10 (0.1 vCPU) | cgroup CPU cap (Linux executors) |

Set limits in the admin portal **Settings** tab for a workflow, or via `POST /v1/snippets/{id}/versions` when creating a version.

Tenant caps (`GET /v1/tenant/runtime-limits`) are read-only in the UI. Platform operators adjust per-tenant caps in the database (`tenants.runtime_limits`).

Agent workflows that import LangGraph or Mastra typically need higher `max_memory_mb` (for example 512–1024). Simple workflows that do not import agent libraries keep a low memory footprint even though the frameworks are present in the executor image.

## Related docs

- [Invocation Modes](../invoke/invocation-modes.md)
- [Environment Variables](../operations/environment-variables.md)
- [Integrations Overview](../integrations/overview.md)
- [KV Store](../invoke/kv-store.md)
