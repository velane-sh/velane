package handlers

import (
	"bytes"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/abskrj/velane/services/control-plane/internal/models"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
	"go.uber.org/zap"
)

type webhookTestStore struct {
	conn     *models.Connection
	receipts map[string]string
	deleted  []string
	findErr  bool
}

func (s *webhookTestStore) UpdateNangoConnectionID(context.Context, string, string, string, string) (*models.Connection, error) {
	return s.conn, nil
}
func (s *webhookTestStore) UpdateNangoConnectionIDByProviderConfigKey(context.Context, string, string, string) (*models.Connection, error) {
	return s.conn, nil
}
func (s *webhookTestStore) FindConnectionForNangoEvent(context.Context, string, string) (*models.Connection, error) {
	if s.findErr {
		return nil, context.Canceled
	}
	return s.conn, nil
}
func (s *webhookTestStore) CreateIntegrationEventReceipt(_ context.Context, r *models.IntegrationEventReceipt) (bool, error) {
	if s.receipts == nil {
		s.receipts = map[string]string{}
	}
	if _, ok := s.receipts[r.DeduplicationKey]; ok {
		return false, nil
	}
	if r.ID == "" {
		r.ID = "receipt-1"
	}
	s.receipts[r.DeduplicationKey] = r.ID
	return true, nil
}
func (s *webhookTestStore) DeletePendingIntegrationEventReceipt(_ context.Context, id string) error {
	s.deleted = append(s.deleted, id)
	for key, value := range s.receipts {
		if value == id {
			delete(s.receipts, key)
		}
	}
	return nil
}

type webhookTestQueue struct {
	jobs []redisstore.IntegrationEventJob
	err  error
}

func (q *webhookTestQueue) EnqueueIntegrationEvent(_ context.Context, j redisstore.IntegrationEventJob) error {
	if q.err != nil {
		return q.err
	}
	q.jobs = append(q.jobs, j)
	return nil
}

func syncWebhookRequest(body string) *http.Request {
	return httptest.NewRequest(http.MethodPost, "/v1/webhooks/nango", strings.NewReader(body))
}

func TestVerifyNangoWebhook(t *testing.T) {
	body := []byte(`{"type":"auth","success":true,"connectionId":"abc","providerConfigKey":"pck","tags":{"end_user_id":"tenant-1"}}`)
	webhookSecret := "test-webhook-signing-key"
	apiSecret := "test-api-secret-key"

	hmacFor := func(secret string) string {
		mac := hmac.New(sha256.New, []byte(secret))
		mac.Write(body)
		return hex.EncodeToString(mac.Sum(nil))
	}
	legacyFor := func(secret string) string {
		combined := append([]byte(secret), body...)
		sum := sha256.Sum256(combined)
		return hex.EncodeToString(sum[:])
	}

	tests := []struct {
		name          string
		webhookSecret string
		apiSecret     string
		headers       map[string]string
		want          bool
	}{
		{
			name:          "nango hmac header with api secret",
			webhookSecret: webhookSecret,
			apiSecret:     apiSecret,
			headers:       map[string]string{"X-Nango-Hmac-Sha256": hmacFor(apiSecret)},
			want:          true,
		},
		{
			name:          "legacy signature header with api secret",
			webhookSecret: webhookSecret,
			apiSecret:     apiSecret,
			headers:       map[string]string{"X-Nango-Signature": legacyFor(apiSecret)},
			want:          true,
		},
		{
			name:          "hmac on signature header with webhook signing key",
			webhookSecret: webhookSecret,
			apiSecret:     apiSecret,
			headers:       map[string]string{"X-Nango-Signature": hmacFor(webhookSecret)},
			want:          true,
		},
		{
			name:          "wrong secret",
			webhookSecret: webhookSecret,
			apiSecret:     apiSecret,
			headers:       map[string]string{"X-Nango-Hmac-Sha256": hmacFor("wrong")},
			want:          false,
		},
		{
			name:          "no headers",
			webhookSecret: webhookSecret,
			apiSecret:     apiSecret,
			headers:       map[string]string{},
			want:          false,
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			hdr := make(http.Header)
			for k, v := range tc.headers {
				hdr.Set(k, v)
			}
			got := verifyNangoWebhook(body, tc.webhookSecret, tc.apiSecret, hdr)
			if got != tc.want {
				t.Fatalf("verifyNangoWebhook() = %v, want %v", got, tc.want)
			}
		})
	}
}

func TestNangoSyncWebhook_PersistsAndEnqueues(t *testing.T) {
	store := &webhookTestStore{conn: &models.Connection{ID: "conn-1", TenantID: "tenant-1"}}
	queue := &webhookTestQueue{}
	h := NewNangoWebhookHandler(store, nil, "", "", zap.NewNop()).WithIntegrationEvents(queue)
	rec := httptest.NewRecorder()
	h.HandleNangoEvent(rec, syncWebhookRequest(`{"type":"sync.records.changed","id":"event-1","connectionId":"nango-1","providerConfigKey":"cfg","model":"Case","modifiedAfter":"2026-08-03T10:00:00Z","added":1}`))
	if rec.Code != http.StatusAccepted {
		t.Fatalf("status=%d body=%s", rec.Code, rec.Body.String())
	}
	if len(queue.jobs) != 1 || queue.jobs[0].ReceiptID != "receipt-1" {
		t.Fatalf("jobs=%v", queue.jobs)
	}
	if len(store.receipts) != 1 {
		t.Fatalf("receipts=%d", len(store.receipts))
	}
}

