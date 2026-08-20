package inputs

import (
	"context"
	"net"
	"testing"
)

type staticResolver struct{ ips []net.IPAddr }

func (s staticResolver) LookupIPAddr(context.Context, string) ([]net.IPAddr, error) {
	return s.ips, nil
}

func TestSafeFetcherRejectsPrivateResolverAnswers(t *testing.T) {
	f := SafeHTTPFetcher{Resolver: staticResolver{[]net.IPAddr{{IP: net.ParseIP("100.64.1.1")}}}}
	_, err := f.Fetch(context.Background(), ExternalInput{URL: "https://example.com/a", SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1})
	if err == nil {
		t.Fatal("accepted CGNAT answer")
	}
}

func TestValidateAddressesRejectsMetadataAndAllowsPublic(t *testing.T) {
	if err := validateAddresses([]net.IP{net.ParseIP("169.254.169.254")}, nil); err == nil {
		t.Fatal("accepted metadata address")
	}
	if err := validateAddresses([]net.IP{net.ParseIP("93.184.216.34")}, nil); err != nil {
		t.Fatalf("rejected public address: %v", err)
	}
}
