package config

import (
	"encoding/base64"
	"fmt"
	"os"
	"strconv"
)

type Config struct {
	LeaseJournalPath, PublicKeyBase64 string
	ControlSocket, PIDDir             string
	NFTBinary, NFTTable               string
	AgentGroupID                      int
	TickMillis                        int
}

func FromEnv() Config {
	return Config{LeaseJournalPath: envOr("SANDBOX_WATCHDOG_LEASE_JOURNAL", "/var/lib/velane-sandbox-watchdog/leases/leases.json"), PublicKeyBase64: os.Getenv("SANDBOX_WATCHDOG_PUBLIC_KEY_BASE64"), ControlSocket: envOr("SANDBOX_WATCHDOG_CONTROL_SOCKET", "/run/velane-sandbox-watchdog/control.sock"), PIDDir: envOr("SANDBOX_WATCHDOG_PID_DIR", "/run/velane-sandbox-watchdog/pids"), NFTBinary: envOr("SANDBOX_WATCHDOG_NFT_BINARY", "nft"), NFTTable: envOr("SANDBOX_WATCHDOG_NFT_TABLE", "velane_sandbox_watchdog"), AgentGroupID: envInt("SANDBOX_WATCHDOG_AGENT_GROUP_ID"), TickMillis: 250}
}
func envOr(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
func envInt(key string) int { value, _ := strconv.Atoi(os.Getenv(key)); return value }
func (c Config) Validate() error {
	if c.LeaseJournalPath == "" || c.ControlSocket == "" || c.PIDDir == "" {
		return fmt.Errorf("lease journal path, control socket, and PID directory are required")
	}
	if c.AgentGroupID <= 0 {
		return fmt.Errorf("agent group ID is required for watchdog socket ownership")
	}
	key, err := base64.StdEncoding.DecodeString(c.PublicKeyBase64)
	if err != nil || len(key) != 32 {
		return fmt.Errorf("32-byte control-plane Ed25519 public key is required")
	}
	if c.TickMillis < 10 || c.TickMillis > 1000 {
		return fmt.Errorf("watchdog tick must be 10-1000ms")
	}
	return nil
}
