package snapshot

import "fmt"

type Candidate struct {
	LineageID                  string
	HostCompatibilityKey       string
	VMRestoreDescriptorDigest  string
	ObservedCapabilitiesDigest string
}

// ValidateRestoreCandidate must be called before download and again directly
// before Firecracker LoadSnapshot. It deliberately offers no fallback result.
func ValidateRestoreCandidate(m SnapshotManifestV1, candidate Candidate) error {
	if candidate.LineageID != m.LineageID {
		return fmt.Errorf("snapshot incompatible: lineage mismatch")
	}
	if candidate.HostCompatibilityKey != m.SourceHostCompatibilityKey {
		return fmt.Errorf("snapshot incompatible: source host key mismatch")
	}
	if candidate.VMRestoreDescriptorDigest != m.VMRestoreDescriptorDigest {
		return fmt.Errorf("snapshot incompatible: VM restore descriptor mismatch")
	}
	if candidate.ObservedCapabilitiesDigest == "" {
		return fmt.Errorf("snapshot incompatible: observed capability proof missing")
	}
	return nil
}
