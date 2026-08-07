package server_test

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abskrj/velane/services/mcp-server/internal/controlplane"
	"github.com/abskrj/velane/services/mcp-server/internal/protocol"
	"github.com/abskrj/velane/services/mcp-server/internal/server"
	"github.com/abskrj/velane/services/mcp-server/internal/tools"
)

func TestInitialize(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "initialize",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	if resp.Result == nil {
		t.Fatal("result should not be nil")
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "protocolVersion") {
		t.Fatalf("expected initialize response to include protocolVersion: %s", string(raw))
	}
	for _, capability := range []string{"tools", "resources", "prompts"} {
		if !strings.Contains(string(raw), capability) {
			t.Fatalf("expected initialize response to include %s capability: %s", capability, string(raw))
		}
	}
}

func TestToolsList(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/list",
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "list_workflows") {
		t.Fatalf("expected tool list to include list_workflows: %s", string(raw))
	}
}

func TestToolsCallListWorkflows(t *testing.T) {
	cp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get("Authorization"); got != "Bearer test" {
			http.Error(w, `{"error":"bad auth"}`, http.StatusUnauthorized)
			return
		}
		if r.URL.Path != "/v1/snippets" {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[{"id":"sn_1","slug":"hello"}]`))
	}))
	defer cp.Close()

	srv := server.New(tools.NewRegistry(controlplane.New(cp.URL)))
	params := map[string]any{
		"name":      "list_workflows",
		"arguments": map[string]any{},
	}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      2,
		Method:  "tools/call",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	b, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(b), "structuredContent") {
		t.Fatalf("expected structuredContent in result: %s", string(b))
	}
	var parsed struct {
		StructuredContent map[string]any `json:"structuredContent"`
	}
	if err := json.Unmarshal(b, &parsed); err != nil {
		t.Fatalf("unmarshal result: %v", err)
	}
	if parsed.StructuredContent == nil {
		t.Fatalf("structuredContent should be an object: %s", string(b))
	}
	if _, ok := parsed.StructuredContent["workflows"]; !ok {
		t.Fatalf("expected workflows key in structuredContent: %s", string(b))
	}
}

func TestHandleJSONRPCEndpoint(t *testing.T) {
	cp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[]`))
	}))
	defer cp.Close()

	srv := server.New(tools.NewRegistry(controlplane.New(cp.URL)))
	httpSrv := httptest.NewServer(srv.Router())
	defer httpSrv.Close()

	reqBody := `{"jsonrpc":"2.0","id":1,"method":"tools/list","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/mcp", strings.NewReader(reqBody))
	req.Header.Set("Authorization", "Bearer test")
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("status = %d; want 200", resp.StatusCode)
	}
}

func TestHandleJSONRPCInitializedNotification(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	httpSrv := httptest.NewServer(srv.Router())
	defer httpSrv.Close()

	reqBody := `{"jsonrpc":"2.0","method":"notifications/initialized","params":{}}`
	req, _ := http.NewRequest(http.MethodPost, httpSrv.URL+"/mcp", strings.NewReader(reqBody))
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		t.Fatalf("request failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusNoContent {
		t.Fatalf("status = %d; want 204", resp.StatusCode)
	}
}

func TestKVToolsPreserveExactJSONNumbersOverJSONRPC(t *testing.T) {
	const largeNumber = "9007199254740993"
	var setValue json.RawMessage
	cp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch {
		case r.Method == http.MethodPut && r.URL.Path == "/v1/kv/entry":
			var body struct {
				Value json.RawMessage `json:"value"`
			}
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				http.Error(w, err.Error(), http.StatusBadRequest)
				return
			}
			setValue = body.Value
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"key":"large"}`))
		case r.Method == http.MethodGet && r.URL.Path == "/v1/kv/entry":
			w.Header().Set("Content-Type", "application/json")
			_, _ = w.Write([]byte(`{"value":` + largeNumber + `}`))
		default:
			http.Error(w, "unexpected route", http.StatusNotFound)
		}
	}))
	defer cp.Close()

	httpSrv := httptest.NewServer(server.New(tools.NewRegistry(controlplane.New(cp.URL))).Router())
	defer httpSrv.Close()

	post := func(body string) []byte {
		t.Helper()
		req, err := http.NewRequest(http.MethodPost, httpSrv.URL+"/mcp", strings.NewReader(body))
		if err != nil {
			t.Fatalf("new request: %v", err)
		}
		req.Header.Set("Authorization", "Bearer test")
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatalf("request: %v", err)
		}
		defer resp.Body.Close()
		if resp.StatusCode != http.StatusOK {
			t.Fatalf("status = %d; want 200", resp.StatusCode)
		}
		responseBody, err := io.ReadAll(resp.Body)
		if err != nil {
			t.Fatalf("read response: %v", err)
		}
		return responseBody
	}

	post(`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"kv_set","arguments":{"key":"large","value":` + largeNumber + `}}}`)
	if got := string(setValue); got != largeNumber {
		t.Fatalf("kv_set value = %s; want %s", got, largeNumber)
	}

	responseBody := post(`{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"kv_get","arguments":{"key":"large"}}}`)
	if !bytes.Contains(responseBody, []byte(`"value":`+largeNumber)) {
		t.Fatalf("kv_get response lost exact number: %s", responseBody)
	}
}

