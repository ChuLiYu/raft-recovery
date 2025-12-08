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

	// Fetch pending jobs
	jobs := make([]*types.Job, 0, maxJobs)
	for i := 0; i < maxJobs; i++ {
		job := c.jobManager.PopPending()
		if job == nil {
			break
		}
		jobs = append(jobs, job)
	}

	if len(jobs) == 0 {
		return jobs, nil
	}

	// Process jobs: Write WAL and update state
	// Note: We are holding the lock, so this is safe but potentially blocking.
	// Optimization: In Phase 1's dispatchLoop, we batched WAL writes without lock.
	// Here, for simplicity and safety in the interface adapter, we hold the lock.
	// Ideally, we should release lock for WAL I/O, but that requires careful state management.
	
	deadline := time.Now().Add(c.config.TaskTimeout)

	for _, job := range jobs {
		// 1. Write WAL (Dispatch Event)
		if err := c.wal.Append(wal.EventDispatch, job); err != nil {
			log.Error("Failed to append DISPATCH event during Poll", "jobID", job.ID, "error", err)
			// If WAL fails, we shouldn't return this job to worker? 
			// For now, continue best effort.
		}

		// 2. Mark In-Flight
		if err := c.jobManager.MarkInFlight(job.ID, deadline); err != nil {
			log.Error("Failed to mark in-flight during Poll", "jobID", job.ID, "error", err)
		}
	}

	return jobs, nil
}

// Acknowledge implements worker.JobSource.Acknowledge
// It processes the result from the worker.
func (c *Controller) Acknowledge(ctx context.Context, jobID string, status types.JobStatus, result *worker.Result) error {
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