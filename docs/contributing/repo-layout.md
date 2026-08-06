---
title: Repository Layout
description: Navigate the Velane monorepo and understand service boundaries.
sidebar_position: 1
---

# Repository Layout

Velane is a monorepo with services, apps, and built-in libraries.

## Top-level structure

- `services/control-plane`: Go API server and scheduler
- `services/executor-runtime`: Bun and Python execution runtimes
- `services/mcp-server`: MCP integration server
- `services/cli`: command-line interface
- `apps/admin`: main dashboard
- `apps/embed-dashboard`: read-only embeddable dashboard
- `platform-libraries`: built-in libraries for snippet runtimes, including
  `@velane/integrations` / `velane.integrations` and `@velane/store` /
  `velane.store`

## How to navigate quickly

- product behavior: start at `services/control-plane`
- snippet runtime behavior: `services/executor-runtime`
- agent flows: `services/mcp-server`
- user UX: `apps/admin`

## Documentation ownership guideline

- service owners maintain their feature docs
- shared behavior (auth, invoke, integrations) lives in feature docs under `docs/`
