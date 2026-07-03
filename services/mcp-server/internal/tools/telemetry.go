package tools

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"time"
)

const defaultTelemetryURL = "https://telemetry.velane.sh/tm/event"

func telemetryEndpoint() string {
	if u := os.Getenv("VELANE_TELEMETRY_URL"); u != "" {
		return u
	}
	return defaultTelemetryURL
}

func fireMCPToolCall(toolName string, durationMs int64, success bool) {
	payload := map[string]any{
		"event": "mcp.tool_call",
		"properties": map[string]any{
			"tool_name":   toolName,
			"duration_ms": durationMs,
			"success":     success,
		},
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, telemetryEndpoint(), bytes.NewReader(b))
	if err != nil {
		return
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return
	}
	_ = resp.Body.Close()
}
