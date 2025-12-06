// ============================================================================
// Beaver-Raft Job Source Interface
// ============================================================================
//
// Package: internal/worker
// File: source.go
// Purpose: Defines the abstraction for fetching jobs and reporting results.
//
// Motivation:
//   To support both local (Phase 1) and distributed (Phase 2) modes, we need
//   to decouple the WorkerPool from the specific job origin.
//
//   - Local Mode: JobSource wraps the local JobManager and WAL.
//   - Distributed Mode: JobSource wraps a gRPC client to the Master node.
//
// ============================================================================

package worker

import (
	"context"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// JobSource defines the interface for fetching jobs and reporting status.
// This abstraction allows the WorkerPool to operate in both standalone and
// distributed modes without changing its core logic.
type JobSource interface {
	// Poll fetches a batch of pending jobs.
	// It is a blocking call (or returns empty if no jobs) that respects the context.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout.
	//   - maxJobs: Maximum number of jobs to fetch.
	//
	// Returns:
	//   - []*types.Job: A slice of fetched jobs.
	//   - error: Error if fetching fails.
	Poll(ctx context.Context, maxJobs int) ([]*types.Job, error)

	// Acknowledge reports the execution result of a job.
	//
	// Parameters:
	//   - ctx: Context for cancellation and timeout.
	//   - jobID: The unique identifier of the job.
	//   - status: The new status (Completed or Dead).
	//   - result: Optional result data or error message (can be nil).
	//
	// Returns:
	//   - error: Error if acknowledgment fails.
	Acknowledge(ctx context.Context, jobID string, status types.JobStatus, result *Result) error

	// Heartbeat sends a heartbeat to the registry (used in Distributed Mode).
	// In Local Mode, this can be a no-op.
	//
	// Parameters:
	//   - ctx: Context for cancellation.
	//   - nodeID: The unique identifier of this worker node.
	//   - load: Current load metric (e.g., active workers).
	//
	// Returns:
	//   - error: Error if heartbeat fails.
	Heartbeat(ctx context.Context, nodeID string, load int) error
}
