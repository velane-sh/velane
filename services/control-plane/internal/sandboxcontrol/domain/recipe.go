package domain

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/url"
	"regexp"
	"strings"
)

var (
	recipeSHA256Hex  = regexp.MustCompile(`^[a-f0-9]{64}$`)
	recipeExact      = regexp.MustCompile(`^[0-9A-Za-z][0-9A-Za-z.+:~_-]*$`)
	recipePinnedBase = regexp.MustCompile(`^.+@sha256:[a-f0-9]{64}$`)
)

type ExternalInputSpec struct {
	URL    string `json:"url"`
	SHA256 string `json:"sha256"`
	Size   int64  `json:"size"`
}
type PackageSpec struct {
	Name    string `json:"name"`
	Version string `json:"version"`
	Digest  string `json:"digest"`
}
type InstallGroup struct {
	RepositorySnapshot string        `json:"repository_snapshot"`
	IndexDigest        string        `json:"index_digest"`
	LockDigest         string        `json:"lock_digest"`
	Packages           []PackageSpec `json:"packages"`
}
type BootstrapSpec struct {
	Script         string `json:"script"`
	TimeoutSeconds int    `json:"timeout_seconds"`
}
type RecipeSpecV1 struct {
	SchemaVersion     string              `json:"schema_version"`
	Platform          string              `json:"platform"`
	Architecture      string              `json:"architecture"`
	BaseImage         string              `json:"base_image"`
	Environment       map[string]string   `json:"environment,omitempty"`
	InstallGroups     []InstallGroup      `json:"install_groups,omitempty"`
	ProfileVersionIDs []string            `json:"profile_version_ids"`
	Bootstrap         *BootstrapSpec      `json:"bootstrap,omitempty"`
	ExternalInputs    []ExternalInputSpec `json:"external_inputs,omitempty"`
	GuestProtocol     string              `json:"guest_protocol"`
}

func ValidateRecipeSpecV1(r RecipeSpecV1) error {
	if r.SchemaVersion != "1" || r.Platform != "linux" || r.Architecture == "" {
		return fmt.Errorf("schema_version, linux platform, and architecture are required")
	}
	if !recipePinnedBase.MatchString(r.BaseImage) {
		return fmt.Errorf("base_image must be digest pinned")
	}
	if strings.TrimSpace(r.GuestProtocol) == "" {
		return fmt.Errorf("guest_protocol is required")
	}
	if len(r.ProfileVersionIDs) == 0 {
		return fmt.Errorf("at least one immutable profile version is required")
	}
	seenProfiles := make(map[string]struct{}, len(r.ProfileVersionIDs))
	for _, id := range r.ProfileVersionIDs {
		if id == "" {
			return fmt.Errorf("recipe profile target is empty")
		}
		if _, exists := seenProfiles[id]; exists {
			return fmt.Errorf("recipe profile targets must be unique")
		}
		seenProfiles[id] = struct{}{}
	}
	for k := range r.Environment {
		if strings.HasPrefix(strings.ToUpper(k), "VELANE_") {
			return fmt.Errorf("reserved environment name %q", k)
		}
	}
	for _, g := range r.InstallGroups {
		if g.RepositorySnapshot == "" || !recipeSHA256Hex.MatchString(g.IndexDigest) || !recipeSHA256Hex.MatchString(g.LockDigest) || len(g.Packages) == 0 {
			return fmt.Errorf("install groups require immutable snapshot and integrity locks")
		}
		for _, p := range g.Packages {
			if p.Name == "" || !recipeExact.MatchString(p.Version) || !recipeSHA256Hex.MatchString(p.Digest) {
				return fmt.Errorf("package %q must have exact version and digest", p.Name)
			}
		}
	}
	if r.Bootstrap != nil {
		if len(r.Bootstrap.Script) == 0 || len(r.Bootstrap.Script) > 64*1024 || r.Bootstrap.TimeoutSeconds < 1 || r.Bootstrap.TimeoutSeconds > 600 {
			return fmt.Errorf("bootstrap exceeds bounds")
		}
	}
	for _, in := range r.ExternalInputs {
		u, err := url.Parse(in.URL)
		if err != nil || u.Scheme != "https" || u.User != nil || u.Fragment != "" || u.RawQuery != "" || u.Host == "" || strings.Contains(u.Host, "%") || strings.HasSuffix(strings.ToLower(u.Hostname()), ".") || !recipeSHA256Hex.MatchString(in.SHA256) || in.Size <= 0 {
			return fmt.Errorf("invalid external input")
		}
	}
	return nil
}
func RecipeDigest(r RecipeSpecV1) (string, error) {
	if err := ValidateRecipeSpecV1(r); err != nil {
		return "", err
	}
	return CanonicalDigest(r)
}
func DecodeRecipeSpecV1(data []byte) (RecipeSpecV1, error) {
	var r RecipeSpecV1
	d := json.NewDecoder(strings.NewReader(string(data)))
	d.DisallowUnknownFields()
	if err := d.Decode(&r); err != nil {
		return r, err
	}
	var trailing any
	if err := d.Decode(&trailing); !errors.Is(err, io.EOF) {
		if err == nil {
			return r, fmt.Errorf("only one recipe document is allowed")
		}
		return r, err
	}
	return r, ValidateRecipeSpecV1(r)
}
