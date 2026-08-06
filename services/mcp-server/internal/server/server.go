package server

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/abskrj/velane/services/mcp-server/internal/prompts"
	"github.com/abskrj/velane/services/mcp-server/internal/protocol"
	"github.com/abskrj/velane/services/mcp-server/internal/resources"
	"github.com/abskrj/velane/services/mcp-server/internal/tools"
	"github.com/go-chi/chi/v5"
)

const defaultProtocolVersion = "2024-11-05"

type Server struct {
	registry  *tools.Registry
	resources *resources.Registry
	prompts   *prompts.Registry
}

func New(registry *tools.Registry) *Server {
	return &Server{
		registry:  registry,
		resources: resources.NewRegistry(registry.Client()),
		prompts:   prompts.NewRegistry(),
	}
}

func (s *Server) Router() http.Handler {
	r := chi.NewRouter()
	r.Get("/healthz", func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusOK)
		_, _ = w.Write([]byte("ok"))
	})
	r.Post("/mcp", s.handleJSONRPC)
	r.Get("/mcp/sse", s.handleSSE)
	return r
}

func (s *Server) HandleRequest(ctx context.Context, authHeader string, req protocol.Request) protocol.Response {
	if req.JSONRPC != "2.0" {
		return protocol.Error(req.ID, -32600, "invalid jsonrpc version", nil)
	}
	switch req.Method {
	case "initialize":
		protocolVersion := defaultProtocolVersion
		if len(req.Params) > 0 {
			var params struct {
				ProtocolVersion string `json:"protocolVersion"`
			}
			if err := json.Unmarshal(req.Params, &params); err == nil && strings.TrimSpace(params.ProtocolVersion) != "" {
				protocolVersion = params.ProtocolVersion
			}
		}
		return protocol.Success(req.ID, map[string]any{
			"protocolVersion": protocolVersion,
			"serverInfo": map[string]any{
				"name":    "velane-mcp-server",
				"version": "0.1.0",
			},
			"capabilities": map[string]any{
				"tools": map[string]any{
					"listChanged": false,
				},
				"resources": map[string]any{
					"listChanged": false,
				},
				"prompts": map[string]any{
					"listChanged": false,
				},
			},
		})
	case "notifications/initialized":
		return protocol.Success(req.ID, nil)
	case "ping":
		return protocol.Success(req.ID, map[string]any{"ok": true})
	case "tools/list":
		return protocol.Success(req.ID, map[string]any{
			"tools": s.registry.List(),
		})
	case "tools/call":
		return s.handleToolsCall(ctx, authHeader, req)
	case "resources/list":
		return protocol.Success(req.ID, map[string]any{"resources": s.resources.List()})
	case "resources/read":
		return s.handleResourcesRead(ctx, authHeader, req)
	case "prompts/list":
		return protocol.Success(req.ID, map[string]any{"prompts": s.prompts.List()})
	case "prompts/get":
		return s.handlePromptsGet(req)
	default:
		return protocol.Error(req.ID, -32601, "method not found", map[string]any{"method": req.Method})
	}
}

