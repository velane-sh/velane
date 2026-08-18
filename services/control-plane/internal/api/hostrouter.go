package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/hostidentity"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	"github.com/go-chi/chi/v5"
)

const sandboxHostIdentityPrefix = "spiffe://velane/sandbox-hosts/"

// NewHostRouter is deliberately separate from the public router. It is mounted
// only on the private mTLS listener and derives every authorization decision
// from the verified client-certificate URI SAN.
// EnrollmentService keeps provider attestation separate from the private host
// protocol. It makes the AWS verifier replaceable by Azure/GCP implementations.
type EnrollmentService interface {
	Challenge(context.Context, EnrollmentChallengeInput) (EnrollmentChallenge, error)
	Enroll(context.Context, EnrollmentInput) (EnrollmentResult, error)
	Renew(context.Context, postgres.SandboxHostIdentity, string) (EnrollmentResult, error)
}
type EnrollmentChallengeInput struct{ PoolID, Provider, CSR string }
type EnrollmentChallenge struct{ ID, Nonce string }
type EnrollmentInput struct {
	EnrollmentChallengeID, Nonce, Provider, CSR, BootID, AgentVersion, ProtocolVersion string
	Proof                                                                              hostidentity.EnrollmentProof
}
type EnrollmentResult struct {
	HostID               string
	Incarnation          int64
	CertificatePEM       string
	CertificateExpiresAt time.Time
}

// NewHostRouter is deliberately separate from the public router. Passing a nil
// enrollment service intentionally disables bootstrap rather than weakening it.
func NewHostRouter(store *postgres.Store, enrollment ...EnrollmentService) *chi.Mux {
	r := chi.NewRouter()
	var service EnrollmentService
	if len(enrollment) == 1 {
		service = enrollment[0]
	}
	h := hostProtocolHandler{store: store, enrollment: service}
	r.Post("/internal/v1/sandbox-hosts/enrollment-challenges", h.enrollmentChallenge)
	r.Post("/internal/v1/sandbox-hosts/register", h.register)
	r.Post("/internal/v1/sandbox-hosts/{hostID}/renew", h.renew)
	r.Post("/internal/v1/sandbox-hosts/{hostID}/heartbeat", h.heartbeat)
	r.Get("/internal/v1/sandbox-hosts/{hostID}/commands", h.commands)
	r.Post("/internal/v1/sandbox-hosts/{hostID}/commands/{commandID}/ack", h.ack)
	r.Post("/internal/v1/sandbox-hosts/{hostID}/commands/{commandID}/result", h.result)
	r.Post("/internal/v1/sandbox-hosts/{hostID}/snapshots/{snapshotID}/upload-plan", h.uploadPlan)
	r.Post("/internal/v1/sandbox-hosts/{hostID}/snapshots/{snapshotID}/complete", h.completeSnapshot)
	return r
}

type hostProtocolHandler struct {
	store      *postgres.Store
	enrollment EnrollmentService
}

type hostIdentity struct {
	postgres.SandboxHostIdentity
}

func hostIdentityFromRequest(r *http.Request) (hostIdentity, error) {
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		return hostIdentity{}, errors.New("verified host client certificate is required")
	}
	certificate := r.TLS.PeerCertificates[0]
	if len(certificate.URIs) != 1 {
		return hostIdentity{}, errors.New("host certificate must contain exactly one URI SAN")
	}
	uri := certificate.URIs[0]
	if uri.Scheme != "spiffe" || uri.Host != "velane" || !strings.HasPrefix(uri.String(), sandboxHostIdentityPrefix) {
		return hostIdentity{}, errors.New("host certificate URI SAN is invalid")
	}
	parts := strings.Split(strings.TrimPrefix(uri.Path, "/sandbox-hosts/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return hostIdentity{}, errors.New("host certificate URI SAN lacks pool, host, or incarnation")
	}
	incarnation, err := strconv.ParseInt(parts[2], 10, 64)
	if err != nil || incarnation < 1 {
		return hostIdentity{}, errors.New("host certificate incarnation is invalid")
	}
	return hostIdentity{postgres.SandboxHostIdentity{PoolID: parts[0], HostID: parts[1], Incarnation: incarnation}}, nil
}

