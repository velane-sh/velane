package redisstore

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/redis/go-redis/v9"
)

const jobQueueKey = "velane:jobs"
const integrationEventQueueKey = "velane:integration-events"

type IntegrationEventJob struct {
	ReceiptID string `json:"receipt_id"`
	Attempt   int    `json:"attempt"`
}

func (c *Client) EnqueueIntegrationEvent(ctx context.Context, job IntegrationEventJob) error {
	b, e := json.Marshal(job)
	if e != nil {
		return e
	}
	return c.rdb.LPush(ctx, integrationEventQueueKey, b).Err()
}

func (c *Client) DequeueIntegrationEvent(ctx context.Context) (*IntegrationEventJob, error) {
	for {
		if ctx.Err() != nil {
			return nil, nil
		}
		v, e := c.rdb.BRPop(ctx, time.Second, integrationEventQueueKey).Result()
		if e != nil {
			if errors.Is(e, context.Canceled) || errors.Is(e, context.DeadlineExceeded) {
				return nil, nil
			}
			if errors.Is(e, redis.Nil) {
				continue
			}
			return nil, e
		}
		if len(v) < 2 {
			return nil, fmt.Errorf("invalid integration event job")
		}
		var j IntegrationEventJob
		if e = json.Unmarshal([]byte(v[1]), &j); e != nil {
			return nil, e
		}
		return &j, nil
	}
}

// EgressPolicyJob carries egress policy in an enqueued job.
type EgressPolicyJob struct {
	BlockedCIDRs   []string `json:"blocked_cidrs"`
	BlockedDomains []string `json:"blocked_domains"`
}

// Job is the unit of async work pushed to Redis.
type Job struct {
	InvocationID  string            `json:"invocation_id"`
	SnippetID     string            `json:"snippet_id"`
	VersionID     string            `json:"version_id"`
	TenantID      string            `json:"tenant_id"`
	Language      string            `json:"language"`
	Code          string            `json:"code"`
	Input         string            `json:"input"`
	TimeoutMs     int               `json:"timeout_ms"`
	MaxMemoryMB   int               `json:"max_memory_mb"`
	MaxCPUPercent int               `json:"max_cpu_percent"`
	CallbackURL   string            `json:"callback_url,omitempty"`
	Env           string            `json:"env"`
	SecretEnvVars map[string]string `json:"secret_env_vars,omitempty"`
	Libraries     map[string]string `json:"libraries,omitempty"`
	EgressPolicy  *EgressPolicyJob  `json:"egress_policy,omitempty"`

	// Stream signals the worker to execute via the streaming path and publish
	// typed events to the per-invocation event stream (used by queued sync and
	// stream invocations). When false the worker uses the buffered path.
	Stream bool `json:"stream,omitempty"`
}

// Enqueue serialises job as JSON and pushes it to the left of the job list.
func (c *Client) Enqueue(ctx context.Context, job Job) error {
	data, err := json.Marshal(job)
	if err != nil {
		return fmt.Errorf("enqueue marshal: %w", err)
	}
	if err := c.rdb.LPush(ctx, jobQueueKey, data).Err(); err != nil {
		return fmt.Errorf("enqueue lpush: %w", err)
	}
	return nil
}

// Dequeue blocks until a job is available or the context is cancelled,
// whichever comes first.
// Returns (nil, nil) when the context is cancelled.
func (c *Client) Dequeue(ctx context.Context) (*Job, error) {
	for {
		if ctx.Err() != nil {
			return nil, nil
		}

		result, err := c.rdb.BRPop(ctx, time.Second, jobQueueKey).Result()
		if err != nil {
			if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
				return nil, nil
			}
			if errors.Is(err, redis.Nil) {
				continue
			}
			return nil, fmt.Errorf("dequeue brpop: %w", err)
		}

		if len(result) < 2 {
			return nil, fmt.Errorf("dequeue: unexpected brpop result length %d", len(result))
		}

		var job Job
		if err := json.Unmarshal([]byte(result[1]), &job); err != nil {
			return nil, fmt.Errorf("dequeue unmarshal: %w", err)
		}
		return &job, nil
	}
}
