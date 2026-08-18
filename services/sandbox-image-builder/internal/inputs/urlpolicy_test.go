package inputs

import (
	"net"
	"testing"
)

func TestRejectsUnsafeExternalInputs(t *testing.T) {
	for _, u := range []string{"http://example.com/a", "https://user@example.com/a", "https://127.0.0.1/a", "https://example.com/a#fragment"} {
		if ValidateExternalInput(ExternalInput{URL: u, SHA256: "aaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaaa", Size: 1}) == nil {
			t.Fatalf("accepted %s", u)
		}
	}
	if ValidateResolvedAddresses([]net.IP{net.ParseIP("127.0.0.1")}) == nil {
		t.Fatal("accepted loopback")
	}
	if ValidateResolvedAddresses([]net.IP{net.ParseIP("10.0.0.1")}) == nil {
		t.Fatal("accepted private")
	}
}