func (h hostProtocolHandler) identity(w http.ResponseWriter, r *http.Request) (hostIdentity, bool) {
	identity, err := hostIdentityFromRequest(r)
	if err != nil {
		writeHostError(w, http.StatusUnauthorized, err.Error())
		return hostIdentity{}, false
	}
	if pathHostID := chi.URLParam(r, "hostID"); pathHostID != "" && pathHostID != identity.HostID {
		writeHostError(w, http.StatusForbidden, "host certificate does not authorize this host")
		return hostIdentity{}, false
	}
	if h.store != nil && r.TLS.PeerCertificates[0].SerialNumber != nil {
		if err := h.store.HostCertificateActive(r.Context(), identity.SandboxHostIdentity, r.TLS.PeerCertificates[0].SerialNumber.Text(16)); err != nil {
			writeHostError(w, http.StatusUnauthorized, "host certificate is stale, revoked, or expired")
			return hostIdentity{}, false
		}
	}
	return identity, true
}

func (h hostProtocolHandler) enrollmentChallenge(w http.ResponseWriter, r *http.Request) {
	if h.enrollment == nil {
		writeHostError(w, http.StatusServiceUnavailable, "host enrollment is not configured")
		return
	}
	var request struct {
		PoolID   string `json:"pool_id"`
		Provider string `json:"provider"`
		CSR      string `json:"csr"`
	}
	if !decodeHostJSON(w, r, &request) {
		return
	}
	challenge, err := h.enrollment.Challenge(r.Context(), EnrollmentChallengeInput{PoolID: request.PoolID, Provider: request.Provider, CSR: request.CSR})
	if err != nil {
		writeHostError(w, http.StatusBadRequest, "enrollment challenge rejected")
		return
	}
	writeHostJSON(w, http.StatusOK, map[string]string{"challenge_id": challenge.ID, "nonce": challenge.Nonce})
}

type hostRegisterRequest struct {
	PoolID               string          `json:"pool_id"`
	BootID               string          `json:"boot_id"`
	HostCompatibilityKey string          `json:"host_compatibility_key"`
	CommandCapabilities  map[string]bool `json:"command_capabilities"`
	TotalVCPU            int             `json:"total_vcpu"`
	TotalMemoryMB        int             `json:"total_memory_mb"`
	TotalDiskBytes       int64           `json:"total_disk_bytes"`
	TotalStagingBytes    int64           `json:"total_staging_bytes"`
	ProtocolVersion      string          `json:"protocol_version"`
	AgentVersion         string          `json:"agent_version"`
	ChallengeID          string          `json:"challenge_id"`
	Nonce                string          `json:"nonce"`
	Provider             string          `json:"provider"`
	CSR                  string          `json:"csr"`
	IdentityDocument     string          `json:"identity_document"`
	IdentitySignature    string          `json:"identity_signature"`
	STSSignedRequest     string          `json:"sts_signed_request"`
}

func (h hostProtocolHandler) register(w http.ResponseWriter, r *http.Request) {
	// Bootstrap has no client certificate; its only authority is the persisted,
	// one-use provider-attested challenge. Once a certificate is issued the same
	// route becomes the mTLS-only inventory registration step.
	if r.TLS == nil || len(r.TLS.PeerCertificates) == 0 {
		h.enroll(w, r)
		return
	}
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	var request hostRegisterRequest
	if !decodeHostJSON(w, r, &request) {
		return
	}
	if request.PoolID != identity.PoolID || request.BootID == "" || request.ProtocolVersion == "" || request.AgentVersion == "" {
		writeHostError(w, http.StatusBadRequest, "invalid authenticated host registration")
		return
	}
	if request.Provider != "" || request.CSR != "" || request.ChallengeID != "" || request.Nonce != "" || request.IdentityDocument != "" || request.IdentitySignature != "" || request.STSSignedRequest != "" {
		writeHostError(w, http.StatusBadRequest, "authenticated registration must not contain bootstrap enrollment evidence")
		return
	}
	incarnation, err := h.store.RegisterSandboxHost(r.Context(), identity.SandboxHostIdentity, postgres.SandboxHostRegistration{PoolID: request.PoolID, BootID: request.BootID, HostCompatibilityKey: request.HostCompatibilityKey, VMRestoreCapabilities: request.CommandCapabilities, TotalVCPU: request.TotalVCPU, TotalMemoryMB: request.TotalMemoryMB, TotalDiskBytes: request.TotalDiskBytes, TotalStagingBytes: request.TotalStagingBytes, AgentVersion: request.AgentVersion, ProtocolVersion: request.ProtocolVersion})
	if err != nil {
		writeHostStoreError(w, err)
		return
	}
	writeHostJSON(w, http.StatusOK, map[string]any{"host_id": identity.HostID, "incarnation": incarnation})
}

