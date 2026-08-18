package main

import (
	"context"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abskrj/velane/services/sandbox-host-agent/internal/agent"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/attestation"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/config"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/controlplane"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/inventory"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/microvm"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/network"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/reconcile"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/resources"
	"github.com/abskrj/velane/services/sandbox-host-agent/internal/watchdog"
)

func main() {
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		log.Printf("configuration error: %v", err)
		os.Exit(2)
	}
	journal, err := reconcile.OpenJournal(cfg.JournalPath)
	if err != nil {
		log.Printf("open journal: %v", err)
		os.Exit(1)
	}
	client, err := controlplane.NewClient(controlplane.TLSConfig{BaseURL: cfg.ControlURL, ServerName: cfg.TLSServerName, CAFile: cfg.CAFile, CertFile: cfg.ClientCertFile, KeyFile: cfg.ClientKeyFile})
	if err != nil {
		log.Printf("mTLS client: %v", err)
		os.Exit(1)
	}
	vm := microvm.NewLinuxManager()
	watchdogClient := watchdog.Client{Socket: cfg.WatchdogSocket}
	networkDriver := network.LinuxManager{IPBinary: cfg.IPBinary}
	provisioner := agent.Provisioner{
		RuntimeRoot: cfg.RuntimeRoot, DiskRoot: cfg.DiskRoot,
		JailerBinary: cfg.JailerBinary, FirecrackerBinary: cfg.FirecrackerBinary,
		VM: vm, Cgroups: resources.Cgroup{Root: cfg.CgroupRoot},
		Disks: resources.RawDiskDriver{}, Network: networkDriver, Watchdog: watchdogClient,
	}
	// Do not advertise a snapshot capability without a configured key-wrapper.
	// The concrete wrapper is installation-specific and must be supplied by the
	// host bootstrap before snapshot commands are enabled.
	executor := agent.LifecycleExecutor{VM: vm, Network: networkDriver, RuntimeRoot: cfg.RuntimeRoot, Create: provisioner.Create, Watchdog: watchdogClient}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	csr, err := controlplane.GenerateCSR(cfg.ClientKeyFile)
	if err != nil {
		log.Printf("generate host CSR: %v", err)
		os.Exit(1)
	}
	hostID := cfg.HostID
	var hostIncarnation uint64
	var certificateExpiresAt time.Time
	if existing, ok, identityErr := client.ExistingIdentity(); identityErr != nil {
		log.Printf("load enrolled host identity: %v", identityErr)
		os.Exit(1)
	} else if ok {
		if existing.PoolID != cfg.PoolID || (hostID != "" && existing.HostID != hostID) {
			log.Printf("enrolled host identity does not match configured pool/host")
			os.Exit(1)
		}
		hostID, hostIncarnation, certificateExpiresAt = existing.HostID, existing.Incarnation, existing.ExpiresAt
	}
	hostInventory, err := inventory.Collect(cfg.StagingPath, cfg.ProtocolVersion, cfg.AgentVersion)
	if err != nil {
		log.Printf("collect host inventory: %v", err)
		os.Exit(1)
	}
	hostInventory.CommandCapabilities = agent.DetectedCapabilities(cfg)
	if !hostInventory.CommandCapabilities["Destroy"] {
		log.Printf("host is not registerable: production lifecycle prerequisites are unavailable")
		os.Exit(1)
	}
	loop := agent.Loop{Client: client, Agent: agent.Agent{Journal: journal, Executor: executor}, Watchdog: watchdogClient, HostID: hostID, HostIncarnation: hostIncarnation, CertificateExpiresAt: certificateExpiresAt, PoolID: cfg.PoolID, BootID: cfg.BootID, HostCompatibilityKey: cfg.HostCompatibilityKey, CSR: csr, Inventory: hostInventory, ProviderEvidence: func(ctx context.Context, nonce string) (string, string, string, error) {
		return attestation.AWSEvidence(ctx, "", nonce)
	}}
	if err := loop.Run(ctx); err != nil && !errors.Is(err, context.Canceled) {
		log.Printf("host agent stopped: %v", err)
		os.Exit(1)
	}
}
