//go:build privileged

package network

import (
	"context"
	"os/exec"
	"testing"
)

// Run only on an isolated root-capable Linux security gate. This proves that a
// real nftables table can be installed; ordinary unit tests use a recording
// runner and cannot claim privileged network fencing succeeded.
func TestPrivilegedNFTablesAvailable(t *testing.T) {
	if _, err := exec.LookPath("nft"); err != nil {
		t.Fatal(err)
	}
	n := NFTables{Table: "velane_watchdog_test"}
	if err := n.DefaultDeny(context.Background(), "sandbox_test"); err != nil {
		t.Fatal(err)
	}
	if err := n.Remove(context.Background(), "sandbox_test"); err != nil {
		t.Fatal(err)
	}
}