func (h hostProtocolHandler) enroll(w http.ResponseWriter, r *http.Request) {
	if h.enrollment == nil {
		writeHostError(w, http.StatusServiceUnavailable, "host enrollment is not configured")
		return
	}
	var request hostRegisterRequest
	if !decodeHostJSON(w, r, &request) {
		return
	}
	registered, err := h.enrollment.Enroll(r.Context(), EnrollmentInput{EnrollmentChallengeID: request.ChallengeID, Nonce: request.Nonce, Provider: request.Provider, CSR: request.CSR, BootID: request.BootID, AgentVersion: request.AgentVersion, ProtocolVersion: request.ProtocolVersion, Proof: hostidentity.EnrollmentProof{Provider: request.Provider, PoolID: request.PoolID, Nonce: request.Nonce, CSR: request.CSR, IdentityDocument: request.IdentityDocument, IdentitySignature: request.IdentitySignature, STSSignedRequest: request.STSSignedRequest}})
	if err != nil {
		writeHostError(w, http.StatusForbidden, "host enrollment rejected")
		return
	}
	writeHostJSON(w, http.StatusOK, map[string]any{"host_id": registered.HostID, "incarnation": registered.Incarnation, "certificate_pem": registered.CertificatePEM, "certificate_expires_at": registered.CertificateExpiresAt})
}

func (h hostProtocolHandler) renew(w http.ResponseWriter, r *http.Request) {
	if h.enrollment == nil {
		writeHostError(w, http.StatusServiceUnavailable, "host enrollment is not configured")
		return
	}
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	var request struct {
		CSR string `json:"csr"`
	}
	if !decodeHostJSON(w, r, &request) {
		return
	}
	renewed, err := h.enrollment.Renew(r.Context(), identity.SandboxHostIdentity, request.CSR)
	if err != nil {
		writeHostError(w, http.StatusForbidden, "host certificate renewal rejected")
		return
	}
	writeHostJSON(w, http.StatusOK, map[string]any{"certificate_pem": renewed.CertificatePEM, "certificate_expires_at": renewed.CertificateExpiresAt})
}

type hostHeartbeatRequest struct {
	Sequence        int64                        `json:"sequence"`
	WatchdogHealthy bool                         `json:"watchdog_healthy"`
	SentAt          time.Time                    `json:"sent_at"`
	Allocations     []hostAllocationLeaseRenewal `json:"allocations"`
}
type hostAllocationLeaseRenewal struct {
	AllocationID    string `json:"allocation_id"`
	LeaseToken      string `json:"lease_token"`
	HostIncarnation int64  `json:"host_incarnation"`
	FenceEpoch      int64  `json:"fence_epoch"`
}

func (h hostProtocolHandler) heartbeat(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	var request hostHeartbeatRequest
	if !decodeHostJSON(w, r, &request) {
		return
	}
	if err := h.store.HeartbeatSandboxHost(r.Context(), identity.SandboxHostIdentity, request.Sequence, request.WatchdogHealthy); err != nil {
		writeHostStoreError(w, err)
		return
	}
	grants := make([]json.RawMessage, 0, len(request.Allocations))
	for _, allocation := range request.Allocations {
		expiresAt, err := h.store.RenewSandboxAllocationLease(r.Context(), identity.SandboxHostIdentity, postgres.AllocationLeaseRenewal{AllocationID: allocation.AllocationID, LeaseToken: allocation.LeaseToken, HostIncarnation: allocation.HostIncarnation, FenceEpoch: allocation.FenceEpoch})
		if err != nil {
			writeHostStoreError(w, err)
			return
		}
		grant, err := h.store.WatchdogLeaseGrant(r.Context(), identity.SandboxHostIdentity, allocation.AllocationID, allocation.FenceEpoch, expiresAt)
		if err != nil {
			writeHostStoreError(w, err)
			return
		}
		grants = append(grants, grant)
	}
	writeHostJSON(w, http.StatusOK, map[string]any{"watchdog_grants": grants})
}

func (h hostProtocolHandler) commands(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	after, err := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if err != nil || after < 0 {
		writeHostError(w, http.StatusBadRequest, "after must be a non-negative sequence")
		return
	}
	wait := 25 * time.Second
	if raw := r.URL.Query().Get("wait"); raw != "" {
		wait, err = time.ParseDuration(raw)
		if err != nil || wait < 0 || wait > 25*time.Second {
			writeHostError(w, http.StatusBadRequest, "wait must be between 0s and 25s")
			return
		}
	}
	deadline := time.NewTimer(wait)
	defer deadline.Stop()
	for {
		commands, queryErr := h.store.PollSandboxHostCommands(r.Context(), identity.SandboxHostIdentity, after)
		if queryErr != nil {
			writeHostStoreError(w, queryErr)
			return
		}
		if len(commands) > 0 || wait == 0 {
			writeHostJSON(w, http.StatusOK, commands)
			return
		}
		select {
		case <-r.Context().Done():
			return
		case <-deadline.C:
			writeHostJSON(w, http.StatusOK, []postgres.SandboxHostCommand{})
			return
		case <-time.After(200 * time.Millisecond):
		}
	}
}

