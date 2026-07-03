package tools

import (
	"context"
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/abskrj/velane/services/mcp-server/internal/controlplane"
)

type Handler func(ctx context.Context, authHeader string, args map[string]any) (any, error)

type Tool struct {
	Name        string         `json:"name"`
	Description string         `json:"description"`
	InputSchema map[string]any `json:"inputSchema"`
	Handle      Handler        `json:"-"`
}

type Registry struct {
	client *controlplane.Client
	tools  map[string]Tool
	order  []string
}

func NewRegistry(client *controlplane.Client) *Registry {
	r := &Registry{
		client: client,
		tools:  map[string]Tool{},
	}
	r.addDefaults()
	return r
}

func (r *Registry) Client() *controlplane.Client {
	return r.client
}

func (r *Registry) List() []Tool {
	out := make([]Tool, 0, len(r.order))
	for _, name := range r.order {
		out = append(out, r.tools[name])
	}
	return out
}

// legacyToolNames maps pre-workflow MCP tool names to their current equivalents.
var legacyToolNames = map[string]string{
	"list_snippets":              "list_workflows",
	"get_snippet":                "get_workflow",
	"create_snippet":             "create_workflow",
	"publish_snippet":            "publish_workflow",
	"invoke_snippet":             "invoke_workflow",
}

func (r *Registry) Call(ctx context.Context, authHeader, name string, args map[string]any) (any, error) {
	if alias, ok := legacyToolNames[name]; ok {
		name = alias
	}
	t, ok := r.tools[name]
	if !ok {
		return nil, fmt.Errorf("unknown tool: %s", name)
	}
	start := time.Now()
	result, err := t.Handle(ctx, authHeader, args)
	go fireMCPToolCall(name, time.Since(start).Milliseconds(), err == nil)
	return result, err
}

func (r *Registry) add(tool Tool) {
	r.tools[tool.Name] = tool
	r.order = append(r.order, tool.Name)
}

func toWorkflowID(args map[string]any, required bool) (string, error) {
	id, err := toString(args, "workflow_id", false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(id) != "" {
		return id, nil
	}
	return toString(args, "snippet_id", required)
}

func toWorkflowSlug(args map[string]any, required bool) (string, error) {
	slug, err := toString(args, "workflow_slug", false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(slug) != "" {
		return slug, nil
	}
	return toString(args, "snippet_slug", required)
}

func toWorkflowInvokeTarget(args map[string]any) (string, error) {
	id, err := toWorkflowID(args, false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(id) != "" {
		return id, nil
	}
	slug, err := toWorkflowSlug(args, false)
	if err != nil {
		return "", err
	}
	if strings.TrimSpace(slug) != "" {
		return slug, nil
	}
	return "", fmt.Errorf("workflow_id is required")
}

func toString(args map[string]any, key string, required bool) (string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		if required {
			return "", fmt.Errorf("missing required argument: %s", key)
		}
		return "", nil
	}
	s, ok := v.(string)
	if !ok {
		return "", fmt.Errorf("argument %s must be a string", key)
	}
	if required && strings.TrimSpace(s) == "" {
		return "", fmt.Errorf("argument %s must not be empty", key)
	}
	return s, nil
}

func toInt(args map[string]any, key string, required bool) (int, error) {
	v, ok := args[key]
	if !ok || v == nil {
		if required {
			return 0, fmt.Errorf("missing required argument: %s", key)
		}
		return 0, nil
	}
	switch n := v.(type) {
	case float64:
		return int(n), nil
	case int:
		return n, nil
	case string:
		parsed, err := strconv.Atoi(n)
		if err != nil {
			return 0, fmt.Errorf("argument %s must be an integer", key)
		}
		return parsed, nil
	default:
		return 0, fmt.Errorf("argument %s must be an integer", key)
	}
}

func toStringSlice(args map[string]any, key string) ([]string, error) {
	v, ok := args[key]
	if !ok || v == nil {
		return nil, nil
	}
	items, ok := v.([]any)
	if !ok {
		return nil, fmt.Errorf("argument %s must be an array", key)
	}
	out := make([]string, 0, len(items))
	for _, item := range items {
		s, ok := item.(string)
		if !ok {
			return nil, fmt.Errorf("argument %s must contain only strings", key)
		}
		out = append(out, s)
	}
	return out, nil
}

func toRawInput(args map[string]any) (json.RawMessage, error) {
	v, ok := args["input"]
	if !ok || v == nil {
		return json.RawMessage(`{}`), nil
	}
	switch typed := v.(type) {
	case string:
		trimmed := strings.TrimSpace(typed)
		if trimmed == "" {
			return json.RawMessage(`{}`), nil
		}
		if !json.Valid([]byte(trimmed)) {
			return nil, fmt.Errorf("input string must be valid JSON")
		}
		return json.RawMessage(trimmed), nil
	default:
		b, err := json.Marshal(typed)
		if err != nil {
			return nil, fmt.Errorf("marshal input: %w", err)
		}
		return b, nil
	}
}
