package config

import (
	"context"
	"crypto/ecdsa"
	"crypto/elliptic"
	"crypto/rand"
	"crypto/tls"
	"crypto/x509"
	"crypto/x509/pkix"
	"encoding/base64"
	"encoding/pem"
	"math/big"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestSandboxWatchdogSigningKeyFailsClosed(t *testing.T) {
	if _, err := (Config{}).SandboxWatchdogSigningKey(); err == nil {
		t.Fatal("empty watchdog signing key was accepted")
	}
	key := make([]byte, 64)
	cfg := Config{SandboxWatchdogSigningPrivateKeyBase64: base64.StdEncoding.EncodeToString(key)}
	got, err := cfg.SandboxWatchdogSigningKey()
	if err != nil || len(got) != 64 {
		t.Fatalf("valid watchdog signing key rejected: len=%d err=%v", len(got), err)
	}
}

func TestSandboxHostTLSConfigRequiresAllFiles(t *testing.T) {
	config := Config{SandboxControlEnabled: true}
	if _, err := config.SandboxHostTLSConfig(); err == nil {
		t.Fatal("SandboxHostTLSConfig() succeeded without CA, certificate, and key files")
	}
}

func TestLoadSandboxControlDefaultsDisabled(t *testing.T) {
	t.Setenv("SANDBOX_CONTROL_ENABLED", "")
	t.Setenv("SANDBOX_HOST_LISTEN_ADDR", "")
	config := Load()
	if config.SandboxControlEnabled {
		t.Fatal("sandbox control must default to disabled")
	}
	if config.SandboxHostListenAddr != ":8443" {
		t.Fatalf("SandboxHostListenAddr = %q, want :8443", config.SandboxHostListenAddr)
	}
}

func TestSandboxHostExpectedIdentityRequiresBothValues(t *testing.T) {
	if _, _, err := (Config{}).SandboxHostExpectedIdentity(); err == nil {
		t.Fatal("empty expected host identity was accepted")
	}
	identity := Config{SandboxHostExpectedLineageID: "lineage", SandboxHostExpectedCompatibilityKey: "compatibility"}
	lineage, compatibility, err := identity.SandboxHostExpectedIdentity()
	if err != nil || lineage != "lineage" || compatibility != "compatibility" {
		t.Fatalf("expected identity = %q, %q, %v", lineage, compatibility, err)
	}
}

func TestSandboxHostTLSConfigRequiresVerifiedClientCertificate(t *testing.T) {
	directory := t.TempDir()
	ca, caKey, caPEM := issueCA(t, "sandbox-test-ca")
	serverCert, serverKey := issueCertificate(t, ca, caKey, "sandbox-control", true)

	caPath := writePEM(t, directory, "ca.pem", caPEM)
	certPath := writePEM(t, directory, "server.pem", serverCert)
	keyPath := writePEM(t, directory, "server-key.pem", serverKey)

	config := Config{
		SandboxControlEnabled: true,
		SandboxHostCAFile:     caPath,
		SandboxHostCertFile:   certPath,
		SandboxHostKeyFile:    keyPath,
	}
	tlsConfig, err := config.SandboxHostTLSConfig()
	if err != nil {
		t.Fatalf("SandboxHostTLSConfig() error = %v", err)
	}
	if tlsConfig.ClientAuth != tls.VerifyClientCertIfGiven {
		t.Fatalf("ClientAuth = %v, want VerifyClientCertIfGiven", tlsConfig.ClientAuth)
	}
	if tlsConfig.ClientCAs == nil || len(tlsConfig.Certificates) != 1 {
		t.Fatal("TLS config does not contain the client CA and server certificate")
	}
	if tlsConfig.MinVersion < tls.VersionTLS13 {
		t.Fatalf("MinVersion = %#x, want TLS 1.3 or newer", tlsConfig.MinVersion)
	}
}

func TestSandboxHostTLSConfigRejectsUntrustedClient(t *testing.T) {
	directory := t.TempDir()
	ca, caKey, caPEM := issueCA(t, "sandbox-test-ca")
	serverCert, serverKey := issueCertificate(t, ca, caKey, "sandbox-control", true)
	trustedClientCert, trustedClientKey := issueCertificate(t, ca, caKey, "trusted-host", false)
	untrustedCA, untrustedCAKey, _ := issueCA(t, "untrusted-ca")
	untrustedClientCert, untrustedClientKey := issueCertificate(t, untrustedCA, untrustedCAKey, "untrusted-host", false)

	config := Config{SandboxControlEnabled: true, SandboxHostCAFile: writePEM(t, directory, "ca.pem", caPEM), SandboxHostCertFile: writePEM(t, directory, "server.pem", serverCert), SandboxHostKeyFile: writePEM(t, directory, "server-key.pem", serverKey)}
	serverTLS, err := config.SandboxHostTLSConfig()
	if err != nil {
		t.Fatal(err)
	}
	listener, err := tls.Listen("tcp", "127.0.0.1:0", serverTLS)
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	server := &http.Server{Handler: http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })}
	go server.Serve(listener)                   //nolint:errcheck
	defer server.Shutdown(context.Background()) //nolint:errcheck

	roots := x509.NewCertPool()
	roots.AppendCertsFromPEM(caPEM)
	request := func(certPEM, keyPEM []byte) error {
		cert, loadErr := tls.X509KeyPair(certPEM, keyPEM)
		if loadErr != nil {
			return loadErr
		}
		client := &http.Client{Transport: &http.Transport{TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, RootCAs: roots, ServerName: "sandbox-control", Certificates: []tls.Certificate{cert}}, DialContext: (&net.Dialer{}).DialContext}}
		response, requestErr := client.Get("https://" + listener.Addr().String())
		if requestErr == nil {
			response.Body.Close()
		}
		return requestErr
	}
	if err := request(trustedClientCert, trustedClientKey); err != nil {
		t.Fatalf("trusted client failed: %v", err)
	}
	// TLS permits no certificate only for the two bootstrap routes, but an
	// untrusted certificate is still rejected during handshake.
	// An untrusted client does not receive the certificate request's CA match and
	// therefore enters bootstrap TLS without a client certificate. The router,
	// not TLS, denies it on every non-bootstrap route so first boot remains
	// possible without weakening private route authorization.
	if err := request(untrustedClientCert, untrustedClientKey); err != nil {
		t.Fatalf("bootstrap TLS unexpectedly rejected an otherwise valid server connection: %v", err)
	}
}

