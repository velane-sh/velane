package sandboxcontrol

import (
	"context"
	"go.uber.org/zap"
	"time"
)

// Manager owns durable reconciliation loops. Each iteration is intentionally
// database-backed; it never relies on Redis or detached retry goroutines.
type Manager struct {
	service  *Service
	log      *zap.Logger
	interval time.Duration
}

func NewManager(service *Service, log *zap.Logger) *Manager {
	return &Manager{service: service, log: log, interval: time.Second}
}
func (m *Manager) Run(ctx context.Context) {
	ticker := time.NewTicker(m.interval)
	defer ticker.Stop()
	for {
		if err := m.service.ReconcileDueOperations(ctx); err != nil && ctx.Err() == nil {
			m.log.Warn("sandbox operation reconcile failed", zap.Error(err))
		}
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}
