package telemetry

import (
	"bytes"
	"context"
	"encoding/json"
	"net/http"
	"os"
	"runtime"
	"time"
)

const (
	defaultTelemetryURL = "https://telemetry.velane.sh/tm/event"
	cliVersion          = "0.1.0"
)

func endpointURL() string {
	if u := os.Getenv("VELANE_TELEMETRY_URL"); u != "" {
		return u
	}
	return defaultTelemetryURL
}

// Fire sends a telemetry event asynchronously (non-blocking, fail-silently).
func Fire(event string, props map[string]any) {
	if props == nil {
		props = map[string]any{}
	}
	props["cli_version"] = cliVersion
	props["os"] = runtime.GOOS
	props["arch"] = runtime.GOARCH

	url := endpointURL()
	go func() {
		payload := map[string]any{
			"event":      event,
			"properties": props,
		}
		b, err := json.Marshal(payload)
		if err != nil {
			return
		}
		ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		req, err := http.NewRequestWithContext(ctx, http.MethodPost, url, bytes.NewReader(b))
		if err != nil {
			return
		}
		req.Header.Set("Content-Type", "application/json")
		resp, err := http.DefaultClient.Do(req)
		if err != nil {
			return
		}
		_ = resp.Body.Close()
	}()
}
