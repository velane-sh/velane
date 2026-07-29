---
title: MCP Overview
description: Use Velane's first-class MCP server for agent-driven workflows.
sidebar_position: 1
---

# MCP in Velane

Velane includes a first-class MCP server so coding agents can create, update, publish, and invoke workflows directly from your IDE.

## Why MCP is first-class in Velane

MCP is not an addon workflow in Velane. It is a primary interface for agent-driven development:

- create workflow drafts
- update and publish versions
- invoke workflows
- read logs and metrics
- discover connected integrations

## MCP endpoint

Local default:

- `http://localhost:8090/mcp`

## Cursor setup example

Add this to your MCP config:

```json
{
  "mcpServers": {
    "velane": {
      "name": "velane",
      "type": "http",
      "url": "http://localhost:8090/mcp",
      "headers": {
        "Authorization": "Bearer vl_xxxx"
      }
    }
  }
}
```

Use an API key with the minimum scope needed for your workflow.

## Tools, resources, and prompts

Velane's MCP server exposes three kinds of capability:

- **Tools** perform actions: create workflows, update drafts, publish versions, invoke workflows, list connections, read invocation records, and manage secrets.
- **Resources** expose bounded read-only context. They help agents understand the current workspace without dumping every object at startup.
- **Prompts** expose reusable Velane workflows so agents use tools in the right order.

### Resources

`resources/list` returns only resource descriptors. It does not return every workflow or every invocation. Clients read resource content explicitly with `resources/read`.

Current resources:

| URI | Purpose |
|---|---|
| `velane://runtime/contract` | Workflow handler shapes, integration helper usage, invocation/logging rules, and recommended MCP workflow. |
| `velane://runtime/agent-frameworks` | Mastra (Bun) and LangGraph (Python). **Read before writing AI agent workflows**. |
| `velane://workflows` | Compact first page of workflows. The response is bounded and omits code. Use `get_workflow` for code, versions, and active environments. |
| `velane://connections` | Compact first page of connected integrations. Use `list_connections` for filtering and pagination. |

This means a tenant with 500 workflows does **not** send all 500 workflows during MCP startup. Startup only advertises `velane://workflows`; content is read only when the agent asks for it, and the workflow catalog is compact and bounded.

### Prompts

Current prompts:

| Prompt | Purpose |
|---|---|
| `create_agent_workflow` | Guides an agent to use Mastra/LangGraph (not custom loops) for LLM/agent workflows. |
| `create_integration_workflow` | Guides an agent through connection discovery, provider docs lookup, workflow creation/update, dev invocation, and validation. |
| `debug_failed_invocation` | Guides an agent through `get_invocation` / `get_logs`, code inspection, draft patching, and dev reruns. |
| `publish_after_validation` | Guides an agent to validate a specific version before publishing it to a target environment. |

## Typical agent workflow

1. `list_connections`
2. `get_integration_docs` for a provider
3. `create_workflow`
4. `update_draft`
5. `invoke_workflow`
6. `publish_workflow`
7. `get_logs` / `get_metrics`

## Core tools you will use often

- workflows: create, update draft, publish, list, get
- invoke: sync/async/stream invocation
- integrations: list tenant connections, provider docs, and **agent framework docs** (`get_agent_framework_docs`)
- operations: logs, metrics, secrets

## Practical guidance

- Keep API keys scoped to the least privilege needed
- Validate in `dev` first, then promote to `staging` and `prod`
- Use provider docs tooling before generating integration-heavy snippet code

## Related docs

- [Integrations Overview](../integrations/overview.md)
- [Invocation Modes](../invoke/invocation-modes.md)
