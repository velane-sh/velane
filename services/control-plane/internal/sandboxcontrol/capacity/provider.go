package capacity

import "context"

type PoolRef struct {
	ID       string
	Provider string
}
type PoolObservation struct{ CurrentHosts, ReadyHosts, DesiredHosts int }
type CapacityResult struct {
	DesiredHosts int
	Changed      bool
}
type LifecycleAction struct{ ID, PoolID, ProviderRef string }
type LifecycleResult struct {
	Completed bool
	Reason    string
}
type Provider interface {
	ObservePool(context.Context, PoolRef) (PoolObservation, error)
	EnsureDesiredCapacity(context.Context, PoolRef, int, string) (CapacityResult, error)
	CompleteLifecycleAction(context.Context, LifecycleAction, LifecycleResult) error
}
