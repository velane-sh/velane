// Package recipe validates the canonical immutable sandbox image recipe contract.
package recipe

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"

	"github.com/abskrj/velane/services/sandbox-image-builder/internal/inputs"
)

type Package struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}

type InstallGroup struct {
	RepositorySnapshot string    `json:"repository_snapshot"`
	IndexDigest        string    `json:"index_digest"`
	LockDigest         string    `json:"lock_digest"`
	Packages           []Package `json:"packages"`
}

type Bootstrap struct {
	Script         string `json:"script"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}

// SpecV1 deliberately mirrors control-plane/domain.RecipeSpecV1. Keep the
// golden JSON fixtures in both modules byte-identical; these modules cannot
// import one another because they are independently deployable binaries.
type SpecV1 struct {
	SchemaVersion     string                 `json:"schema_version"`
	Platform          string                 `json:"platform"`
	Architecture      string                 `json:"architecture"`
	BaseImage         string                 `json:"base_image"`
	Environment       map[string]string      `json:"environment,omitempty"`
	InstallGroups     []InstallGroup         `json:"install_groups,omitempty"`
	ProfileVersionIDs []string               `json:"profile_version_ids"`
	Bootstrap         *Bootstrap             `json:"bootstrap,omitempty"`
	ExternalInputs    []inputs.ExternalInput `json:"external_inputs,omitempty"`
	GuestProtocol     string                 `json:"guest_protocol"`
}

var (
	exactVersion = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+:~_-]*$`)
	sha256Hex    = regexp.MustCompile(`^[a-f0-9]{64}$`)
	pinnedBase   = regexp.MustCompile(`^.+@sha256:[a-f0-9]{64}$`)
)

func Validate(spec SpecV1) error {
	if spec.SchemaVersion != "1" || spec.Platform != "linux" || spec.Architecture == "" {
		return errors.New("recipe requires schema v1, Linux platform, and architecture")
	}
	if !pinnedBase.MatchString(spec.BaseImage) {
		return errors.New("recipe requires a digest-pinned base_image")
	}
	if strings.TrimSpace(spec.GuestProtocol) == "" {
		return errors.New("recipe requires a guest protocol")
	}
	if len(spec.ProfileVersionIDs) == 0 {
		return errors.New("recipe requires immutable profile targets")
	}
	seenProfiles := map[string]struct{}{}
	for _, id := range spec.ProfileVersionIDs {
		if id == "" {
			return errors.New("recipe profile target is empty")
		}
		if _, exists := seenProfiles[id]; exists {
			return errors.New("recipe profile targets must be unique")
		}
		seenProfiles[id] = struct{}{}
	}
	for k := range spec.Environment {
		if strings.HasPrefix(strings.ToUpper(k), "VELANE_") {
			return fmt.Errorf("reserved environment variable %q", k)
		}
	}
	for _, g := range spec.InstallGroups {
		if g.RepositorySnapshot == "" || !sha256Hex.MatchString(g.IndexDigest) || !sha256Hex.MatchString(g.LockDigest) || len(g.Packages) == 0 {
			return errors.New("install group lacks immutable repository metadata or lock")
		}
		for _, p := range g.Packages {
			if p.Name == "" || !exactVersion.MatchString(p.Version) || !sha256Hex.MatchString(p.Digest) {
				return fmt.Errorf("package %q is not exactly pinned", p.Name)
			}
		}
	}
	if b := spec.Bootstrap; b != nil {
		if len(b.Script) == 0 || len(b.Script) > 64*1024 || b.TimeoutSeconds < 1 || b.TimeoutSeconds > 600 {
			return errors.New("invalid bounded bootstrap")
		}
	}
	for _, in := range spec.ExternalInputs {
		if err := inputs.ValidateExternalInput(in); err != nil {
			return err
		}
	}
	return nil
}

func Decode(data []byte) (SpecV1, error) {
	var spec SpecV1
	decoder := json.NewDecoder(bytes.NewReader(data))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&spec); err != nil {
		return SpecV1{}, err
	}
	var trailing any
	if err := decoder.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return SpecV1{}, errors.New("only one recipe document is allowed")
		}
		return SpecV1{}, err
	}
	return spec, Validate(spec)
}
