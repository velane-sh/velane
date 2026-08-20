// Package control provides the root-owned Unix socket for signed lease grants.
package control

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"

	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/lease"
)

type Acceptor interface {
	Accept(context.Context, lease.Grant) error
}
type request struct {
	Grant lease.Grant `json:"grant"`
}
type response struct {
	Error string `json:"error,omitempty"`
}

type Server struct {
	Path     string
	GroupID  int
	Acceptor Acceptor
}

func (s Server) Listen(ctx context.Context) error {
	if s.Path == "" || s.Acceptor == nil {
		return errors.New("watchdog control socket and acceptor are required")
	}
	if err := os.MkdirAll(filepath.Dir(s.Path), 0700); err != nil {
		return err
	}
	if err := os.Remove(s.Path); err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	ln, err := net.Listen("unix", s.Path)
	if err != nil {
		return fmt.Errorf("listen watchdog control socket: %w", err)
	}
	defer func() { _ = ln.Close(); _ = os.Remove(s.Path) }()
	if s.GroupID > 0 {
		if err := os.Chown(s.Path, 0, s.GroupID); err != nil {
			return err
		}
	}
	if err := os.Chmod(s.Path, 0660); err != nil {
		return err
	}
	go func() { <-ctx.Done(); _ = ln.Close() }()
	for {
		conn, err := ln.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.handle(ctx, conn)
	}
}
func (s Server) handle(ctx context.Context, conn net.Conn) {
	defer conn.Close()
	var req request
	if err := json.NewDecoder(net.Conn(conn)).Decode(&req); err != nil {
		_ = json.NewEncoder(conn).Encode(response{Error: "invalid lease request"})
		return
	}
	if err := s.Acceptor.Accept(ctx, req.Grant); err != nil {
		_ = json.NewEncoder(conn).Encode(response{Error: err.Error()})
		return
	}
	_ = json.NewEncoder(conn).Encode(response{})
}

type Client struct{ Path string }

func (c Client) Deliver(ctx context.Context, grant lease.Grant) error {
	if c.Path == "" {
		return errors.New("watchdog control socket is required")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Path)
	if err != nil {
		return fmt.Errorf("connect watchdog: %w", err)
	}
	defer conn.Close()
	if err := json.NewEncoder(conn).Encode(request{Grant: grant}); err != nil {
		return err
	}
	var resp response
	if err := json.NewDecoder(conn).Decode(&resp); err != nil {
		return err
	}
	if resp.Error != "" {
		return fmt.Errorf("watchdog rejected lease: %s", resp.Error)
	}
	return nil
}
