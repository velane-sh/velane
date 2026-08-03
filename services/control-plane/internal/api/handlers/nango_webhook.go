package handlers

import (
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"io"
	"net/http"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	"github.com/abskrj/velane/services/control-plane/internal/nango"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
	"go.uber.org/zap"
)

// NangoWebhookStore is the store subset needed by NangoWebhookHandler.
type NangoWebhookStore interface {
	UpdateNangoConnectionID(ctx context.Context, tenantID, provider, alias, nangoConnID string) (*models.Connection, error)
	UpdateNangoConnectionIDByProviderConfigKey(ctx context.Context, tenantID, providerConfigKey, nangoConnID string) (*models.Connection, error)
	FindConnectionForNangoEvent(ctx context.Context, nangoID, configKey string) (*models.Connection, error)
	CreateIntegrationEventReceipt(ctx context.Context, receipt *models.IntegrationEventReceipt) (bool, error)
	DeletePendingIntegrationEventReceipt(ctx context.Context, id string) error
}
type IntegrationEventEnqueuer interface {
	EnqueueIntegrationEvent(context.Context, redisstore.IntegrationEventJob) error
}

// NangoWebhookHandler handles POST /v1/webhooks/nango.
type NangoWebhookHandler struct {
	store         NangoWebhookStore
	nango         *nango.Client
	webhookSecret string // NANGO_WEBHOOK_SECRET — dashboard webhook signing key
	apiSecret     string // NANGO_SECRET_KEY — self-hosted Nango also signs with the env API secret
	log           *zap.Logger
	events        IntegrationEventEnqueuer
}

func NewNangoWebhookHandler(store NangoWebhookStore, nangoClient *nango.Client, webhookSecret, apiSecret string, log *zap.Logger) *NangoWebhookHandler {
	return &NangoWebhookHandler{store: store, nango: nangoClient, webhookSecret: webhookSecret, apiSecret: apiSecret, log: log}
}
func (h *NangoWebhookHandler) WithIntegrationEvents(q IntegrationEventEnqueuer) *NangoWebhookHandler {
	h.events = q
	return h
}

// nangoWebhookPayload is the subset of Nango's webhook body that we care about.
type nangoWebhookPayload struct {
	Type              string            `json:"type"`
	ConnectionID      string            `json:"connectionId"`
	ProviderConfigKey string            `json:"providerConfigKey"`
	Success           bool              `json:"success"`
	Model             string            `json:"model"`
	SyncName          string            `json:"syncName"`
	ModifiedAfter     string            `json:"modifiedAfter"`
	WebhookID         string            `json:"id"`
	InitialSync       bool              `json:"initialSync"`
	Added             int               `json:"added"`
	Updated           int               `json:"updated"`
	Deleted           int               `json:"deleted"`
	Tags              map[string]string `json:"tags"`
	EndUser           struct {
		ID          string `json:"id"`
		EndUserID   string `json:"endUserId"`
		DisplayName string `json:"displayName"`
	} `json:"endUser"`
}

func (p *nangoWebhookPayload) tenantID() string {
	if id := p.EndUser.ID; id != "" {
		return id
	}
	if id := p.EndUser.EndUserID; id != "" {
		return id
	}
	if p.Tags != nil {
		if id := p.Tags["end_user_id"]; id != "" {
			return id
		}
	}
	return ""
}

// HandleNangoEvent receives Nango webhook events and stores the real connection UUID.
func (h *NangoWebhookHandler) HandleNangoEvent(w http.ResponseWriter, r *http.Request) {
	body, err := io.ReadAll(io.LimitReader(r.Body, (1<<20)+1))
	if err != nil {
		writeError(w, http.StatusBadRequest, "failed to read body")
		return
	}
	if len(body) > 1<<20 {
		writeError(w, http.StatusRequestEntityTooLarge, "webhook payload too large")
		return
	}

	if h.webhookSecret != "" || h.apiSecret != "" {
		if !verifyNangoWebhook(body, h.webhookSecret, h.apiSecret, r.Header) {
			writeError(w, http.StatusUnauthorized, "invalid webhook signature")
			return
		}
	} else {
		h.log.Warn("NANGO_WEBHOOK_SECRET not set — skipping signature verification")
	}

	var payload nangoWebhookPayload
	if err := json.Unmarshal(body, &payload); err != nil {
		writeError(w, http.StatusBadRequest, "invalid JSON")
		return
	}
	if payload.Type == "sync" || payload.Type == "sync.records.changed" || payload.Type == "sync_model_changed" {
		h.handleSyncEvent(w, r, &payload, body)
		return
	}

	// Only handle successful auth events; acknowledge everything else silently.
	if payload.Type != "auth" || !payload.Success {
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}

	tenantID := payload.tenantID()
	provider := payload.ProviderConfigKey
	nangoConnID := payload.ConnectionID

	if tenantID == "" || provider == "" || nangoConnID == "" {
		writeError(w, http.StatusBadRequest, "missing required fields in webhook payload")
		return
	}

	conn, err := h.store.UpdateNangoConnectionIDByProviderConfigKey(r.Context(), tenantID, provider, nangoConnID)
	if err != nil {
		// The row may not exist yet if the webhook beats the frontend's RecordConnection call
		// (a rare race). Return 200 so Nango does not retry — the frontend will create the row
		// shortly after and the user can re-authenticate to get the UUID written.
		h.log.Warn("nango webhook: connection row not found — RecordConnection not yet called",
			zap.String("tenant_id", tenantID),
			zap.String("provider", provider),
			zap.Error(err),
		)
		writeJSON(w, http.StatusOK, map[string]string{"status": "deferred"})
		return
	}

	// Write the alias back to Nango as connection metadata so it's visible on the
	// Nango side. Best-effort — a failure here does not affect our own DB state.
	if h.nango != nil {
		providerConfigKey := conn.ProviderConfigKey
		if providerConfigKey == "" {
			providerConfigKey = provider
		}
		if err := h.nango.PatchConnectionMetadata(r.Context(), nangoConnID, providerConfigKey, map[string]any{
			"velane_alias":     conn.Alias,
			"velane_tenant_id": tenantID,
		}); err != nil {
			h.log.Warn("nango webhook: failed to patch connection metadata (non-fatal)",
				zap.String("nango_connection_id", nangoConnID),
				zap.Error(err),
			)
		}
	}

	h.log.Info("nango webhook: connection ID stored",
		zap.String("tenant_id", tenantID),
		zap.String("provider", provider),
		zap.String("alias", conn.Alias),
		zap.String("nango_connection_id", nangoConnID),
	)
	writeJSON(w, http.StatusOK, map[string]string{"status": "ok"})
}

