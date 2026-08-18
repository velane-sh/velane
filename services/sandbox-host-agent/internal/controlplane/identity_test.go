package controlplane

import (
	"os"
	"path/filepath"
	"testing"
)

func TestGenerateCSRReusesExistingPrivateKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "identity", "client.key")
	first, err := GenerateCSR(path)
	if err != nil {
		t.Fatal(err)
	}
	keyBefore, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	second, err := GenerateCSR(path)
	if err != nil {
		t.Fatal(err)
	}
	keyAfter, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if first == "" || second == "" || string(keyBefore) != string(keyAfter) {
		t.Fatal("CSR generation replaced the enrolled host private key")
	}
}
