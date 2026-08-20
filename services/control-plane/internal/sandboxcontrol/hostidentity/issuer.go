package hostidentity

import (
	"crypto"
	"crypto/ecdsa"
	"crypto/rand"
	"crypto/rsa"
	"crypto/x509"
	"encoding/pem"
	"fmt"
	"math/big"
	"net/url"
	"os"
	"path"
	"time"
)

const HostIdentityPrefix = "spiffe://velane/sandbox-hosts/"

// CertificateIssuer issues short-lived client certificates from an
// installation-local CA. The host private key stays on the host: only a CSR is
// ever accepted here.
type CertificateIssuer struct {
	certificate *x509.Certificate
	key         crypto.Signer
	ttl         time.Duration
	now         func() time.Time
}

func NewCertificateIssuer(certificateFile, keyFile string, ttl time.Duration) (*CertificateIssuer, error) {
	if ttl <= 0 || ttl > 24*time.Hour {
		return nil, fmt.Errorf("host certificate lifetime must be between zero and 24h")
	}
	certificatePEM, err := os.ReadFile(certificateFile)
	if err != nil {
		return nil, fmt.Errorf("read host signing CA: %w", err)
	}
	block, _ := pem.Decode(certificatePEM)
	if block == nil {
		return nil, fmt.Errorf("parse host signing CA: no PEM certificate")
	}
	certificate, err := x509.ParseCertificate(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse host signing CA: %w", err)
	}
	if !certificate.IsCA || certificate.KeyUsage&x509.KeyUsageCertSign == 0 {
		return nil, fmt.Errorf("host signing certificate is not a certificate authority")
	}
	keyPEM, err := os.ReadFile(keyFile)
	if err != nil {
		return nil, fmt.Errorf("read host signing key: %w", err)
	}
	keyBlock, _ := pem.Decode(keyPEM)
	if keyBlock == nil {
		return nil, fmt.Errorf("parse host signing key: no PEM private key")
	}
	parsed, err := x509.ParsePKCS8PrivateKey(keyBlock.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse host signing key: %w", err)
	}
	key, ok := parsed.(crypto.Signer)
	if !ok {
		return nil, fmt.Errorf("host signing key cannot sign certificates")
	}
	return &CertificateIssuer{certificate: certificate, key: key, ttl: ttl, now: time.Now}, nil
}

func (i *CertificateIssuer) Issue(csrPEM, poolID, hostID string, incarnation int64) ([]byte, time.Time, string, error) {
	if poolID == "" || hostID == "" || incarnation < 1 {
		return nil, time.Time{}, "", fmt.Errorf("invalid host certificate identity")
	}
	block, _ := pem.Decode([]byte(csrPEM))
	if block == nil {
		return nil, time.Time{}, "", fmt.Errorf("parse host CSR: no PEM request")
	}
	csr, err := x509.ParseCertificateRequest(block.Bytes)
	if err != nil || csr.CheckSignature() != nil {
		return nil, time.Time{}, "", fmt.Errorf("parse host CSR: invalid signature")
	}
	if err := validateCSRKey(csr.PublicKey); err != nil {
		return nil, time.Time{}, "", err
	}
	identity := path.Join("/sandbox-hosts", poolID, hostID, fmt.Sprintf("%d", incarnation))
	uri, err := url.Parse("spiffe://velane" + identity)
	if err != nil {
		return nil, time.Time{}, "", err
	}
	now := i.now().UTC()
	expiresAt := now.Add(i.ttl)
	serialLimit := new(big.Int).Lsh(big.NewInt(1), 128)
	serial, err := rand.Int(rand.Reader, serialLimit)
	if err != nil {
		return nil, time.Time{}, "", fmt.Errorf("generate certificate serial: %w", err)
	}
	certificateDER, err := x509.CreateCertificate(rand.Reader, &x509.Certificate{
		SerialNumber: serial, Subject: csr.Subject, NotBefore: now.Add(-time.Minute), NotAfter: expiresAt,
		KeyUsage:    x509.KeyUsageDigitalSignature | x509.KeyUsageKeyEncipherment,
		ExtKeyUsage: []x509.ExtKeyUsage{x509.ExtKeyUsageClientAuth}, URIs: []*url.URL{uri},
	}, i.certificate, csr.PublicKey, i.key)
	if err != nil {
		return nil, time.Time{}, "", fmt.Errorf("sign host certificate: %w", err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: certificateDER}), expiresAt, serial.Text(16), nil
}

func validateCSRKey(key any) error {
	switch key := key.(type) {
	case *rsa.PublicKey:
		if key.Size() < 256 {
			return fmt.Errorf("host CSR RSA key is too small")
		}
	case *ecdsa.PublicKey:
		if key.Curve.Params().BitSize < 256 {
			return fmt.Errorf("host CSR EC key is too small")
		}
	default:
		return fmt.Errorf("host CSR uses an unsupported key type")
	}
	return nil
}
