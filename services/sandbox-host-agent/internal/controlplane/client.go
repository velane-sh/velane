// Package controlplane implements the outbound-only mTLS host-control client.
package controlplane

import (
	"bytes"
	"context"
	"crypto"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// PresignedSnapshotUploader uses only a one-time scoped URL. No S3 provider
// credentials are configured on a sandbox host.
type PresignedSnapshotUploader struct{ HTTPClient *http.Client }

func (u PresignedSnapshotUploader) Put(ctx context.Context, target SnapshotUploadPlanChunk, body io.Reader, size int64) (SnapshotCompletedUpload, error) {
	if target.URL == "" || target.ObjectRef == "" || target.UploadID == "" || target.PartNumber < 1 || target.ChecksumSHA256 == "" || size <= 0 {
		return SnapshotCompletedUpload{}, fmt.Errorf("invalid scoped snapshot upload descriptor")
	}
	client := u.HTTPClient
	if client == nil {
		client = http.DefaultClient
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPut, target.URL, io.LimitReader(body, size))
	if err != nil {
		return SnapshotCompletedUpload{}, err
	}
	req.Header.Set("Content-Length", strconv.FormatInt(size, 10))
	req.Header.Set("x-amz-checksum-sha256", target.ChecksumSHA256)
	resp, err := client.Do(req)
	if err != nil {
		return SnapshotCompletedUpload{}, fmt.Errorf("upload snapshot part: %w", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return SnapshotCompletedUpload{}, fmt.Errorf("upload snapshot part returned %s", resp.Status)
	}
	etag := strings.Trim(resp.Header.Get("ETag"), `"`)
	if etag == "" {
		return SnapshotCompletedUpload{}, fmt.Errorf("upload snapshot part did not return an ETag")
	}
	return SnapshotCompletedUpload{ObjectRef: target.ObjectRef, UploadID: target.UploadID, PartNumber: target.PartNumber, ETag: etag, ChecksumSHA256: target.ChecksumSHA256}, nil
}

type Client struct {
	base      *url.URL
	http      *http.Client
	tlsConfig *tls.Config
	certFile  string
	keyFile   string
}
type TLSConfig struct{ BaseURL, ServerName, CAFile, CertFile, KeyFile string }

type ExistingIdentity struct {
	PoolID, HostID string
	Incarnation    uint64
	ExpiresAt      time.Time
}

func NewClient(c TLSConfig) (*Client, error) {
	base, err := url.Parse(c.BaseURL)
	if err != nil || base.Scheme != "https" || base.Host == "" {
		return nil, fmt.Errorf("invalid HTTPS host-control URL")
	}
	caPEM, err := os.ReadFile(c.CAFile)
	if err != nil {
		return nil, err
	}
	roots := x509.NewCertPool()
	if !roots.AppendCertsFromPEM(caPEM) {
		return nil, fmt.Errorf("host-control CA file contains no certificate")
	}
	tlsConfig := &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: c.ServerName}
	cert, err := tls.LoadX509KeyPair(c.CertFile, c.KeyFile)
	if err == nil {
		if len(cert.Certificate) == 0 {
			return nil, fmt.Errorf("host client certificate chain is empty")
		}
		leaf, parseErr := x509.ParseCertificate(cert.Certificate[0])
		if parseErr != nil {
			return nil, parseErr
		}
		// An expired client certificate would make even the bootstrap enrollment
		// handshake fail. Omit it so a host that was offline through expiry can
		// re-attest and receive a replacement certificate using its existing key.
		if leaf.NotAfter.After(time.Now()) {
			tlsConfig.Certificates = []tls.Certificate{cert}
		}
	} else if !os.IsNotExist(err) {
		return nil, err
	}
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = nil
	transport.TLSClientConfig = tlsConfig
	return &Client{base: base, http: &http.Client{Transport: transport, Timeout: 35 * time.Second}, tlsConfig: tlsConfig, certFile: c.CertFile, keyFile: c.KeyFile}, nil
}

// GenerateCSR creates the host private key locally if needed. The control
// plane receives only this CSR; it never receives host key material.
func GenerateCSR(keyFile string) (string, error) {
	if keyFile == "" {
		return "", fmt.Errorf("host key path is required")
	}
	var signer crypto.Signer
	keyPEM, err := os.ReadFile(keyFile)
	if err == nil {
		block, _ := pem.Decode(keyPEM)
		if block == nil {
			return "", fmt.Errorf("host key is not PEM")
		}
		parsed, parseErr := x509.ParsePKCS8PrivateKey(block.Bytes)
		if parseErr != nil {
			return "", parseErr
		}
		var ok bool
		signer, ok = parsed.(crypto.Signer)
		if !ok {
			return "", fmt.Errorf("host key cannot sign a CSR")
		}
	} else if os.IsNotExist(err) {
		key, generateErr := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
		if generateErr != nil {
			return "", generateErr
		}
		signer = key
		keyDER, marshalErr := x509.MarshalPKCS8PrivateKey(key)
		if marshalErr != nil {
			return "", marshalErr
		}
		if mkdirErr := os.MkdirAll(filepath.Dir(keyFile), 0o700); mkdirErr != nil {
			return "", mkdirErr
		}
		if writeErr := os.WriteFile(keyFile, pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER}), 0o600); writeErr != nil {
			return "", writeErr
		}
	} else {
		return "", err
	}
	csr, err := x509.CreateCertificateRequest(rand.Reader, &x509.CertificateRequest{}, signer)
	if err != nil {
		return "", err
	}
	return string(pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE REQUEST", Bytes: csr})), nil
}

