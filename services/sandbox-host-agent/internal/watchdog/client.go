// Package watchdog delivers only opaque, control-plane-signed grants to the
// independent root watchdog. The agent cannot construct or extend a grant.
package watchdog

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
)

type Client struct{ Socket string }
type request struct {
	Grant json.RawMessage `json:"grant"`
}
type response struct {
	Error string `json:"error"`
}

func (c Client) Deliver(ctx context.Context, grant json.RawMessage) error {
	if c.Socket == "" || len(grant) == 0 {
		return errors.New("watchdog socket and signed lease grant are required")
	}
	conn, err := (&net.Dialer{}).DialContext(ctx, "unix", c.Socket)
	if err != nil {
		return fmt.Errorf("connect watchdog control socket: %w", err)
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
		return fmt.Errorf("watchdog rejected signed lease grant: %s", resp.Error)
	}
	return nil
}
