package api

import (
	"crypto/tls"
	"crypto/x509"
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"
)

func hostRequest(t *testing.T, method, path, uri string) *http.Request {
	t.Helper()
	req := httptest.NewRequest(method, path, nil)
	parsed, err := url.Parse(uri)
	if err != nil {
		t.Fatal(err)
	}
	req.TLS = &tls.ConnectionState{PeerCertificates: []*x509.Certificate{{URIs: []*url.URL{parsed}}}}
	return req
}

func TestHostRouterBindsRouteHostToCertificateSAN(t *testing.T) {
	router := NewHostRouter(nil)
	request := hostRequest(t, http.MethodPost, "/internal/v1/sandbox-hosts/other/heartbeat", "spiffe://velane/sandbox-hosts/pool-a/host-a/2")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusForbidden {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusForbidden)
	}
}

func TestHostRouterRejectsMalformedHostCertificateSAN(t *testing.T) {
	router := NewHostRouter(nil)
	request := hostRequest(t, http.MethodGet, "/internal/v1/sandbox-hosts/host-a/commands?after=not-a-number", "spiffe://velane/sandbox-hosts/pool-a/host-a/not-an-incarnation")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusUnauthorized {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusUnauthorized)
	}
}

func TestHostRouterValidatesLongPollBeforeStoreAccess(t *testing.T) {
	router := NewHostRouter(nil)
	request := hostRequest(t, http.MethodGet, "/internal/v1/sandbox-hosts/host-a/commands?after=-1", "spiffe://velane/sandbox-hosts/pool-a/host-a/2")
	recorder := httptest.NewRecorder()
	router.ServeHTTP(recorder, request)
	if recorder.Code != http.StatusBadRequest {
		t.Fatalf("status = %d, want %d", recorder.Code, http.StatusBadRequest)
	}
}
