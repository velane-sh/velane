package telemetry

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"net/http"
	"os"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"go.uber.org/zap"
)

const (
	defaultTelemetryURL   = "https://telemetry.velane.sh/tm/event"
	heartbeatInterval     = 24 * time.Hour
)

func telemetryEndpoint() string {
	if u := os.Getenv("VELANE_TELEMETRY_URL"); u != "" {
		return u
	}
	return defaultTelemetryURL
}

func RunHeartbeat(ctx context.Context, store *postgres.Store, publicBaseURL, licenseKey string, log *zap.Logger) {
	// Fire once at startup (after a short delay so the server is ready), then every 24h.
	select {
	case <-ctx.Done():
		return
	case <-time.After(30 * time.Second):
	}
	fireHeartbeat(ctx, store, publicBaseURL, licenseKey, log)

	ticker := time.NewTicker(heartbeatInterval)
	defer ticker.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			fireHeartbeat(ctx, store, publicBaseURL, licenseKey, log)
		}
	}
}

func fireHeartbeat(ctx context.Context, store *postgres.Store, publicBaseURL, licenseKey string, log *zap.Logger) {
	stats, err := store.GetTelemetryStats(ctx)
	if err != nil {
		log.Debug("telemetry: stats query failed", zap.Error(err))
		return
	}

	instanceID := hashInstanceID(publicBaseURL)

	props := map[string]any{
		"admin_domain":       publicBaseURL,
		"snippet_count":      stats.SnippetCount,
		"member_count":       stats.MemberCount,
		"invocations_24h":    stats.InvocationCount24h,
		"connection_count":   stats.ConnectionCount,
		"embed_token_count":  stats.EmbedTokenCount,
		"bun_snippets":       stats.BunSnippetCount,
		"python_snippets":    stats.PythonSnippetCount,
		"has_license":        licenseKey != "",
	}

	fire(telemetryEndpoint(), instanceID, "instance.heartbeat", props)
}

func hashInstanceID(publicBaseURL string) string {
	h := sha256.Sum256([]byte(publicBaseURL))
	return hex.EncodeToString(h[:8])
}

// Fire sends a telemetry event fire-and-forget (non-blocking, fail-silently).
func Fire(event, instanceID string, props map[string]any) {
	go fire(telemetryEndpoint(), instanceID, event, props)
}

func fire(endpoint, instanceID, event string, props map[string]any) {
	payload := map[string]any{
		"event":       event,
		"instance_id": instanceID,
		"properties":  props,
	}
	b, err := json.Marshal(payload)
	if err != nil {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(b))
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