func TestResourcesAndPromptsListAreEmpty(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	for _, method := range []string{"resources/list", "prompts/list"} {
		resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
			JSONRPC: "2.0",
			ID:      1,
			Method:  method,
		})
		if resp.Error != nil {
			t.Fatalf("%s returned error: %+v", method, resp.Error)
		}
		raw, _ := json.Marshal(resp.Result)
		if !strings.Contains(string(raw), strings.TrimSuffix(method, "/list")) {
			t.Fatalf("%s returned unexpected result: %s", method, string(raw))
		}
	}
}

func TestResourcesReadRuntimeContract(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	params := map[string]any{"uri": "velane://runtime/contract"}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/read",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "Velane Runtime Contract") {
		t.Fatalf("expected runtime contract content: %s", string(raw))
	}
	if !strings.Contains(string(raw), "Mastra") {
		t.Fatalf("expected runtime contract to mention Mastra: %s", string(raw))
	}
}

func TestResourcesReadAgentFrameworks(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	params := map[string]any{"uri": "velane://runtime/agent-frameworks"}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/read",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "LangGraph") {
		t.Fatalf("expected agent framework content: %s", string(raw))
	}
}

func TestToolsCallGetAgentFrameworkDocs(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	params := map[string]any{
		"name":      "get_agent_framework_docs",
		"arguments": map[string]any{},
	}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "tools/call",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "@mastra/core") {
		t.Fatalf("expected Mastra in tool result: %s", string(raw))
	}
}

func TestResourcesReadWorkflowCatalogTruncates(t *testing.T) {
	cp := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/snippets" {
			http.Error(w, `{"error":"not found"}`, http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`[
			{"id":"1","slug":"one","name":"One","language":"bun","code":"should-not-leak"},
			{"id":"2","slug":"two","name":"Two","language":"python","code":"should-not-leak"}
		]`))
	}))
	defer cp.Close()

	srv := server.New(tools.NewRegistry(controlplane.New(cp.URL)))
	params := map[string]any{"uri": "velane://workflows"}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "resources/read",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if strings.Contains(string(raw), "should-not-leak") {
		t.Fatalf("workflow catalog should not include code: %s", string(raw))
	}
	if !strings.Contains(string(raw), "one") || !strings.Contains(string(raw), "two") {
		t.Fatalf("expected compact workflow metadata: %s", string(raw))
	}
}

func TestPromptsGet(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	params := map[string]any{
		"name": "create_integration_workflow",
		"arguments": map[string]any{
			"provider": "github",
			"goal":     "create an issue",
		},
	}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/get",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "list_connections") || !strings.Contains(string(raw), "github") {
		t.Fatalf("expected workflow prompt content: %s", string(raw))
	}
}

func TestPromptsGetCreateAgentWorkflow(t *testing.T) {
	srv := server.New(tools.NewRegistry(controlplane.New("http://localhost:1")))
	params := map[string]any{
		"name": "create_agent_workflow",
		"arguments": map[string]any{
			"goal":     "summarize support tickets",
			"language": "bun",
		},
	}
	pb, _ := json.Marshal(params)
	resp := srv.HandleRequest(context.Background(), "Bearer test", protocol.Request{
		JSONRPC: "2.0",
		ID:      1,
		Method:  "prompts/get",
		Params:  pb,
	})
	if resp.Error != nil {
		t.Fatalf("unexpected error: %+v", resp.Error)
	}
	raw, _ := json.Marshal(resp.Result)
	if !strings.Contains(string(raw), "get_agent_framework_docs") || !strings.Contains(string(raw), "Mastra") {
		t.Fatalf("expected agent workflow prompt: %s", string(raw))
	}
}