func (c *Client) ExistingIdentity() (ExistingIdentity, bool, error) {
	if len(c.tlsConfig.Certificates) == 0 || len(c.tlsConfig.Certificates[0].Certificate) == 0 {
		return ExistingIdentity{}, false, nil
	}
	certificate, err := x509.ParseCertificate(c.tlsConfig.Certificates[0].Certificate[0])
	if err != nil {
		return ExistingIdentity{}, false, err
	}
	if len(certificate.URIs) != 1 {
		return ExistingIdentity{}, false, fmt.Errorf("host certificate must contain exactly one URI SAN")
	}
	uri := certificate.URIs[0]
	if uri.Scheme != "spiffe" || uri.Host != "velane" {
		return ExistingIdentity{}, false, fmt.Errorf("host certificate URI SAN is invalid")
	}
	parts := strings.Split(strings.TrimPrefix(uri.Path, "/sandbox-hosts/"), "/")
	if len(parts) != 3 || parts[0] == "" || parts[1] == "" {
		return ExistingIdentity{}, false, fmt.Errorf("host certificate identity is incomplete")
	}
	incarnation, err := strconv.ParseUint(parts[2], 10, 64)
	if err != nil || incarnation == 0 {
		return ExistingIdentity{}, false, fmt.Errorf("host certificate incarnation is invalid")
	}
	return ExistingIdentity{PoolID: parts[0], HostID: parts[1], Incarnation: incarnation, ExpiresAt: certificate.NotAfter}, true, nil
}

// InstallIdentity atomically persists the issued certificate and starts using
// it for subsequent mTLS requests in this process.
func (c *Client) InstallIdentity(certificatePEM string) error {
	if c.certFile == "" || certificatePEM == "" {
		return fmt.Errorf("host certificate is required")
	}
	if err := os.MkdirAll(filepath.Dir(c.certFile), 0o700); err != nil {
		return err
	}
	temporary := c.certFile + ".tmp"
	if err := os.WriteFile(temporary, []byte(certificatePEM), 0o600); err != nil {
		return err
	}
	if err := os.Rename(temporary, c.certFile); err != nil {
		return err
	}
	certificate, err := tls.LoadX509KeyPair(c.certFile, c.keyFile)
	if err != nil {
		return err
	}
	c.tlsConfig.Certificates = []tls.Certificate{certificate}
	return nil
}
func (c *Client) call(ctx context.Context, method, p string, in, out any) error {
	relative, err := url.Parse(p)
	if err != nil || relative.IsAbs() {
		return fmt.Errorf("invalid host-control relative path")
	}
	u := *c.base
	u.Path = path.Join(c.base.Path, relative.Path)
	u.RawQuery = relative.RawQuery
	var body io.Reader
	if in != nil {
		b, err := json.Marshal(in)
		if err != nil {
			return err
		}
		body = bytes.NewReader(b)
	}
	req, err := http.NewRequestWithContext(ctx, method, u.String(), body)
	if err != nil {
		return err
	}
	req.Header.Set("Accept", "application/json")
	if in != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := c.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode > 299 {
		limited, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("host-control %s %s: %s", method, p, string(limited))
	}
	if out != nil {
		return json.NewDecoder(io.LimitReader(resp.Body, 8<<20)).Decode(out)
	}
	return nil
}

