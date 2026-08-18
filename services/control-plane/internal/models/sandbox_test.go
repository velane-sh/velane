package models

import (
	"encoding/json"
	"strings"
	"testing"
)

func TestPublicSandboxOmitsPrivateControlPlaneFields(t *testing.T) {
	encoded, err := json.Marshal(PublicSandbox(Sandbox{
		ID:                        "sb-1",
		TenantID:                  "tenant-private",
		Name:                      "demo",
		RecipeVersionID:           "recipe-version-1",
		ProfileVersionID:          "profile-version-1",
		VMRestoreDescriptorDigest: "descriptor-private",
		DesiredState:              "running",
		ObservedState:             "pending",
		Generation:                1,
		ObservedGeneration:        0,
		FenceEpoch:                42,
		EverBooted:                true,
	}, []string{"stop"}))
	if err != nil {
		t.Fatal(err)
	}

	for _, private := range []string{"tenant-private", "descriptor-private", "tenant_id", "observed_generation", "fence_epoch", "ever_booted"} {
		if strings.Contains(string(encoded), private) {
			t.Fatalf("public sandbox JSON contains private field/value %q: %s", private, encoded)
		}
	}
	if !strings.Contains(string(encoded), `"available_actions":["stop"]`) {
		t.Fatalf("public sandbox JSON is missing available actions: %s", encoded)
	}
}
