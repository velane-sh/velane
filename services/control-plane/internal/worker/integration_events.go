package worker

import (
	"context"
	"encoding/json"
	"time"

	"github.com/abskrj/velane/services/control-plane/internal/nango"
	"github.com/abskrj/velane/services/control-plane/internal/scheduler"
	"github.com/abskrj/velane/services/control-plane/internal/store/postgres"
	redisstore "github.com/abskrj/velane/services/control-plane/internal/store/redis"
	"go.uber.org/zap"
)

type IntegrationEventWorker struct {
	queue     *redisstore.Client
	store     *postgres.Store
	nango     *nango.Client
	scheduler *scheduler.Scheduler
	log       *zap.Logger
}

func NewIntegrationEventWorker(q *redisstore.Client, s *postgres.Store, n *nango.Client, sched *scheduler.Scheduler, log *zap.Logger) *IntegrationEventWorker {
	return &IntegrationEventWorker{q, s, n, sched, log}
}
func (w *IntegrationEventWorker) Run(ctx context.Context) {
	for ctx.Err() == nil {
		job, err := w.queue.DequeueIntegrationEvent(ctx)
		if err != nil {
			w.log.Error("integration event dequeue", zap.Error(err))
			continue
		}
		if job == nil {
			return
		}
		if err = w.process(ctx, job); err != nil {
			w.log.Error("integration event processing", zap.String("receipt_id", job.ReceiptID), zap.Error(err))
			if job.Attempt < 4 {
				job.Attempt++
				delay := time.Second * time.Duration(1<<job.Attempt)
				go func(j redisstore.IntegrationEventJob) {
					select {
					case <-ctx.Done():
					case <-time.After(delay):
						_ = w.queue.EnqueueIntegrationEvent(ctx, j)
					}
				}(*job)
			} else {
				_ = w.store.MarkIntegrationEvent(ctx, job.ReceiptID, "failed", err.Error())
			}
		}
	}
}

type eventRecord struct {
	Action string         `json:"action"`
	Data   map[string]any `json:"data"`
}

func (w *IntegrationEventWorker) process(ctx context.Context, job *redisstore.IntegrationEventJob) error {
	r, err := w.store.GetIntegrationEventReceipt(ctx, job.ReceiptID)
	if err != nil {
		return err
	}
	if r.Status == "completed" {
		return nil
	}
	_ = w.store.MarkIntegrationEvent(ctx, r.ID, "processing", "")
	if r.InitialSync {
		return w.store.MarkIntegrationEvent(ctx, r.ID, "completed", "")
	}
	triggers, err := w.store.ListEnabledTriggersForEvent(ctx, r.ConnectionID, r.Model)
	if err != nil {
		return err
	}
	if len(triggers) == 0 {
		return w.store.MarkIntegrationEvent(ctx, r.ID, "completed", "")
	}
	conn, err := w.store.GetConnectionByID(ctx, r.TenantID, r.ConnectionID)
	if err != nil {
		return err
	}
	var records []eventRecord
	cursor := ""
	for {
		page, e := w.nango.ListRecords(ctx, conn.NangoConnectionID, r.ProviderConfigKey, r.Model, r.ModifiedAfter, cursor)
		if e != nil {
			return e
		}
		for _, rec := range page.Records {
			records = append(records, eventRecord{rec.Action, rec.Data})
		}
		if page.NextCursor == "" || page.NextCursor == cursor {
			break
		}
		cursor = page.NextCursor
	}
	for _, t := range triggers {
		if predatesActivation(r.ModifiedAfter, t.ActivatedAt) {
			continue
		}
		filtered := filterEventRecords(records, t.ChangeTypes)
		if len(filtered) == 0 {
			_ = w.store.UpsertTriggerDispatch(ctx, r.ID, t.ID, []string{}, "completed", "")
			continue
		}
		batches := splitEventRecords(filtered)
		ids := []string{}
		for i, batch := range batches {
			envelope := buildEventEnvelope(r.ID, conn.Provider, conn.ID, conn.Alias, r.Model, r.ModifiedAfter, batch, i+1, len(batches))
			raw, _ := json.Marshal(envelope)
			inv, e := w.scheduler.InvokeAsync(ctx, scheduler.InvokeRequest{TenantID: r.TenantID, SnippetSlug: t.WorkflowID, Env: t.Environment, Input: string(raw)}, "")
			if e != nil {
				_ = w.store.UpsertTriggerDispatch(ctx, r.ID, t.ID, ids, "failed", e.Error())
				return e
			}
			ids = append(ids, inv.ID)
		}
		if e := w.store.UpsertTriggerDispatch(ctx, r.ID, t.ID, ids, "completed", ""); e != nil {
			return e
		}
	}
	return w.store.MarkIntegrationEvent(ctx, r.ID, "completed", "")
}

func predatesActivation(modifiedAfter string, activatedAt *time.Time) bool {
	if activatedAt == nil {
		return false
	}
	modified, err := time.Parse(time.RFC3339, modifiedAfter)
	return err == nil && modified.Before(*activatedAt)
}

func filterEventRecords(records []eventRecord, changeTypes []string) []eventRecord {
	allowed := map[string]bool{}
	for _, change := range changeTypes {
		allowed[change] = true
	}
	out := []eventRecord{}
	for _, record := range records {
		if allowed[record.Action] {
			out = append(out, record)
		}
	}
	return out
}

func buildEventEnvelope(receiptID, provider, connectionID, alias, model, modifiedAfter string, records []eventRecord, index, count int) map[string]any {
	return map[string]any{"id": receiptID, "source": "nango", "provider": provider, "connection": map[string]any{"id": connectionID, "alias": alias}, "event": map[string]any{"type": "sync.records.changed", "model": model, "modified_after": modifiedAfter}, "changes": map[string]any{"records": records}, "batch": map[string]any{"index": index, "count": count, "is_last": index == count}}
}
func splitEventRecords(records []eventRecord) [][]eventRecord {
	const maxBytes = 512 * 1024
	result := [][]eventRecord{}
	batch := []eventRecord{}
	size := 2
	for _, r := range records {
		b, _ := json.Marshal(r)
		if len(batch) > 0 && (len(batch) >= 100 || size+len(b)+1 > maxBytes) {
			result = append(result, batch)
			batch = []eventRecord{}
			size = 2
		}
		batch = append(batch, r)
		size += len(b) + 1
	}
	if len(batch) > 0 {
		result = append(result, batch)
	}
	return result
}
