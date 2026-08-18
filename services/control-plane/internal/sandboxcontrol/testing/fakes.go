package testing

import (
	"context"
	"github.com/abskrj/velane/services/control-plane/internal/sandboxcontrol/capacity"
	"sync"
	"time"
)

type Clock struct {
	mu       sync.Mutex
	NowValue time.Time
}

func (c *Clock) Now() time.Time { c.mu.Lock(); defer c.mu.Unlock(); return c.NowValue }
func (c *Clock) Advance(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.NowValue = c.NowValue.Add(d)
}

type CapacityProvider struct {
	mu          sync.Mutex
	Observation capacity.PoolObservation
	Requests    []int
}

func (f *CapacityProvider) ObservePool(context.Context, capacity.PoolRef) (capacity.PoolObservation, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.Observation, nil
}
func (f *CapacityProvider) EnsureDesiredCapacity(_ context.Context, _ capacity.PoolRef, n int, _ string) (capacity.CapacityResult, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.Requests = append(f.Requests, n)
	f.Observation.DesiredHosts = n
	return capacity.CapacityResult{DesiredHosts: n, Changed: true}, nil
}
func (f *CapacityProvider) CompleteLifecycleAction(context.Context, capacity.LifecycleAction, capacity.LifecycleResult) error {
	return nil
}
