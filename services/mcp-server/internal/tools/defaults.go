package tools

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strings"

	"github.com/abskrj/velane/services/mcp-server/internal/agentdocs"
	"github.com/abskrj/velane/services/mcp-server/internal/controlplane"
)

func (r *Registry) addDefaults() {
	r.add(Tool{
		Name:        "kv_get",
		Description: "Get a JSON value from the tenant-wide KV store. The default namespace is shared by every workflow in the tenant. Returns null when the key is missing; a stored JSON null is indistinguishable from a missing key, so use kv_list with a prefix when that distinction matters.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"key"},
			"properties": map[string]any{
				"key":       map[string]any{"type": "string", "description": "Key to retrieve."},
				"namespace": map[string]any{"type": "string", "description": "Optional namespace; omitted means the tenant-wide default namespace."},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			key, err := toString(args, "key", true)
			if err != nil {
				return nil, err
			}
			namespace, err := toString(args, "namespace", false)
			if err != nil {
				return nil, err
			}

			var out struct {
				Value json.RawMessage `json:"value"`
			}
			path := "/v1/kv/entry" + controlplane.Query(map[string]string{
				"namespace": namespace,
				"key":       key,
			})
			if err := r.client.Get(ctx, authHeader, path, &out); err != nil {
				var statusErr *controlplane.StatusError
				if errors.As(err, &statusErr) && statusErr.StatusCode == 404 {
					return nil, nil
				}
				return nil, err
			}
			return out.Value, nil
		},
	})

	r.add(Tool{
		Name:        "kv_set",
		Description: "Set a JSON value in the tenant-wide KV store. The default namespace is shared by every workflow in the tenant. ttl_seconds is optional, measured in seconds, and omitting it stores the value without an expiry.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"key", "value"},
			"properties": map[string]any{
				"key":         map[string]any{"type": "string", "description": "Key to set."},
				"value":       map[string]any{"description": "Any JSON value, including an object, array, string, number, boolean, or null."},
				"namespace":   map[string]any{"type": "string", "description": "Optional namespace; omitted means the tenant-wide default namespace."},
				"ttl_seconds": map[string]any{"type": "integer", "description": "Optional time to live in seconds."},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			key, err := toString(args, "key", true)
			if err != nil {
				return nil, err
			}
			namespace, err := toString(args, "namespace", false)
			if err != nil {
				return nil, err
			}
			value, err := toRawJSON(args, "value")
			if err != nil {
				return nil, err
			}

			body := map[string]any{"value": value}
			if ttlValue, ok := args["ttl_seconds"]; ok && ttlValue != nil {
				ttlSeconds, err := toInt(args, "ttl_seconds", false)
				if err != nil {
					return nil, err
				}
				body["ttl_seconds"] = ttlSeconds
			}

			var out map[string]any
			path := "/v1/kv/entry" + controlplane.Query(map[string]string{
				"namespace": namespace,
				"key":       key,
			})
			if err := r.client.Put(ctx, authHeader, path, body, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "kv_delete",
		Description: "Delete a value from the tenant-wide KV store. The default namespace is shared by every workflow in the tenant. This cleanup operation is idempotent and returns deleted: false when the key is already missing.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"key"},
			"properties": map[string]any{
				"key":       map[string]any{"type": "string", "description": "Key to delete."},
				"namespace": map[string]any{"type": "string", "description": "Optional namespace; omitted means the tenant-wide default namespace."},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			key, err := toString(args, "key", true)
			if err != nil {
				return nil, err
			}
			namespace, err := toString(args, "namespace", false)
			if err != nil {
				return nil, err
			}
			path := "/v1/kv/entry" + controlplane.Query(map[string]string{
				"namespace": namespace,
				"key":       key,
			})
			if err := r.client.Delete(ctx, authHeader, path, nil); err != nil {
				var statusErr *controlplane.StatusError
				if errors.As(err, &statusErr) && statusErr.StatusCode == 404 {
					return map[string]bool{"deleted": false}, nil
				}
				return nil, err
			}
			return map[string]bool{"deleted": true}, nil
		},
	})

	r.add(Tool{
		Name:        "kv_list",
		Description: "List KV metadata without values. Omit namespace to list every namespace, or provide one to filter it. The default namespace is shared by every workflow in the tenant.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"namespace": map[string]any{"type": "string", "description": "Optional namespace. Omit to list every namespace; provide default to list only the tenant-wide default namespace."},
				"prefix":    map[string]any{"type": "string", "description": "Optional literal key prefix."},
				"limit":     map[string]any{"type": "integer", "description": "Maximum entries to return."},
				"offset":    map[string]any{"type": "integer", "description": "Number of entries to skip."},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			namespace, err := toString(args, "namespace", false)
			if err != nil {
				return nil, err
			}
			prefix, err := toString(args, "prefix", false)
			if err != nil {
				return nil, err
			}
			limit, err := toInt(args, "limit", false)
			if err != nil {
				return nil, err
			}
			offset, err := toInt(args, "offset", false)
			if err != nil {
				return nil, err
			}

			query := map[string]string{"prefix": prefix}
			if _, ok := args["namespace"]; ok {
				if namespace == "" {
					namespace = "default"
				}
				query["namespace"] = namespace
			}
			if limit > 0 {
				query["limit"] = fmt.Sprintf("%d", limit)
			}
			if offset > 0 {
				query["offset"] = fmt.Sprintf("%d", offset)
			}

			var out map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/kv/entries"+controlplane.Query(query), &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name: "list_connections",
		Description: `List OAuth integrations connected for this tenant.

IMPORTANT — how to use integrations in workflow code:

Bun/TypeScript:
  import { integration } from '@velane/integrations'
  const client = integration('github')                           // provider slug
  const user   = await client.get('/user')                      // GET
  const issue  = await client.post('/repos/org/repo/issues',    // POST
                   { title: 'Bug', body: 'Details' })
  await client.patch('/repos/org/repo/issues/1', { state: 'closed' })
  await client.delete('/repos/org/repo/labels/old')

Python:
  from velane.integrations import integration
  client = integration("salesforce")
  cases  = client.get("/services/data/v60.0/sobjects/Case/describe")
  result = client.post("/services/data/v60.0/sobjects/Case",
             {"Subject": "Login issue", "Status": "New"})

Methods: .get(path)  .post(path, body)  .patch(path, body)  .put(path, body)  .delete(path)
All methods return parsed JSON. Paths are the provider's native API paths.
@velane/integrations is always available — no install, no credentials needed in code.
Call get_integration_docs(provider) to look up endpoints for any provider.`,
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"provider": map[string]any{"type": "string", "description": "Filter by provider/alias substring, e.g. 'github'."},
				"limit":    map[string]any{"type": "integer", "description": "Max results to return (default 50)."},
				"offset":   map[string]any{"type": "integer", "description": "Number of results to skip for pagination."},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			provider, _ := toString(args, "provider", false)
			limit, _ := toInt(args, "limit", false)
			offset, _ := toInt(args, "offset", false)
			if limit <= 0 {
				limit = 50
			}

			query := map[string]string{
				"q":     provider,
				"limit": fmt.Sprintf("%d", limit),
			}
			if offset > 0 {
				query["offset"] = fmt.Sprintf("%d", offset)
			}

			var out []map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/connections"+controlplane.Query(query), &out); err != nil {
				return nil, err
			}
			if out == nil {
				return []map[string]any{}, nil
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "get_integration_docs",
		Description: "Get API endpoints, base URL, and a working code example for a specific integration provider. Call this before writing workflow code that uses an integration.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"provider"},
			"properties": map[string]any{
				"provider": map[string]any{
					"type":        "string",
					"description": "Provider slug, e.g. github, salesforce, slack, hubspot, notion, linear, stripe, zendesk, airtable",
				},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			provider, err := toString(args, "provider", true)
			if err != nil {
				return nil, err
			}
			provider = strings.TrimSpace(strings.ToLower(provider))
			var out map[string]any
			path := "/v1/integrations/" + url.PathEscape(provider) + "/docs"
			if err := r.client.Get(ctx, authHeader, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name: "get_agent_framework_docs",
		Description: `Returns Mastra (Bun) and LangGraph (Python) patterns for AI agent workflows.

Call this BEFORE create_workflow / update_draft when building chatbots, tool-using agents, or multi-step LLM flows.
Do not hand-roll custom agent loops — use the preinstalled frameworks.`,
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handle: func(_ context.Context, _ string, _ map[string]any) (any, error) {
			return map[string]any{
				"bun_framework":    "Mastra (@mastra/core/agent)",
				"python_framework": "LangGraph (langgraph)",
				"docs_markdown":    agentdocs.Doc,
			}, nil
		},
	})

	r.add(Tool{
		Name:        "list_workflows",
		Description: "List workflows available to the authenticated tenant.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handle: func(ctx context.Context, authHeader string, _ map[string]any) (any, error) {
			var out []map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/snippets", &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "get_workflow",
		Description: "Get a workflow by ID, including its version history (with code) and the active version per environment. Use this to read the current code before editing an existing workflow.",
		InputSchema: map[string]any{
			"type": "object",
			"required": []string{
				"workflow_id",
			},
			"properties": map[string]any{
				"workflow_id": map[string]any{"type": "string", "description": "Workflow ID."},
				"snippet_id":  map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			snippetID, err := toWorkflowID(args, true)
			if err != nil {
				return nil, err
			}
			esc := url.PathEscape(snippetID)

			var snippet map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/snippets/"+esc, &snippet); err != nil {
				return nil, err
			}

			// Best-effort enrichment: versions (with code) and per-env active
			// version. Failures here should not fail the whole call.
			var versions []map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/snippets/"+esc+"/versions", &versions); err != nil {
				versions = nil
			}
			var environments []map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/snippets/"+esc+"/environments", &environments); err != nil {
				environments = nil
			}
			var triggers []map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/snippets/"+esc+"/triggers", &triggers); err != nil {
				triggers = nil
			}

			// Surface the latest version's code directly for convenience.
			var activeCode string
			if n := len(versions); n > 0 {
				if code, ok := versions[n-1]["code"].(string); ok {
					activeCode = code
				}
			}

			return map[string]any{
				"workflow":     snippet,
				"versions":     versions,
				"environments": environments,
				"triggers":     triggers,
				"latest_code":  activeCode,
			}, nil
		},
	})

	r.add(Tool{
		Name:        "create_workflow",
		Description: "Create a workflow. The workflow ID (UUID) is assigned automatically. For AI agent workflows, call get_agent_framework_docs first and use Mastra (bun) or LangGraph (python).",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"name", "language"},
			"properties": map[string]any{
				"name":     map[string]any{"type": "string"},
				"language": map[string]any{"type": "string", "enum": []string{"bun", "python"}},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			name, err := toString(args, "name", true)
			if err != nil {
				return nil, err
			}
			if slug, _ := toString(args, "slug", false); strings.TrimSpace(slug) != "" {
				return nil, fmt.Errorf("slug is assigned automatically; do not send slug")
			}
			lang, err := toString(args, "language", true)
			if err != nil {
				return nil, err
			}
			body := map[string]any{"name": name, "language": lang}
			var out map[string]any
			if err := r.client.Post(ctx, authHeader, "/v1/snippets", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name: "update_draft",
		Description: `Create a new workflow version from source code.

Built-in imports (no install needed):
  Integrations — Bun: import { integration } from '@velane/integrations'
                 Python: from velane.integrations import integration
  AI agents    — Bun: Mastra — import { Agent } from '@mastra/core/agent'
                 Python: LangGraph — from langgraph.graph import StateGraph

For chat/tool-using/LLM agent workflows you MUST use Mastra or LangGraph.
Call get_agent_framework_docs before writing agent code. Do not hand-roll custom agent loops.

Call list_connections / get_integration_docs for OAuth provider APIs.
For agent workflows set higher limits via timeout_ms, max_memory_mb (e.g. 512), max_cpu_percent.`,
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"workflow_id", "code"},
			"properties": map[string]any{
				"workflow_id":     map[string]any{"type": "string", "description": "Workflow ID."},
				"snippet_id":      map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"code":            map[string]any{"type": "string"},
				"input_schema":    map[string]any{"type": "string"},
				"output_schema":   map[string]any{"type": "string"},
				"timeout_ms":      map[string]any{"type": "integer"},
				"max_memory_mb":   map[string]any{"type": "integer"},
				"max_cpu_percent": map[string]any{"type": "integer"},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			snippetID, err := toWorkflowID(args, true)
			if err != nil {
				return nil, err
			}
			code, err := toString(args, "code", true)
			if err != nil {
				return nil, err
			}
			inputSchema, _ := toString(args, "input_schema", false)
			outputSchema, _ := toString(args, "output_schema", false)
			timeoutMs, _ := toInt(args, "timeout_ms", false)
			maxMemoryMB, _ := toInt(args, "max_memory_mb", false)
			maxCPUPercent, _ := toInt(args, "max_cpu_percent", false)

			body := map[string]any{
				"code": code,
			}
			if inputSchema != "" {
				body["input_schema"] = inputSchema
			}
			if outputSchema != "" {
				body["output_schema"] = outputSchema
			}
			if timeoutMs > 0 {
				body["timeout_ms"] = timeoutMs
			}
			if maxMemoryMB > 0 {
				body["max_memory_mb"] = maxMemoryMB
			}
			if maxCPUPercent > 0 {
				body["max_cpu_percent"] = maxCPUPercent
			}

			var out map[string]any
			path := "/v1/snippets/" + url.PathEscape(snippetID) + "/versions"
			if err := r.client.Post(ctx, authHeader, path, body, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "publish_workflow",
		Description: "Publish a workflow version to an environment.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"workflow_id", "version_number", "env"},
			"properties": map[string]any{
				"workflow_id":    map[string]any{"type": "string", "description": "Workflow ID."},
				"snippet_id":     map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"version_number": map[string]any{"type": "integer"},
				"env":            map[string]any{"type": "string", "enum": []string{"dev", "staging", "prod"}},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			snippetID, err := toWorkflowID(args, true)
			if err != nil {
				return nil, err
			}
			versionNum, err := toInt(args, "version_number", true)
			if err != nil {
				return nil, err
			}
			env, err := toString(args, "env", true)
			if err != nil {
				return nil, err
			}
			path := fmt.Sprintf("/v1/snippets/%s/versions/%d/publish?env=%s",
				url.PathEscape(snippetID),
				versionNum,
				url.QueryEscape(env),
			)
			var out map[string]any
			if err := r.client.Post(ctx, authHeader, path, map[string]any{}, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "invoke_workflow",
		Description: "Invoke a workflow synchronously, asynchronously, or as a stream. tenant_slug is optional; when omitted the tenant is inferred from the API key.",
		InputSchema: map[string]any{
			"type": "object",
			"properties": map[string]any{
				"tenant_slug":   map[string]any{"type": "string", "description": "Tenant slug. Optional — inferred from the API key when omitted."},
				"workflow_id":   map[string]any{"type": "string", "description": "Workflow ID (UUID)."},
				"workflow_slug": map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"snippet_slug":  map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"snippet_id":    map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"env":           map[string]any{"type": "string"},
				"version":       map[string]any{"type": "string"},
				"invoke_mode":   map[string]any{"type": "string", "enum": []string{"sync", "async", "stream"}},
				"callback_url":  map[string]any{"type": "string"},
				"input":         map[string]any{},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			tenantSlug, _ := toString(args, "tenant_slug", false)
			workflowRef, err := toWorkflowInvokeTarget(args)
			if err != nil {
				return nil, err
			}
			env, _ := toString(args, "env", false)
			version, _ := toString(args, "version", false)
			mode, _ := toString(args, "invoke_mode", false)
			callbackURL, _ := toString(args, "callback_url", false)

			rawInput, err := toRawInput(args)
			if err != nil {
				return nil, err
			}

			// Use the slug-free route when tenant_slug is not provided; the control
			// plane resolves the tenant from the authenticated API key.
			var path string
			if tenantSlug != "" {
				path = fmt.Sprintf("/v1/invoke/%s/%s", url.PathEscape(tenantSlug), url.PathEscape(workflowRef))
			} else {
				path = fmt.Sprintf("/v1/invoke/%s", url.PathEscape(workflowRef))
			}
			query := url.Values{}
			if env != "" {
				query.Set("env", env)
			}
			if version != "" {
				query.Set("version", version)
			}
			if encoded := query.Encode(); encoded != "" {
				path += "?" + encoded
			}

			var out map[string]any
			requestBody := any(json.RawMessage(rawInput))
			if callbackURL != "" {
				requestBody = map[string]any{"callback_url": callbackURL}
			}

			if mode == "" || mode == "sync" {
				if err := r.client.Post(ctx, authHeader, path, requestBody, &out); err != nil {
					return nil, err
				}
				return out, nil
			}

			var response map[string]any
			err = r.client.PostWithHeaders(
				ctx,
				authHeader,
				map[string]string{"X-Invoke-Mode": mode},
				path,
				requestBody,
				&response,
			)
			if err != nil {
				return nil, err
			}
			return response, nil
		},
	})

	r.add(Tool{
		Name:        "get_logs",
		Description: "List past invocations for a workflow (status, output, error, stderr, duration, mode). Use get_invocation for the full record of a single run by ID. Note: streamed debug logs (console.log/print) are live-only and not stored here.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"workflow_id"},
			"properties": map[string]any{
				"workflow_id": map[string]any{"type": "string", "description": "Workflow ID."},
				"snippet_id":  map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"limit":       map[string]any{"type": "integer"},
				"status":      map[string]any{"type": "string"},
				"env":         map[string]any{"type": "string"},
				"start_time":  map[string]any{"type": "string"},
				"end_time":    map[string]any{"type": "string"},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			snippetID, err := toWorkflowID(args, true)
			if err != nil {
				return nil, err
			}
			limit, _ := toInt(args, "limit", false)
			status, _ := toString(args, "status", false)
			env, _ := toString(args, "env", false)
			startTime, _ := toString(args, "start_time", false)
			endTime, _ := toString(args, "end_time", false)

			query := map[string]string{
				"status":     status,
				"env":        env,
				"start_time": startTime,
				"end_time":   endTime,
			}
			if limit > 0 {
				query["limit"] = fmt.Sprintf("%d", limit)
			}
			path := "/v1/logs/snippets/" + url.PathEscape(snippetID) + controlplane.Query(query)
			var out map[string]any
			if err := r.client.Get(ctx, authHeader, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "get_invocation",
		Description: "Get a single invocation by ID, including status, output, error, stderr, and duration. Use this to poll an async invocation (invoke_workflow with invoke_mode=async returns an invocation_id) until status is 'completed', 'failed', 'timeout', or 'oom_killed'.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"invocation_id"},
			"properties": map[string]any{
				"invocation_id": map[string]any{"type": "string"},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			invocationID, err := toString(args, "invocation_id", true)
			if err != nil {
				return nil, err
			}
			var out map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/invocations/"+url.PathEscape(invocationID), &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "list_secrets",
		Description: "List secret metadata for the authenticated tenant.",
		InputSchema: map[string]any{"type": "object", "properties": map[string]any{}},
		Handle: func(ctx context.Context, authHeader string, _ map[string]any) (any, error) {
			var out []map[string]any
			if err := r.client.Get(ctx, authHeader, "/v1/secrets", &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "set_secret",
		Description: "Create a secret.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"name", "value"},
			"properties": map[string]any{
				"name":         map[string]any{"type": "string"},
				"value":        map[string]any{"type": "string"},
				"workflow_id":  map[string]any{"type": "string", "description": "Optional workflow ID scope."},
				"snippet_id":   map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"environments": map[string]any{"type": "array", "items": map[string]any{"type": "string"}},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			name, err := toString(args, "name", true)
			if err != nil {
				return nil, err
			}
			value, err := toString(args, "value", true)
			if err != nil {
				return nil, err
			}
			snippetID, _ := toWorkflowID(args, false)
			environments, err := toStringSlice(args, "environments")
			if err != nil {
				return nil, err
			}

			body := map[string]any{
				"name":  name,
				"value": value,
			}
			if snippetID != "" {
				body["snippet_id"] = snippetID
			}
			if environments != nil {
				body["environments"] = environments
			}
			var out map[string]any
			if err := r.client.Post(ctx, authHeader, "/v1/secrets", body, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})

	r.add(Tool{
		Name:        "get_metrics",
		Description: "Get aggregate and time-series metrics for a workflow.",
		InputSchema: map[string]any{
			"type":     "object",
			"required": []string{"workflow_id"},
			"properties": map[string]any{
				"workflow_id": map[string]any{"type": "string", "description": "Workflow ID."},
				"snippet_id":  map[string]any{"type": "string", "description": "Deprecated alias for workflow_id."},
				"window":      map[string]any{"type": "string", "enum": []string{"1h", "24h", "7d"}},
			},
		},
		Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
			snippetID, err := toWorkflowID(args, true)
			if err != nil {
				return nil, err
			}
			window, _ := toString(args, "window", false)
			query := ""
			if strings.TrimSpace(window) != "" {
				query = "?window=" + url.QueryEscape(window)
			}
			path := "/v1/metrics/snippets/" + url.PathEscape(snippetID) + query
			var out map[string]any
			if err := r.client.Get(ctx, authHeader, path, &out); err != nil {
				return nil, err
			}
			return out, nil
		},
	})
	r.add(Tool{Name: "get_event_trigger_docs", Description: "Read the Nango sync event envelope and reliability contract before writing an event workflow.", InputSchema: map[string]any{"type": "object", "properties": map[string]any{}}, Handle: func(ctx context.Context, authHeader string, args map[string]any) (any, error) {
		return map[string]any{"resource": "velane://runtime/event-triggers", "envelope": map[string]any{"id": "stable receipt id", "source": "nango", "provider": "provider slug", "connection": map[string]any{"id": "connection id", "alias": "default"}, "event": map[string]any{"type": "sync.records.changed", "model": "Model", "modified_after": "RFC3339 cursor"}, "changes": map[string]any{"records": []any{map[string]any{"action": "added|updated|deleted", "data": map[string]any{}}}}, "batch": map[string]any{"index": 1, "count": 1, "is_last": true}}, "requirements": []string{"Trigger configuration is separate from workflow code.", "Test a representative envelope before exact-version publication and explicit enablement.", "Delivery is at least once; use receipt id and batch index for idempotency.", "Handle bounded batches, retries, deletion tombstones, and partial records.", "Use integration(provider, { alias: input.connection.alias })."}}, nil
	}})
	r.add(Tool{Name: "list_integration_event_models", Description: "Discover deployed Nango sync models for a connection. manual_entry=true means this Nango installation cannot enumerate them.", InputSchema: map[string]any{"type": "object", "required": []string{"connection_id"}, "properties": map[string]any{"connection_id": map[string]any{"type": "string"}}}, Handle: func(ctx context.Context, a string, args map[string]any) (any, error) {
		id, e := toString(args, "connection_id", true)
		if e != nil {
			return nil, e
		}
		var out map[string]any
		e = r.client.Get(ctx, a, "/v1/connections/"+url.PathEscape(id)+"/sync-models", &out)
		return out, e
	}})
	r.add(Tool{Name: "list_workflow_triggers", Description: "List Nango sync triggers and recent delivery state for a workflow.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id"}, "properties": map[string]any{"workflow_id": map[string]any{"type": "string"}}}, Handle: func(ctx context.Context, a string, args map[string]any) (any, error) {
		id, e := toWorkflowID(args, true)
		if e != nil {
			return nil, e
		}
		var out []map[string]any
		e = r.client.Get(ctx, a, "/v1/snippets/"+url.PathEscape(id)+"/triggers", &out)
		return out, e
	}})
	triggerSchema := map[string]any{"type": "object", "required": []string{"workflow_id", "connection_id", "model", "environment"}, "properties": map[string]any{"workflow_id": map[string]any{"type": "string"}, "connection_id": map[string]any{"type": "string"}, "model": map[string]any{"type": "string"}, "environment": map[string]any{"type": "string", "enum": []string{"dev", "staging", "prod"}}, "change_types": map[string]any{"type": "array", "items": map[string]any{"type": "string", "enum": []string{"added", "updated", "deleted"}}}}}
	r.add(Tool{Name: "create_workflow_trigger", Description: "Create a disabled workflow trigger. Enable it only after representative testing and exact-version publication.", InputSchema: triggerSchema, Handle: func(ctx context.Context, a string, args map[string]any) (any, error) {
		id, e := toWorkflowID(args, true)
		if e != nil {
			return nil, e
		}
		body := map[string]any{"connection_id": args["connection_id"], "model": args["model"], "environment": args["environment"], "change_types": args["change_types"]}
		var out map[string]any
		e = r.client.Post(ctx, a, "/v1/snippets/"+url.PathEscape(id)+"/triggers", body, &out)
		return out, e
	}})
	r.add(Tool{Name: "update_workflow_trigger", Description: "Enable, disable, or update a workflow trigger.", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id", "trigger_id", "model", "environment", "change_types", "enabled"}, "properties": map[string]any{"workflow_id": map[string]any{"type": "string"}, "trigger_id": map[string]any{"type": "string"}, "model": map[string]any{"type": "string"}, "environment": map[string]any{"type": "string"}, "change_types": map[string]any{"type": "array", "items": map[string]any{"type": "string"}}, "enabled": map[string]any{"type": "boolean"}}}, Handle: func(ctx context.Context, a string, args map[string]any) (any, error) {
		wid, e := toWorkflowID(args, true)
		if e != nil {
			return nil, e
		}
		tid, e := toString(args, "trigger_id", true)
		if e != nil {
			return nil, e
		}
		var out map[string]any
		e = r.client.Patch(ctx, a, "/v1/snippets/"+url.PathEscape(wid)+"/triggers/"+url.PathEscape(tid), args, &out)
		return out, e
	}})
	r.add(Tool{Name: "delete_workflow_trigger", Description: "Permanently delete a workflow trigger (admin scope).", InputSchema: map[string]any{"type": "object", "required": []string{"workflow_id", "trigger_id"}, "properties": map[string]any{"workflow_id": map[string]any{"type": "string"}, "trigger_id": map[string]any{"type": "string"}}}, Handle: func(ctx context.Context, a string, args map[string]any) (any, error) {
		wid, e := toWorkflowID(args, true)
		if e != nil {
			return nil, e
		}
		tid, e := toString(args, "trigger_id", true)
		if e != nil {
			return nil, e
		}
		e = r.client.Delete(ctx, a, "/v1/snippets/"+url.PathEscape(wid)+"/triggers/"+url.PathEscape(tid), nil)
		return map[string]any{"deleted": e == nil}, e
	}})
}
