//go:build linux

package isolation

import "errors"

// BuildScriptNetworkSpec is an assertion consumed by the Linux runner: scripts
// get no network namespace connectivity; the controlled fetcher runs separately.
type BuildScriptNetworkSpec struct {
	NetworkDisabled bool
	AllowDNS        bool
	AllowLoopback   bool
}

func ValidateBuildScriptNetwork(s BuildScriptNetworkSpec) error {
	if !s.NetworkDisabled || s.AllowDNS || s.AllowLoopback {
		return errors.New("bootstrap script network must be fully disabled")
	}
	return nil
}
