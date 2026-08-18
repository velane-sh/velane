package handlers

import (
	"net/http"
	"os"

	"github.com/abskrj/velane/services/control-plane/internal/license"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol"
	"go.uber.org/zap"
)

type InstanceHandler struct {
	licMgr       *license.Manager
	cloudMode    bool
	capabilities sandboxcontrol.Provider
	log          *zap.Logger
}

func NewInstanceHandler(licMgr *license.Manager, log *zap.Logger, capabilities sandboxcontrol.Provider) *InstanceHandler {
	if capabilities == nil {
		capabilities = sandboxcontrol.StaticProvider{}
	}
	return &InstanceHandler{
		licMgr: licMgr, cloudMode: os.Getenv("VELANE_CLOUD") == "true",
		capabilities: capabilities, log: log,
	}
}

type instanceCapabilities struct {
	Sandboxes           bool `json:"sandboxes"`
	SandboxProfiles     bool `json:"sandbox_profiles"`
	SandboxImageRecipes bool `json:"sandbox_image_recipes"`
	SandboxOperations   bool `json:"sandbox_operations"`
	SandboxSnapshots    bool `json:"sandbox_snapshots"`
	SandboxEvents       bool `json:"sandbox_events"`
	SandboxLogs         bool `json:"sandbox_logs"`
}

type instanceInfoResponse struct {
	Cloud        bool                 `json:"cloud"`
	Plan         string               `json:"plan"`
	LicenseValid bool                 `json:"license_valid"`
	Features     []string             `json:"features"`
	Capabilities instanceCapabilities `json:"capabilities"`
}

func (h *InstanceHandler) GetInfo(w http.ResponseWriter, r *http.Request) {
	plan, features, valid := h.licMgr.InstanceStatus(r.Context())
	if features == nil {
		features = []string{}
	}
	capabilities := h.capabilities.Capabilities(r.Context())
	writeJSON(w, http.StatusOK, instanceInfoResponse{
		Cloud:        h.cloudMode,
		Plan:         plan,
		LicenseValid: valid,
		Features:     features,
		Capabilities: instanceCapabilities{
			Sandboxes: capabilities.Sandboxes, SandboxProfiles: capabilities.SandboxProfiles,
			SandboxImageRecipes: capabilities.SandboxImageRecipes, SandboxOperations: capabilities.SandboxOperations,
			SandboxSnapshots: capabilities.SandboxSnapshots, SandboxEvents: capabilities.SandboxEvents,
			SandboxLogs: capabilities.SandboxLogs,
		},
	})
}
