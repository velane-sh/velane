package inputs

import (
	"context"
	"crypto/sha256"
	"crypto/tls"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/netip"
	"net/url"
	"strings"
	"time"
)

type Response interface {
	io.Reader
	Close() error
}
type Fetcher interface {
	Fetch(context.Context, ExternalInput) (Response, error)
}

// FetchExact accepts only the declared byte count, so an upstream cannot make a
// build retain an unbounded response even when its Content-Length is false.
func FetchExact(ctx context.Context, f Fetcher, input ExternalInput, max int64) ([]byte, error) {
	if err := ValidateExternalInput(input); err != nil {
		return nil, err
	}
	if input.Size > max {
		return nil, errors.New("declared input exceeds service limit")
	}
	r, err := f.Fetch(ctx, input)
	if err != nil {
		return nil, err
	}
	defer r.Close()
	data, err := io.ReadAll(io.LimitReader(r, input.Size+1))
	if err != nil {
		return nil, err
	}
	if int64(len(data)) != input.Size {
		return nil, errors.New("input byte size mismatch")
	}
	d := sha256.Sum256(data)
	if hex.EncodeToString(d[:]) != input.SHA256 {
		return nil, errors.New("input checksum mismatch")
	}
	return data, nil
}

type Resolver interface {
	LookupIPAddr(context.Context, string) ([]net.IPAddr, error)
}
type SafeHTTPFetcher struct {
	Resolver      Resolver
	ClientTimeout time.Duration
	DenyPrefixes  []netip.Prefix
}

func (f SafeHTTPFetcher) Fetch(ctx context.Context, input ExternalInput) (Response, error) {
	if err := ValidateExternalInput(input); err != nil {
		return nil, err
	}
	u, err := url.Parse(input.URL)
	if err != nil {
		return nil, err
	}
	resolver := f.Resolver
	if resolver == nil {
		resolver = net.DefaultResolver
	}
	answers, err := resolver.LookupIPAddr(ctx, u.Hostname())
	if err != nil {
		return nil, fmt.Errorf("resolve external input: %w", err)
	}
	ips := make([]net.IP, 0, len(answers))
	for _, answer := range answers {
		ips = append(ips, answer.IP)
	}
	if err := validateAddresses(ips, f.DenyPrefixes); err != nil {
		return nil, err
	}
	// Resolve and validate immediately before the individual connection and pin
	// the selected public address; net/http cannot silently re-resolve it.
	chosen := ips[0]
	hostPort := net.JoinHostPort(chosen.String(), "443")
	dialer := &net.Dialer{Timeout: 10 * time.Second}
	transport := &http.Transport{Proxy: nil, ForceAttemptHTTP2: false, DisableCompression: true, DialContext: func(callCtx context.Context, network, address string) (net.Conn, error) {
		fresh, err := resolver.LookupIPAddr(callCtx, u.Hostname())
		if err != nil {
			return nil, err
		}
		freshIPs := make([]net.IP, 0, len(fresh))
		for _, a := range fresh {
			freshIPs = append(freshIPs, a.IP)
		}
		if err := validateAddresses(freshIPs, f.DenyPrefixes); err != nil {
			return nil, err
		}
		found := false
		for _, ip := range freshIPs {
			if ip.Equal(chosen) {
				found = true
				break
			}
		}
		if !found {
			return nil, errors.New("validated address changed before dial")
		}
		return dialer.DialContext(callCtx, network, hostPort)
	}, TLSClientConfig: &tls.Config{MinVersion: tls.VersionTLS13, ServerName: u.Hostname()}, TLSHandshakeTimeout: 10 * time.Second, ResponseHeaderTimeout: 15 * time.Second}
	client := &http.Client{Transport: transport, Timeout: f.timeout(), CheckRedirect: func(*http.Request, []*http.Request) error {
		return errors.New("redirects are forbidden for external inputs")
	}}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "velane-image-builder/1")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		resp.Body.Close()
		return nil, fmt.Errorf("external input returned HTTP %d", resp.StatusCode)
	}
	if resp.ContentLength > input.Size {
		resp.Body.Close()
		return nil, errors.New("input response exceeds declared size")
	}
	return resp.Body, nil
}
func (f SafeHTTPFetcher) timeout() time.Duration {
	if f.ClientTimeout > 0 {
		return f.ClientTimeout
	}
	return 30 * time.Second
}
func validateAddresses(addresses []net.IP, deny []netip.Prefix) error {
	if len(addresses) == 0 {
		return ErrUnsafeURL
	}
	for _, ip := range addresses {
		if !isPublicFetch(ip) {
			return fmt.Errorf("%w: non-public resolver result", ErrUnsafeURL)
		}
		a, _ := netip.AddrFromSlice(ip)
		a = a.Unmap()
		for _, p := range append(defaultDeniedPrefixes(), deny...) {
			if p.Contains(a) {
				return fmt.Errorf("%w: denied resolver result", ErrUnsafeURL)
			}
		}
	}
	return nil
}
func defaultDeniedPrefixes() []netip.Prefix {
	out := []string{"100.64.0.0/10", "169.254.0.0/16", "198.18.0.0/15", "127.0.0.0/8", "0.0.0.0/8", "10.0.0.0/8", "172.16.0.0/12", "192.168.0.0/16", "::1/128", "::/128", "fc00::/7", "fe80::/10"}
	p := make([]netip.Prefix, 0, len(out))
	for _, v := range out {
		p = append(p, netip.MustParsePrefix(v))
	}
	return p
}
func isPublicFetch(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() || a.IsPrivate() || a.IsLoopback() || a.IsLinkLocalUnicast() || a.IsUnspecified() || a.IsMulticast() {
		return false
	}
	return !strings.EqualFold(a.String(), "169.254.169.254")
}
