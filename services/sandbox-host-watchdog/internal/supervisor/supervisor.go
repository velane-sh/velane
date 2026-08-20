package supervisor

import (
	"context"
	"errors"
	"os"
	"strings"
	"time"

	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/journal"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/lease"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/microvm"
	"github.com/abskrj/velane/services/sandbox-host-watchdog/internal/network"
)

type Clock interface {
	Now() time.Time
	BootID() string
}
type systemClock struct{}

func NewSystemClock() Clock        { return systemClock{} }
func (systemClock) Now() time.Time { return time.Now() }
func (systemClock) BootID() string {
	b, err := os.ReadFile("/proc/sys/kernel/random/boot_id")
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

type Supervisor struct {
	PublicKey            []byte
	Store                *journal.Store
	Network              network.Controller
	Fencer               microvm.Fencer
	Clock                Clock
	MaxTTL, SafetyMargin time.Duration
}

func (s *Supervisor) defaults() {
	if s.Clock == nil {
		s.Clock = NewSystemClock()
	}
	if s.MaxTTL == 0 {
		s.MaxTTL = 60 * time.Second
	}
	if s.SafetyMargin == 0 {
		s.SafetyMargin = 10 * time.Second
	}
}
func (s *Supervisor) Accept(ctx context.Context, g lease.Grant) error {
	s.defaults()
	if s.Store == nil || s.Network == nil || s.Fencer == nil || len(s.PublicKey) != 32 {
		return errors.New("watchdog is not configured")
	}
	now := s.Clock.Now()
	if err := lease.Validate(g, s.PublicKey, now, s.MaxTTL); err != nil {
		return err
	}
	if previous, found := s.Store.Entries()[g.SandboxID]; found {
		old := previous.Grant
		if old.HostID != g.HostID || g.HostIncarnation < old.HostIncarnation || (g.HostIncarnation == old.HostIncarnation && g.FenceEpoch < old.FenceEpoch) || (g.HostIncarnation == old.HostIncarnation && g.FenceEpoch == old.FenceEpoch && g.IssuedAtUnixMilli <= old.IssuedAtUnixMilli) || g.ExpiresAtUnixMilli <= old.ExpiresAtUnixMilli {
			return errors.New("stale watchdog lease grant")
		}
	}
	if time.UnixMilli(g.ExpiresAtUnixMilli).Sub(now) <= s.SafetyMargin {
		return errors.New("lease expires inside fencing safety margin")
	}
	if err := s.Store.Put(journal.Entry{Grant: g, AcceptedWallUnixMilli: now.UnixMilli(), AcceptedBootID: s.Clock.BootID()}); err != nil {
		return err
	}
	return s.Network.EnableLease(ctx, g.SandboxID)
}

// Tick derives deadlines exclusively from the signed wall expiry. Clock drift,
// an absent boot identity, or an expiry within the safety margin is unsafe.
func (s *Supervisor) Tick(ctx context.Context) error {
	s.defaults()
	if s.Store == nil || s.Network == nil || s.Fencer == nil {
		return errors.New("watchdog is not configured")
	}
	for id, e := range s.Store.Entries() {
		remaining := time.UnixMilli(e.Grant.ExpiresAtUnixMilli).Sub(s.Clock.Now())
		bootUnsafe := s.Clock.BootID() == "" || e.AcceptedBootID == "" || e.AcceptedBootID != s.Clock.BootID()
		unsafe := bootUnsafe || remaining <= s.SafetyMargin
		if unsafe {
			// A boot identity change means any previously scheduled deadline is
			// untrustworthy, so it must terminate immediately rather than wait.
			if err := s.fence(ctx, id, bootUnsafe || remaining <= 0); err != nil {
				return err
			}
		}
	}
	return nil
}

// Recover treats every persisted grant as unsafe: a restarted watchdog cannot
// prove a process-local scheduling deadline, so it denies, pauses, kills, and
// removes the old path until a new signed grant is accepted.
func (s *Supervisor) Recover(ctx context.Context) error {
	s.defaults()
	if s.Store == nil || s.Network == nil || s.Fencer == nil {
		return errors.New("watchdog is not configured")
	}
	for id := range s.Store.Entries() {
		if err := s.fence(ctx, id, true); err != nil {
			return err
		}
	}
	return nil
}
func (s *Supervisor) fence(ctx context.Context, id string, kill bool) error {
	if err := s.Network.DefaultDeny(ctx, id); err != nil {
		return err
	}
	if s.Fencer != nil {
		if err := s.Fencer.Pause(ctx, id); err != nil {
			return err
		}
		if kill {
			if err := s.Fencer.Kill(ctx, id); err != nil {
				return err
			}
		}
	}
	if kill {
		if err := s.Network.Remove(ctx, id); err != nil {
			return err
		}
		return s.Store.Delete(id)
	}
	return nil
}