func TestNangoSyncWebhook_DeduplicatesCanonicalJSON(t *testing.T) {
	store := &webhookTestStore{conn: &models.Connection{ID: "conn-1", TenantID: "tenant-1"}}
	queue := &webhookTestQueue{}
	h := NewNangoWebhookHandler(store, nil, "", "", zap.NewNop()).WithIntegrationEvents(queue)
	bodies := []string{`{"type":"sync","connectionId":"nango-1","providerConfigKey":"cfg","model":"Case","modifiedAfter":"2026-08-03T10:00:00Z","counts":{"added":1}}`, `{
 "counts":{"added":1}, "modifiedAfter":"2026-08-03T10:00:00Z", "model":"Case", "providerConfigKey":"cfg", "connectionId":"nango-1", "type":"sync"}`}
	for i, body := range bodies {
		rec := httptest.NewRecorder()
		h.HandleNangoEvent(rec, syncWebhookRequest(body))
		want := http.StatusAccepted
		if i == 1 {
			want = http.StatusOK
		}
		if rec.Code != want {
			t.Fatalf("request %d status=%d body=%s", i, rec.Code, rec.Body.String())
		}
	}
	if len(queue.jobs) != 1 {
		t.Fatalf("jobs=%d, want 1", len(queue.jobs))
	}
}

func TestNangoSyncWebhook_EnqueueFailureRollsBackReceipt(t *testing.T) {
	store := &webhookTestStore{conn: &models.Connection{ID: "conn-1", TenantID: "tenant-1"}}
	queue := &webhookTestQueue{err: errors.New("redis down")}
	h := NewNangoWebhookHandler(store, nil, "", "", zap.NewNop()).WithIntegrationEvents(queue)
	rec := httptest.NewRecorder()
	h.HandleNangoEvent(rec, syncWebhookRequest(`{"type":"sync","connectionId":"nango-1","providerConfigKey":"cfg","model":"Case","modifiedAfter":"2026-08-03T10:00:00Z"}`))
	if rec.Code != http.StatusServiceUnavailable {
		t.Fatalf("status=%d", rec.Code)
	}
	if len(store.deleted) != 1 || len(store.receipts) != 0 {
		t.Fatalf("deleted=%v receipts=%v", store.deleted, store.receipts)
	}
}

func TestNangoSyncWebhook_UnknownConnectionIgnored(t *testing.T) {
	store := &webhookTestStore{findErr: true}
	queue := &webhookTestQueue{}
	h := NewNangoWebhookHandler(store, nil, "", "", zap.NewNop()).WithIntegrationEvents(queue)
	rec := httptest.NewRecorder()
	h.HandleNangoEvent(rec, syncWebhookRequest(`{"type":"sync","connectionId":"unknown","providerConfigKey":"cfg","model":"Case","modifiedAfter":"2026-08-03T10:00:00Z"}`))
	if rec.Code != http.StatusOK {
		t.Fatalf("status=%d", rec.Code)
	}
	if len(queue.jobs) != 0 {
		t.Fatalf("jobs=%d", len(queue.jobs))
	}
}

func TestNangoWebhook_PayloadLimit(t *testing.T) {
	h := NewNangoWebhookHandler(&webhookTestStore{}, nil, "", "", zap.NewNop())
	rec := httptest.NewRecorder()
	req := httptest.NewRequest(http.MethodPost, "/v1/webhooks/nango", bytes.NewReader(bytes.Repeat([]byte("x"), (1<<20)+1)))
	h.HandleNangoEvent(rec, req)
	if rec.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("status=%d, want 413", rec.Code)
	}
}

func TestNangoWebhookPayloadTenantID(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{
			name: "tags end_user_id",
			raw:  `{"tags":{"end_user_id":"tenant-from-tags"}}`,
			want: "tenant-from-tags",
		},
		{
			name: "endUser endUserId",
			raw:  `{"endUser":{"endUserId":"tenant-from-end-user"}}`,
			want: "tenant-from-end-user",
		},
		{
			name: "legacy endUser id",
			raw:  `{"endUser":{"id":"tenant-legacy"}}`,
			want: "tenant-legacy",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var p nangoWebhookPayload
			if err := json.Unmarshal([]byte(tc.raw), &p); err != nil {
				t.Fatalf("unmarshal: %v", err)
			}
			if got := p.tenantID(); got != tc.want {
				t.Fatalf("tenantID() = %q, want %q", got, tc.want)
			}
		})
	}
}