func (s *Server) handleToolsCall(ctx context.Context, authHeader string, req protocol.Request) protocol.Response {
	if strings.TrimSpace(authHeader) == "" {
		return protocol.Error(req.ID, -32001, "authorization header is required", nil)
	}

	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := decodeJSON(req.Params, &params); err != nil {
		return protocol.Error(req.ID, -32602, "invalid tools/call params", err.Error())
	}
	if params.Name == "" {
		return protocol.Error(req.ID, -32602, "tool name is required", nil)
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	result, err := s.registry.Call(ctx, authHeader, params.Name, params.Arguments)
	if err != nil {
		return protocol.Success(req.ID, map[string]any{
			"content": []map[string]any{
				{"type": "text", "text": err.Error()},
			},
			"isError": true,
		})
	}

	structured, text := formatToolResult(params.Name, result)
	return protocol.Success(req.ID, map[string]any{
		"structuredContent": structured,
		"content": []map[string]any{
			{
				"type": "text",
				"text": text,
			},
		},
		"isError": false,
	})
}

// formatToolResult shapes tool output for MCP clients. Cursor and other hosts validate
// structuredContent as a JSON object (record), not a top-level array.
func formatToolResult(toolName string, result any) (map[string]any, string) {
	if result == nil {
		return map[string]any{}, fmt.Sprintf("%s completed successfully", toolName)
	}

	b, err := json.Marshal(result)
	if err != nil {
		return map[string]any{"value": fmt.Sprint(result)}, fmt.Sprintf("%s completed successfully", toolName)
	}

	if len(b) > 0 && b[0] == '[' {
		var items []any
		if err := decodeJSON(b, &items); err == nil {
			key := listResultKey(toolName)
			structured := map[string]any{key: items}
			return structured, fmt.Sprintf("%s completed successfully\n%s", toolName, string(b))
		}
	}

	var obj map[string]any
	if err := decodeJSON(b, &obj); err == nil && obj != nil {
		text := fmt.Sprintf("%s completed successfully\n%s", toolName, string(b))
		if toolName == "get_integration_docs" {
			text = formatIntegrationDocsText(obj)
		}
		return obj, text
	}

	return map[string]any{"value": result}, fmt.Sprintf("%s completed successfully", toolName)
}

func formatIntegrationDocsText(doc map[string]any) string {
	provider, _ := doc["provider"].(string)
	name, _ := doc["name"].(string)
	baseURL, _ := doc["base_url"].(string)
	docsURL, _ := doc["docs_url"].(string)
	authMode, _ := doc["auth_mode"].(string)
	note, _ := doc["note"].(string)
	bunEx, _ := doc["bun_example"].(string)
	pyEx, _ := doc["python_example"].(string)

	var b strings.Builder
	b.WriteString("# Integration docs: ")
	if name != "" {
		b.WriteString(name)
	} else {
		b.WriteString(provider)
	}
	if provider != "" {
		b.WriteString(" (")
		b.WriteString(provider)
		b.WriteString(")")
	}
	b.WriteString("\n\n")
	if baseURL != "" {
		b.WriteString("Base URL: ")
		b.WriteString(baseURL)
		b.WriteString("\n")
	}
	if docsURL != "" {
		b.WriteString("Official docs: ")
		b.WriteString(docsURL)
		b.WriteString("\n")
	}
	if authMode != "" {
		b.WriteString("Auth: ")
		b.WriteString(authMode)
		b.WriteString("\n")
	}
	if note != "" {
		b.WriteString("\n")
		b.WriteString(note)
		b.WriteString("\n")
	}

	endpoints, ok := doc["common_endpoints"].([]any)
	if ok && len(endpoints) > 0 {
		b.WriteString("\n## Common endpoints\n")
		for _, ep := range endpoints {
			m, ok := ep.(map[string]any)
			if !ok {
				continue
			}
			method, _ := m["method"].(string)
			path, _ := m["path"].(string)
			desc, _ := m["description"].(string)
			b.WriteString("- ")
			b.WriteString(method)
			b.WriteString(" ")
			b.WriteString(path)
			if desc != "" {
				b.WriteString(" — ")
				b.WriteString(desc)
			}
			b.WriteString("\n")
		}
	}

	if bunEx != "" {
		b.WriteString("\n## Bun example\n```typescript\n")
		b.WriteString(bunEx)
		b.WriteString("\n```\n")
	}
	if pyEx != "" {
		b.WriteString("\n## Python example\n```python\n")
		b.WriteString(pyEx)
		b.WriteString("\n```\n")
	}
	if nangoMD, _ := doc["nango_docs_markdown"].(string); nangoMD != "" {
		b.WriteString("\n## Nango provider guide\n\n")
		b.WriteString(nangoMD)
		b.WriteString("\n")
	}
	return strings.TrimSpace(b.String())
}

func listResultKey(toolName string) string {
	switch toolName {
	case "list_workflows", "list_snippets":
		return "workflows"
	case "list_connections":
		return "connections"
	case "list_secrets":
		return "secrets"
	default:
		return "items"
	}
}

func (s *Server) handleResourcesRead(ctx context.Context, authHeader string, req protocol.Request) protocol.Response {
	if strings.TrimSpace(authHeader) == "" {
		return protocol.Error(req.ID, -32001, "authorization header is required", nil)
	}

	var params struct {
		URI string `json:"uri"`
	}
	if err := decodeJSON(req.Params, &params); err != nil {
		return protocol.Error(req.ID, -32602, "invalid resources/read params", err.Error())
	}
	if strings.TrimSpace(params.URI) == "" {
		return protocol.Error(req.ID, -32602, "resource uri is required", nil)
	}

	contents, err := s.resources.Read(ctx, authHeader, params.URI)
	if err != nil {
		return protocol.Error(req.ID, -32002, err.Error(), nil)
	}
	return protocol.Success(req.ID, map[string]any{"contents": contents})
}

func (s *Server) handlePromptsGet(req protocol.Request) protocol.Response {
	var params struct {
		Name      string         `json:"name"`
		Arguments map[string]any `json:"arguments"`
	}
	if err := decodeJSON(req.Params, &params); err != nil {
		return protocol.Error(req.ID, -32602, "invalid prompts/get params", err.Error())
	}
	if strings.TrimSpace(params.Name) == "" {
		return protocol.Error(req.ID, -32602, "prompt name is required", nil)
	}
	if params.Arguments == nil {
		params.Arguments = map[string]any{}
	}

	prompt, messages, err := s.prompts.Get(params.Name, params.Arguments)
	if err != nil {
		return protocol.Error(req.ID, -32003, err.Error(), nil)
	}
	return protocol.Success(req.ID, map[string]any{
		"description": prompt.Description,
		"messages":    messages,
	})
}

func (s *Server) handleJSONRPC(w http.ResponseWriter, r *http.Request) {
	var req protocol.Request
	decoder := json.NewDecoder(r.Body)
	decoder.UseNumber()
	if err := decoder.Decode(&req); err != nil {
		writeJSON(w, http.StatusBadRequest, protocol.Error(nil, -32700, "parse error", err.Error()))
		return
	}
	if isNotification(req) {
		_ = s.HandleRequest(r.Context(), r.Header.Get("Authorization"), req)
		w.WriteHeader(http.StatusNoContent)
		return
	}
	resp := s.HandleRequest(r.Context(), r.Header.Get("Authorization"), req)
	writeJSON(w, http.StatusOK, resp)
}

func (s *Server) handleSSE(w http.ResponseWriter, r *http.Request) {
	flusher, ok := w.(http.Flusher)
	if !ok {
		http.Error(w, "streaming unsupported", http.StatusInternalServerError)
		return
	}
	w.Header().Set("Content-Type", "text/event-stream")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Connection", "keep-alive")

	_, _ = fmt.Fprintf(w, "event: endpoint\ndata: /mcp\n\n")
	flusher.Flush()

	ticker := time.NewTicker(15 * time.Second)
	defer ticker.Stop()
	for {
		select {
		case <-r.Context().Done():
			return
		case <-ticker.C:
			_, _ = fmt.Fprintf(w, ": keep-alive\n\n")
			flusher.Flush()
		}
	}
}

func writeJSON(w http.ResponseWriter, status int, body any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(body)
}

func decodeJSON(data []byte, out any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
		return err
	}
	if err := decoder.Decode(&struct{}{}); err != io.EOF {
		if err == nil {
			return fmt.Errorf("invalid trailing data")
		}
		return err
	}
	return nil
}

func isNotification(req protocol.Request) bool {
	return req.ID == nil && strings.HasPrefix(req.Method, "notifications/")
}
