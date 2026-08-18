//go:build linux

// Package quiesce coordinates bounded guest IO drain and filesystem freeze.
package quiesce

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"sort"
)

type Filesystem interface {
	SyncFS(context.Context, string) error
	Freeze(context.Context, string) error
	Thaw(context.Context, string) error
}
type Drainer interface {
	Drain(context.Context) error
	RevokeEphemeralCredentials(context.Context) error
}
type Mount struct {
	DriveID, Path string
	Writable      bool
}
type Coordinator struct {
	Filesystem Filesystem
	Drainer    Drainer
}

func (c Coordinator) Quiesce(ctx context.Context, mounts []Mount) (string, []Mount, error) {
	if c.Filesystem == nil || c.Drainer == nil {
		return "", nil, errors.New("quiesce dependencies are required")
	}
	if err := c.Drainer.Drain(ctx); err != nil {
		return "", nil, err
	}
	if err := c.Drainer.RevokeEphemeralCredentials(ctx); err != nil {
		return "", nil, err
	}
	writable := make([]Mount, 0)
	for _, m := range mounts {
		if !m.Writable {
			continue
		}
		if m.DriveID == "" || m.Path == "" {
			return "", nil, errors.New("writable mount has no drive inventory")
		}
		if err := c.Filesystem.SyncFS(ctx, m.Path); err != nil {
			return "", nil, err
		}
		if err := c.Filesystem.Freeze(ctx, m.Path); err != nil {
			_ = c.thaw(context.Background(), writable)
			return "", nil, err
		}
		writable = append(writable, m)
	}
	sort.Slice(writable, func(i, j int) bool { return writable[i].DriveID < writable[j].DriveID })
	b := make([]byte, 32)
	if _, err := rand.Read(b); err != nil {
		_ = c.thaw(context.Background(), writable)
		return "", nil, err
	}
	return hex.EncodeToString(b), writable, nil
}
func (c Coordinator) Thaw(ctx context.Context, mounts []Mount) error { return c.thaw(ctx, mounts) }
func (c Coordinator) thaw(ctx context.Context, mounts []Mount) error {
	var first error
	for i := len(mounts) - 1; i >= 0; i-- {
		if err := c.Filesystem.Thaw(ctx, mounts[i].Path); err != nil && first == nil {
			first = err
		}
	}
	return first
}
