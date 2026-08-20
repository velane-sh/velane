package microvm

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"time"
)

// FirecrackerClient speaks directly to the Firecracker Unix-domain API socket.
// It intentionally has no boot-source method: restores must use LoadSnapshot.
type FirecrackerClient struct {
	APISocket string
	Timeout   time.Duration
}

func (c FirecrackerClient) request(ctx context.Context, method, path string, body any) error {
	if c.APISocket == "" {
		return fmt.Errorf("Firecracker API socket is required")
	}
	encoded, err := json.Marshal(body)
	if err != nil {
		return err
	}
	transport := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
		return (&net.Dialer{}).DialContext(ctx, "unix", c.APISocket)
	}}
	defer transport.CloseIdleConnections()
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = 15 * time.Second
	}
	req, err := http.NewRequestWithContext(ctx, method, "http://localhost"+path, bytes.NewReader(encoded))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := (&http.Client{Transport: transport, Timeout: timeout}).Do(req)
	if err != nil {
		return fmt.Errorf("Firecracker API %s %s: %w", method, path, err)
	}
	defer resp.Body.Close()
	if resp.StatusCode < http.StatusOK || resp.StatusCode >= http.StatusMultipleChoices {
		return fmt.Errorf("Firecracker API %s %s returned %s", method, path, resp.Status)
	}
	return nil
}

func (c FirecrackerClient) Pause(ctx context.Context) error {
	return c.request(ctx, http.MethodPut, "/actions", map[string]string{"action_type": "Pause"})
}
func (c FirecrackerClient) Resume(ctx context.Context) error {
	return c.request(ctx, http.MethodPut, "/actions", map[string]string{"action_type": "Resume"})
}
func (c FirecrackerClient) StartInstance(ctx context.Context) error {
	return c.request(ctx, http.MethodPut, "/actions", map[string]string{"action_type": "InstanceStart"})
}
func (c FirecrackerClient) CreateFullSnapshot(ctx context.Context, memory, state string) error {
	if memory == "" || state == "" {
		return fmt.Errorf("Firecracker full snapshot requires memory and state paths")
	}
	return c.request(ctx, http.MethodPut, "/snapshot/create", map[string]string{
		"snapshot_type": "Full", "mem_file_path": memory, "snapshot_path": state,
	})
}
func (c FirecrackerClient) LoadSnapshot(ctx context.Context, memory, state string) error {
	if memory == "" || state == "" {
		return fmt.Errorf("Firecracker snapshot load requires memory and state paths")
	}
	return c.request(ctx, http.MethodPut, "/snapshot/load", map[string]any{
		"mem_file_path": memory, "snapshot_path": state, "enable_diff_snapshots": false,
	})
}
func (c FirecrackerClient) ConfigureMachine(ctx context.Context, vcpu, memoryMB int, smt bool) error {
	return c.request(ctx, http.MethodPut, "/machine-config", map[string]any{"vcpu_count": vcpu, "mem_size_mib": memoryMB, "smt": smt})
}
func (c FirecrackerClient) ConfigureBootSource(ctx context.Context, kernelPath, bootArgs string) error {
	return c.request(ctx, http.MethodPut, "/boot-source", map[string]string{"kernel_image_path": kernelPath, "boot_args": bootArgs})
}
func (c FirecrackerClient) ConfigureDrive(ctx context.Context, drive DriveConfig) error {
	return c.request(ctx, http.MethodPut, "/drives/"+drive.ID, map[string]any{"drive_id": drive.ID, "path_on_host": drive.PathOnHost, "is_root_device": drive.Root, "is_read_only": drive.ReadOnly})
}