type EnrollmentChallengeRequest struct {
	PoolID   string `json:"pool_id"`
	Provider string `json:"provider"`
	CSR      string `json:"csr"`
}
type EnrollmentChallenge struct {
	ChallengeID string `json:"challenge_id"`
	Nonce       string `json:"nonce"`
}
type RegisterRequest struct {
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
type RegisterResponse struct {
	HostID               string    `json:"host_id"`
	Incarnation          uint64    `json:"incarnation"`
	CertificatePEM       string    `json:"certificate_pem"`
	CertificateExpiresAt time.Time `json:"certificate_expires_at"`
}
type HeartbeatRequest struct {
	Sequence        uint64                   `json:"sequence"`
	WatchdogHealthy bool                     `json:"watchdog_healthy"`
	SentAt          time.Time                `json:"sent_at"`
	Allocations     []AllocationLeaseRenewal `json:"allocations"`
}
type AllocationLeaseRenewal struct {
	AllocationID    string `json:"allocation_id"`
	LeaseToken      string `json:"lease_token"`
	HostIncarnation uint64 `json:"host_incarnation"`
	FenceEpoch      uint64 `json:"fence_epoch"`
}
type HeartbeatResponse struct {
	WatchdogGrants []json.RawMessage `json:"watchdog_grants"`
}
type Command struct {
	ID              string          `json:"id"`
	Kind            string          `json:"kind"`
	AllocationID    string          `json:"allocation_id"`
	HostIncarnation uint64          `json:"host_incarnation"`
	FenceEpoch      uint64          `json:"fence_epoch"`
	Sequence        uint64          `json:"sequence"`
	Payload         json.RawMessage `json:"payload"`
	LeaseToken      string          `json:"lease_token"`
	WatchdogGrant   json.RawMessage `json:"watchdog_grant"`
}
type CommandResult struct {
	AllocationID    string          `json:"allocation_id"`
	HostIncarnation uint64          `json:"host_incarnation"`
	FenceEpoch      uint64          `json:"fence_epoch"`
	CommandID       string          `json:"command_id"`
	Succeeded       bool            `json:"succeeded"`
	FailureCode     string          `json:"failure_code,omitempty"`
	FailureMessage  string          `json:"failure_message,omitempty"`
	Result          json.RawMessage `json:"result,omitempty"`
}
type SnapshotUploadPlanRequest struct {
	AllocationID    string          `json:"allocation_id"`
	HostIncarnation uint64          `json:"host_incarnation"`
	FenceEpoch      uint64          `json:"fence_epoch"`
	Manifest        json.RawMessage `json:"manifest"`
}
type SnapshotUploadPlan struct {
	SnapshotID string                       `json:"snapshot_id"`
	State      string                       `json:"state"`
	Artifacts  []SnapshotUploadPlanArtifact `json:"artifacts"`
}
type SnapshotUploadPlanArtifact struct {
	Type    string                    `json:"type"`
	DriveID string                    `json:"drive_id,omitempty"`
	Chunks  []SnapshotUploadPlanChunk `json:"chunks"`
}
type SnapshotUploadPlanChunk struct {
	Index          int    `json:"index"`
	ObjectRef      string `json:"object_ref"`
	UploadID       string `json:"upload_id"`
	PartNumber     int    `json:"part_number"`
	URL            string `json:"url"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}
type SnapshotCompleteRequest struct {
	AllocationID     string                    `json:"allocation_id"`
	HostIncarnation  uint64                    `json:"host_incarnation"`
	FenceEpoch       uint64                    `json:"fence_epoch"`
	Manifest         json.RawMessage           `json:"manifest"`
	CompletedUploads []SnapshotCompletedUpload `json:"completed_uploads"`
}
type SnapshotCompletedUpload struct {
	ObjectRef      string `json:"object_ref"`
	UploadID       string `json:"upload_id"`
	PartNumber     int    `json:"part_number"`
	ETag           string `json:"etag"`
	ChecksumSHA256 string `json:"checksum_sha256"`
}

func (c *Client) EnrollmentChallenge(ctx context.Context, poolID, provider, csr string) (EnrollmentChallenge, error) {
	var out EnrollmentChallenge
	return out, c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/enrollment-challenges", EnrollmentChallengeRequest{PoolID: poolID, Provider: provider, CSR: csr}, &out)
}
func (c *Client) Register(ctx context.Context, r RegisterRequest) (RegisterResponse, error) {
	var out RegisterResponse
	return out, c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/register", r, &out)
}
func (c *Client) Enroll(ctx context.Context, r RegisterRequest) (RegisterResponse, error) {
	return c.Register(ctx, r)
}
func (c *Client) Renew(ctx context.Context, hostID, csr string) (RegisterResponse, error) {
	var out RegisterResponse
	return out, c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/"+hostID+"/renew", map[string]string{"csr": csr}, &out)
}
func (c *Client) Heartbeat(ctx context.Context, hostID string, r HeartbeatRequest) (HeartbeatResponse, error) {
	var out HeartbeatResponse
	return out, c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/"+hostID+"/heartbeat", r, &out)
}
func (c *Client) Poll(ctx context.Context, hostID string, after uint64) ([]Command, error) {
	var out []Command
	return out, c.call(ctx, http.MethodGet, fmt.Sprintf("/internal/v1/sandbox-hosts/%s/commands?after=%d", hostID, after), nil, &out)
}
func (c *Client) Ack(ctx context.Context, hostID, id string) error {
	return c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/"+hostID+"/commands/"+id+"/ack", map[string]bool{"acknowledged": true}, nil)
}
func (c *Client) Result(ctx context.Context, hostID, id string, r CommandResult) error {
	return c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/"+hostID+"/commands/"+id+"/result", r, nil)
}
func (c *Client) SnapshotUploadPlan(ctx context.Context, hostID, snapshotID string, r SnapshotUploadPlanRequest) (SnapshotUploadPlan, error) {
	var out SnapshotUploadPlan
	err := c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/"+hostID+"/snapshots/"+snapshotID+"/upload-plan", r, &out)
	return out, err
}
func (c *Client) CompleteSnapshot(ctx context.Context, hostID, snapshotID string, r SnapshotCompleteRequest) error {
	return c.call(ctx, http.MethodPost, "/internal/v1/sandbox-hosts/"+hostID+"/snapshots/"+snapshotID+"/complete", r, nil)
}
