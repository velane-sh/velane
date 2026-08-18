package control

import (
	"context"
	"errors"
	"io"
)

// Server is transport-agnostic so the privileged guest vsock listener can be
// integration-tested separately. A transport must authenticate the host before
// forwarding requests; guest init never opens a network listener.
type Server interface {
	Serve(context.Context, io.ReadWriteCloser) error
}

type FailClosedServer struct{}

func (FailClosedServer) Serve(context.Context, io.ReadWriteCloser) error {
	return errors.New("guest-init vsock service is not installed")
}
