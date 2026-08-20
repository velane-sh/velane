// Package inputs validates credential-free external image build inputs.
package inputs

import (
	"errors"
	"fmt"
	"net"
	"net/netip"
	"net/url"
	"strings"
)

var ErrUnsafeURL = errors.New("external input URL is unsafe")

type ExternalInput struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}

func ValidateExternalInput(in ExternalInput) error {
	if in.Size <= 0 || len(in.SHA256) != 64 {
		return fmt.Errorf("%w: digest and exact size are required", ErrUnsafeURL)
	}
	u, e := url.Parse(in.URL)
	if e != nil || u.Scheme != "https" || u.Host == "" || u.User != nil || u.Fragment != "" || u.RawQuery != "" {
		return ErrUnsafeURL
	}
	if strings.Contains(u.Host, "%") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".") {
		return ErrUnsafeURL
	}
	if _, e := netip.ParseAddr(u.Hostname()); e == nil {
		return ErrUnsafeURL
	}
	return nil
}

// ValidateResolvedAddresses rejects a connection if any DNS response is unsafe;
// callers must resolve immediately before each dial and pin the chosen address.
func ValidateResolvedAddresses(addresses []net.IP) error {
	if len(addresses) == 0 {
		return ErrUnsafeURL
	}
	for _, ip := range addresses {
		if !isPublic(ip) {
			return fmt.Errorf("%w: non-public resolver result", ErrUnsafeURL)
		}
	}
	return nil
}
func isPublic(ip net.IP) bool {
	a, ok := netip.AddrFromSlice(ip)
	if !ok {
		return false
	}
	a = a.Unmap()
	if !a.IsGlobalUnicast() {
		return false
	}
	if a.Is4() {
		return !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !a.IsUnspecified() && !a.IsMulticast()
	}
	return !a.IsPrivate() && !a.IsLoopback() && !a.IsLinkLocalUnicast() && !a.IsUnspecified() && !a.IsMulticast()
}