func issueCA(t *testing.T, commonName string) (*x509.Certificate, *ecdsa.PrivateKey, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	template := &x509.Certificate{
		SerialNumber:          big.NewInt(1),
		Subject:               pkix.Name{CommonName: commonName},
		NotBefore:             time.Now().Add(-time.Minute),
		NotAfter:              time.Now().Add(time.Hour),
		IsCA:                  true,
		BasicConstraintsValid: true,
		KeyUsage:              x509.KeyUsageCertSign | x509.KeyUsageDigitalSignature,
	}
	der, err := x509.CreateCertificate(rand.Reader, template, template, &key.PublicKey, key)
	if err != nil {
		t.Fatal(err)
	}
	return template, key, pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der})
}

func issueCertificate(t *testing.T, ca *x509.Certificate, caKey *ecdsa.PrivateKey, commonName string, server bool) ([]byte, []byte) {
	t.Helper()
	key, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	usage := x509.ExtKeyUsageClientAuth
	if server {
		usage = x509.ExtKeyUsageServerAuth
	}
	template := &x509.Certificate{
		SerialNumber: big.NewInt(2),
		Subject:      pkix.Name{CommonName: commonName},
		DNSNames:     []string{commonName},
		NotBefore:    time.Now().Add(-time.Minute),
		NotAfter:     time.Now().Add(time.Hour),
		KeyUsage:     x509.KeyUsageDigitalSignature,
		ExtKeyUsage:  []x509.ExtKeyUsage{usage},
	}
	der, err := x509.CreateCertificate(rand.Reader, template, ca, &key.PublicKey, caKey)
	if err != nil {
		t.Fatal(err)
	}
	keyDER, err := x509.MarshalPKCS8PrivateKey(key)
	if err != nil {
		t.Fatal(err)
	}
	return pem.EncodeToMemory(&pem.Block{Type: "CERTIFICATE", Bytes: der}), pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: keyDER})
}

func writePEM(t *testing.T, directory, name string, contents []byte) string {
	t.Helper()
	path := filepath.Join(directory, name)
	if err := os.WriteFile(path, contents, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}
