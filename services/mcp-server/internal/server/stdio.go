package server

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"strings"

	"github.com/abskrj/velane/services/mcp-server/internal/protocol"
)

func RunStdio(ctx context.Context, srv *Server, in io.Reader, out io.Writer, authHeader string) error {
	scanner := bufio.NewScanner(in)
	encoder := json.NewEncoder(out)

	for scanner.Scan() {
		select {
		case <-ctx.Done():
			return ctx.Err()
		default:
		}

		line := strings.TrimSpace(scanner.Text())
		if line == "" {
			continue
		}
		var req protocol.Request
		if err := decodeJSON([]byte(line), &req); err != nil {
			resp := protocol.Error(nil, -32700, "parse error", err.Error())
			if encodeErr := encoder.Encode(resp); encodeErr != nil {
				return encodeErr
			}
			continue
		}

		if isNotification(req) {
			_ = srv.HandleRequest(ctx, authHeader, req)
			continue
		}

		resp := srv.HandleRequest(ctx, authHeader, req)
		if err := encoder.Encode(resp); err != nil {
			return err
		}
	}

	return scanner.Err()
}