func (h *NangoWebhookHandler) handleSyncEvent(w http.ResponseWriter, r *http.Request, p *nangoWebhookPayload, body []byte) {
	if p.ConnectionID == "" || p.ProviderConfigKey == "" || p.Model == "" || p.ModifiedAfter == "" {
		writeError(w, http.StatusBadRequest, "missing required sync event fields")
		return
	}
	conn, err := h.store.FindConnectionForNangoEvent(r.Context(), p.ConnectionID, p.ProviderConfigKey)
	if err != nil {
		h.log.Warn("nango sync event for unknown connection", zap.Error(err))
		writeJSON(w, http.StatusOK, map[string]string{"status": "ignored"})
		return
	}
	key := p.WebhookID
	if key == "" {
		canonicalBody := body
		var raw any
		if json.Unmarshal(body, &raw) == nil {
			if encoded, e := json.Marshal(raw); e == nil {
				canonicalBody = encoded
			}
		}
		canonical := fmt.Sprintf("%s\n%s\n%s\n%s\n%d\n%d\n%d\n%s", p.ConnectionID, p.ProviderConfigKey, p.Model, p.ModifiedAfter, p.Added, p.Updated, p.Deleted, string(canonicalBody))
		sum := sha256.Sum256([]byte(canonical))
		key = hex.EncodeToString(sum[:])
	}
	receipt := &models.IntegrationEventReceipt{DeduplicationKey: key, TenantID: conn.TenantID, ConnectionID: conn.ID, ProviderConfigKey: p.ProviderConfigKey, Model: p.Model, ModifiedAfter: p.ModifiedAfter, InitialSync: p.InitialSync, Payload: body}
	created, err := h.store.CreateIntegrationEventReceipt(r.Context(), receipt)
	if err != nil {
		writeError(w, http.StatusServiceUnavailable, "failed to persist event")
		return
	}
	if !created {
		writeJSON(w, http.StatusOK, map[string]string{"status": "duplicate"})
		return
	}
	if h.events == nil || h.events.EnqueueIntegrationEvent(r.Context(), redisstore.IntegrationEventJob{ReceiptID: receipt.ID}) != nil {
		// Roll back only the still-pending receipt so Nango's retry can enqueue it.
		_ = h.store.DeletePendingIntegrationEventReceipt(r.Context(), receipt.ID)
		writeError(w, http.StatusServiceUnavailable, "failed to enqueue event")
		return
	}
	writeJSON(w, http.StatusAccepted, map[string]string{"status": "accepted", "id": receipt.ID})
}

// verifyNangoWebhook checks Nango webhook signatures.
//
// Self-hosted Nango signs outgoing webhooks with the environment API secret
// (NANGO_SECRET_KEY_DEV), sending:
//   - X-Nango-Hmac-Sha256: HMAC-SHA256(secret, body)
//   - X-Nango-Signature: SHA256(secret + body)  (legacy)
//
// Nango Cloud / dashboard docs also describe a separate webhook signing key;
// we accept HMAC on X-Nango-Signature with that key for backwards compatibility.
func verifyNangoWebhook(body []byte, webhookSecret, apiSecret string, headers http.Header) bool {
	secrets := []string{}
	for _, s := range []string{webhookSecret, apiSecret} {
		if s == "" {
			continue
		}
		dup := false
		for _, existing := range secrets {
			if existing == s {
				dup = true
				break
			}
		}
		if !dup {
			secrets = append(secrets, s)
		}
	}
	if len(secrets) == 0 {
		return false
	}

	if sig := headers.Get("X-Nango-Hmac-Sha256"); sig != "" {
		for _, secret := range secrets {
			if verifyNangoHMAC(body, secret, sig) {
				return true
			}
		}
	}

	if sig := headers.Get("X-Nango-Signature"); sig != "" {
		for _, secret := range secrets {
			if verifyNangoLegacySignature(body, secret, sig) || verifyNangoHMAC(body, secret, sig) {
				return true
			}
		}
	}

	return false
}

func verifyNangoHMAC(body []byte, secret, sig string) bool {
	if sig == "" {
		return false
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(body)
	expected := hex.EncodeToString(mac.Sum(nil))
	return hmac.Equal([]byte(expected), []byte(sig))
}

func verifyNangoLegacySignature(body []byte, secret, sig string) bool {
	if sig == "" {
		return false
	}
	combined := append([]byte(secret), body...)
	sum := sha256.Sum256(combined)
	expected := hex.EncodeToString(sum[:])
	return hmac.Equal([]byte(expected), []byte(sig))
}