func (h hostProtocolHandler) ack(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	if err := h.store.AckSandboxHostCommand(r.Context(), identity.SandboxHostIdentity, chi.URLParam(r, "commandID")); err != nil {
		writeHostStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

type hostCommandResultRequest struct {
	AllocationID    string          `json:"allocation_id"`
	HostIncarnation int64           `json:"host_incarnation"`
	FenceEpoch      int64           `json:"fence_epoch"`
	CommandID       string          `json:"command_id"`
	Succeeded       bool            `json:"succeeded"`
	FailureCode     string          `json:"failure_code"`
	FailureMessage  string          `json:"failure_message"`
	Result          json.RawMessage `json:"result"`
}

func (h hostProtocolHandler) result(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	var request hostCommandResultRequest
	if !decodeHostJSON(w, r, &request) {
		return
	}
	if request.CommandID != chi.URLParam(r, "commandID") {
		writeHostError(w, http.StatusBadRequest, "command_id must match the route")
		return
	}
	err := h.store.CompleteSandboxHostCommand(r.Context(), identity.SandboxHostIdentity, postgres.SandboxHostCommandResult{AllocationID: request.AllocationID, HostIncarnation: request.HostIncarnation, FenceEpoch: request.FenceEpoch, CommandID: request.CommandID, Succeeded: request.Succeeded, FailureCode: request.FailureCode, FailureMessage: request.FailureMessage, Result: request.Result})
	if err != nil {
		writeHostStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func (h hostProtocolHandler) uploadPlan(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	var request struct {
		AllocationID    string          `json:"allocation_id"`
		HostIncarnation int64           `json:"host_incarnation"`
		FenceEpoch      int64           `json:"fence_epoch"`
		Manifest        json.RawMessage `json:"manifest"`
	}
	if !decodeHostJSON(w, r, &request) {
		return
	}
	plan, err := h.store.SandboxSnapshotUploadPlan(r.Context(), identity.SandboxHostIdentity, chi.URLParam(r, "snapshotID"), request.AllocationID, request.HostIncarnation, request.FenceEpoch, request.Manifest)
	if err != nil {
		writeHostStoreError(w, err)
		return
	}
	writeHostJSON(w, http.StatusOK, plan)
}

type hostSnapshotCompleteRequest struct {
	AllocationID     string                             `json:"allocation_id"`
	HostIncarnation  int64                              `json:"host_incarnation"`
	FenceEpoch       int64                              `json:"fence_epoch"`
	Manifest         json.RawMessage                    `json:"manifest"`
	CompletedUploads []postgres.SnapshotCompletedUpload `json:"completed_uploads"`
}

func (h hostProtocolHandler) completeSnapshot(w http.ResponseWriter, r *http.Request) {
	identity, ok := h.identity(w, r)
	if !ok {
		return
	}
	var request hostSnapshotCompleteRequest
	if !decodeHostJSON(w, r, &request) {
		return
	}
	if err := h.store.CompleteSandboxSnapshot(r.Context(), identity.SandboxHostIdentity, chi.URLParam(r, "snapshotID"), request.AllocationID, request.HostIncarnation, request.FenceEpoch, request.Manifest, request.CompletedUploads); err != nil {
		writeHostStoreError(w, err)
		return
	}
	w.WriteHeader(http.StatusNoContent)
}

func decodeHostJSON(w http.ResponseWriter, r *http.Request, target any) bool {
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, 1<<20))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(target); err != nil {
		writeHostError(w, http.StatusBadRequest, "invalid JSON request")
		return false
	}
	return true
}
func writeHostJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func writeHostError(w http.ResponseWriter, status int, message string) {
	writeHostJSON(w, status, map[string]string{"error": message})
}
func writeHostStoreError(w http.ResponseWriter, err error) {
	if errors.Is(err, postgres.ErrNotFound) {
		writeHostError(w, http.StatusNotFound, "host resource not found")
		return
	}
	if strings.Contains(err.Error(), "stale") || strings.Contains(err.Error(), "identity") || strings.Contains(err.Error(), "compatibility") || strings.Contains(err.Error(), "heartbeat") {
		writeHostError(w, http.StatusConflict, "stale host identity or fence")
		return
	}
	writeHostError(w, http.StatusBadRequest, fmt.Sprintf("host protocol rejected request: %s", err.Error()))
}

func notImplementedHost(w http.ResponseWriter, _ *http.Request) {
	writeHostError(w, http.StatusNotImplemented, "sandbox host endpoint is not configured")
}
