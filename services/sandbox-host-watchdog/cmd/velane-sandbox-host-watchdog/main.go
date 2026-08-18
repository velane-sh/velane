package main

import (
	"context"
	"encoding/base64"
	"errors"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/config"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/control"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/journal"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/microvm"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/network"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/supervisor"
)

func main() {
	cfg := config.FromEnv()
	if err := cfg.Validate(); err != nil {
		log.Printf("configuration error: %v", err)
		os.Exit(2)
	}
	key, err := base64.StdEncoding.DecodeString(cfg.PublicKeyBase64)
	if err != nil {
		log.Printf("decode public key: %v", err)
		os.Exit(2)
	}
	store, err := journal.Open(cfg.LeaseJournalPath)
	if err != nil {
		log.Printf("open lease journal: %v", err)
		os.Exit(1)
	}
	nft := network.NFTables{Binary: cfg.NFTBinary, Table: cfg.NFTTable}
	s := &supervisor.Supervisor{PublicKey: key, Store: store, Network: nft, Fencer: microvm.ProcessFencer{PIDDir: cfg.PIDDir}}
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	// Privilege/tooling failures are fatal. Continuing with an unfenced path is
	// unsafe and must make this host non-placeable.
	if err := s.Recover(ctx); err != nil {
		log.Printf("recover watchdog: %v", err)
		os.Exit(1)
	}
	go func() {
		if err := (control.Server{Path: cfg.ControlSocket, GroupID: cfg.AgentGroupID, Acceptor: s}).Listen(ctx); err != nil && !errors.Is(err, context.Canceled) {
			log.Printf("watchdog control socket: %v", err)
			stop()
		}
	}()
	ticker := time.NewTicker(time.Duration(cfg.TickMillis) * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := s.Tick(ctx); err != nil {
			log.Printf("watchdog tick: %v", err)
			os.Exit(1)
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
