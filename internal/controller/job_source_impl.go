package controller

import (
	"context"
	"fmt"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/storage/wal"
	"github.com/ChuLiYu/raft-recovery/internal/worker"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ============================================================================
// JobSource Interface Implementation (Phase 2)
// ============================================================================

// Poll implements worker.JobSource.Poll
// It acts as a local job dispatcher: fetching pending jobs and marking them as in-flight.
func (c *Controller) Poll(ctx context.Context, maxJobs int) ([]*types.Job, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Check if controller is stopped
	if c.stopped {
		return nil, worker.ErrPoolClosed
	}
	if c.raftNode != nil {
		return nil, fmt.Errorf("replicated jobs must be dispatched through Raft")
	}

	// WAL append and local state change share the standalone snapshot lock.
	pending := c.jobManager.Pending(maxJobs)
	jobs := make([]*types.Job, 0, len(pending))
	deadline := time.Now().Add(c.config.TaskTimeout)
	for _, job := range pending {
		if err := c.wal.Append(wal.EventDispatch, &job); err != nil {
			return nil, fmt.Errorf("persist dispatch %s: %w", job.ID, err)
		}
		if popped := c.jobManager.PopPending(); popped == nil || popped.ID != job.ID {
			return nil, fmt.Errorf("pending queue changed during dispatch of %s", job.ID)
		}
		if err := c.jobManager.MarkInFlight(job.ID, deadline); err != nil {
			return nil, err
		}
		assigned := job
		assigned.Status = types.StatusInFlight
		deadlineMs := deadline.UnixMilli()
		assigned.Deadline = &deadlineMs
		jobs = append(jobs, &assigned)
	}

	return jobs, nil
}

// Acknowledge implements worker.JobSource.Acknowledge
// It processes the result from the worker.
func (c *Controller) Acknowledge(ctx context.Context, jobID string, status types.JobStatus, result *worker.Result) error {
	c.mu.Lock()
	replicated := c.raftNode != nil
	c.mu.Unlock()
	if replicated {
		return fmt.Errorf("replicated acknowledgement must be proposed through Raft")
	}
	// Reuse handleResult logic, but adaptable to interface
	// Since handleResult takes worker.Result (which contains status indirectly via Success bool),
	// we construct a result object if needed, or use the passed one.

	if result == nil {
		// Should not happen in current worker implementation, but handle gracefully
		return fmt.Errorf("result is nil")
	}

	c.handleResult(*result)
	return nil
}

// Heartbeat implements worker.JobSource.Heartbeat
// For local controller, this is a no-op as we manage workers directly via Pool.
func (c *Controller) Heartbeat(ctx context.Context, nodeID string, load int) error {
	return nil
}
