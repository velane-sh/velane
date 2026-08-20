package api

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/ids"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/hostidentity"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
)

// HostEnrollmentManager coordinates durable challenge consumption, provider
// evidence, and certificate issuance. It does not know any cloud-specific
// evidence format; the Verifier boundary is intentionally provider-neutral.
type HostEnrollmentManager struct {
	Store        *postgres.Store
	Verifier     hostidentity.Verifier
	Issuer       *hostidentity.CertificateIssuer
	ChallengeTTL time.Duration
}

func (m HostEnrollmentManager) Challenge(ctx context.Context, in EnrollmentChallengeInput) (EnrollmentChallenge, error) {
	if m.Store == nil || m.Verifier == nil || m.Issuer == nil || in.PoolID == "" || in.Provider == "" || in.CSR == "" {
		return EnrollmentChallenge{}, fmt.Errorf("host enrollment is not configured")
	}
	if m.ChallengeTTL <= 0 || m.ChallengeTTL > 10*time.Minute {
		m.ChallengeTTL = 5 * time.Minute
	}
	nonceBytes := make([]byte, 32)
	if _, err := rand.Read(nonceBytes); err != nil {
		return EnrollmentChallenge{}, fmt.Errorf("generate enrollment nonce: %w", err)
	}
	nonce := hex.EncodeToString(nonceBytes)
	challenge := EnrollmentChallenge{ID: ids.New(), Nonce: nonce}
	if err := m.Store.CreateSandboxHostEnrollmentChallenge(ctx, challenge.ID, sha256Hex(nonce), in.PoolID, sha256Hex(in.CSR), in.Provider, time.Now().Add(m.ChallengeTTL)); err != nil {
		return EnrollmentChallenge{}, err
	}
	return challenge, nil
}

func (m HostEnrollmentManager) Enroll(ctx context.Context, in EnrollmentInput) (EnrollmentResult, error) {
	if m.Store == nil || m.Verifier == nil || m.Issuer == nil || in.EnrollmentChallengeID == "" || in.Nonce == "" || in.CSR == "" || in.BootID == "" {
		return EnrollmentResult{}, fmt.Errorf("incomplete enrollment")
	}
	if in.Proof.Provider != in.Provider || in.Proof.Nonce != in.Nonce || in.Proof.CSR != in.CSR {
		return EnrollmentResult{}, fmt.Errorf("enrollment evidence is not challenge-bound")
	}
	verified, err := m.Verifier.Verify(ctx, in.Proof)
	if err != nil {
		return EnrollmentResult{}, err
	}
	// The final certificate serial is generated before persistence, so an
	// enrollment cannot leave a usable "pending" credential after a crash.
	certificate, expiresAt, serial, err := m.Issuer.Issue(in.CSR, verified.PoolID, "pending", 1)
	if err != nil {
		return EnrollmentResult{}, err
	}
	host, err := m.Store.CreateSandboxHostEnrollment(ctx, in.EnrollmentChallengeID, sha256Hex(in.Nonce), sha256Hex(in.CSR), in.BootID, in.AgentVersion, in.ProtocolVersion, verified, serial, expiresAt)
	if err != nil {
		return EnrollmentResult{}, err
	}
	certificate, expiresAt, serial, err = m.Issuer.Issue(in.CSR, verified.PoolID, host.HostID, host.Incarnation)
	if err != nil {
		return EnrollmentResult{}, err
	}
	if err := m.Store.RenewSandboxHostCertificate(ctx, postgres.SandboxHostIdentity{PoolID: verified.PoolID, HostID: host.HostID, Incarnation: host.Incarnation}, serial, expiresAt); err != nil {
		return EnrollmentResult{}, err
	}
	return EnrollmentResult{HostID: host.HostID, Incarnation: host.Incarnation, CertificatePEM: string(certificate), CertificateExpiresAt: expiresAt}, nil
}

func (m HostEnrollmentManager) Renew(ctx context.Context, identity postgres.SandboxHostIdentity, csr string) (EnrollmentResult, error) {
	if m.Store == nil || m.Issuer == nil || csr == "" {
		return EnrollmentResult{}, fmt.Errorf("incomplete renewal")
	}
	certificate, expiresAt, serial, err := m.Issuer.Issue(csr, identity.PoolID, identity.HostID, identity.Incarnation)
	if err != nil {
		return EnrollmentResult{}, err
	}
	if err := m.Store.RenewSandboxHostCertificate(ctx, identity, serial, expiresAt); err != nil {
		return EnrollmentResult{}, err
	}
	return EnrollmentResult{HostID: identity.HostID, Incarnation: identity.Incarnation, CertificatePEM: string(certificate), CertificateExpiresAt: expiresAt}, nil
}

func sha256Hex(value string) string { return fmt.Sprintf("%x", sha256.Sum256([]byte(value))) }
