package main

import (
	"fmt"
	"log"
	"os"
)

// Guest init is started by the immutable guest image. Without its trusted vsock
// supervisor, accepting work would break the isolation boundary, so fail closed.
func main() {
	vsockDevice := os.Getenv("VELANE_SANDBOX_GUEST_VSOCK_DEVICE")
	if vsockDevice == "" {
		log.Print("configuration error: VELANE_SANDBOX_GUEST_VSOCK_DEVICE is required")
		os.Exit(2)
	}
	if _, err := os.Stat(vsockDevice); err != nil {
		log.Printf("configuration error: guest vsock device unavailable: %v", err)
		os.Exit(2)
	}
	log.Printf("guest-init cannot start without the immutable authenticated vsock supervisor for %s", vsockDevice)
	os.Exit(1)
}

var _ = fmt.Sprintf
