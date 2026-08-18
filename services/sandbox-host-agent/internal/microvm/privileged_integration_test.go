//go:build privileged

package microvm

import (
	"os"
	"testing"
)

// This gate intentionally never emulates KVM. CI runs it only on a dedicated
// Linux host image with Firecracker and jailer installed by the image recipe.
func TestPrivilegedHostPrerequisites(t *testing.T) {
	for _, path := range []string{"/dev/kvm", "/sys/fs/cgroup/cgroup.controllers"} {
		if _, err := os.Stat(path); err != nil {
			t.Fatalf("required production host prerequisite %s: %v", path, err)
		}
	}
}
