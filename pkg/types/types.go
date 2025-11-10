// ============================================================================
// Beaver-Raft Core Type Definitions
// ============================================================================
//
// Package: pkg/types
// Purpose: Core domain models and data structures
//
// Design Principles:
//   1. Domain-Driven Design (DDD) - Business concepts as types
//   2. Type Safety - Custom types prevent primitive obsession
//   3. JSON Serialization - Full serialization support
//   4. Backward Compatibility - Schema versioning
//
// Core Types:
//   - Job: Task entity with full lifecycle tracking
//   - JobStatus: State enum (pending/in_flight/completed/dead)
//   - InFlightInfo: Execution tracking for running jobs
//   - SnapshotData: System state snapshot structure
//
// Usage:
//   - JobManager: Task state management
//   - Controller: Job scheduling and dispatching
//   - Snapshot: State persistence and recovery
//   - WAL: Operation logging
//
// Timestamps:
//   Unix milliseconds for cross-platform compatibility,
//   precise timeout calculations, and JSON portability
//
// ============================================================================

// Package types defines core domain models for the beaver-raft system
package types

import (
	"time"
)

// JobID uniquely identifies a job
type JobID string

// JobStatus represents job execution state
type JobStatus string

// Job status constants
const (
	StatusPending   JobStatus = "pending"   // Job created but not yet started
	StatusInFlight  JobStatus = "in_flight" // Job currently being processed by worker
	StatusCompleted JobStatus = "completed" // Job successfully completed
	StatusDead      JobStatus = "dead"      // Job failed or timed out
)

// Job represents a unit of work in the system
type Job struct {
	// Identification and data
	ID      JobID                  `json:"id"`      // Unique job identifier
	Payload map[string]interface{} `json:"payload"` // Job execution data

	// State tracking
	Status  JobStatus `json:"status"`  // Current job state
	Attempt int       `json:"attempt"` // Retry count

	// Time management (Unix milliseconds)
	Timeout   time.Duration `json:"timeout"`               // Job execution timeout
	Deadline  *int64        `json:"deadline_ms,omitempty"` // Job deadline (Unix ms)
	CreatedAt int64         `json:"created_at"`            // Job creation time (Unix ms)
	UpdatedAt int64         `json:"updated_at"`            // Last update time (Unix ms)

	// Execution info
	WorkerID string `json:"worker_id,omitempty"` // Worker handling this job
}

// InFlightInfo tracks execution state for running jobs
type InFlightInfo struct {
	JobID     JobID `json:"job_id"`      // Job being executed
	WorkerID  int   `json:"worker_id"`   // Worker executing the job
	Deadline  int64 `json:"deadline_ms"` // Execution deadline (Unix ms)
	StartedAt int64 `json:"started_at"`  // Execution start time (Unix ms)
}

// SnapshotData contains system state for persistence and recovery
type SnapshotData struct {
	Jobs      map[JobID]*Job `json:"jobs"`       // Complete job data
	SchemaVer int            `json:"schema_ver"` // Schema version for compatibility
	LastSeq   uint64         `json:"last_seq"`   // Last processed sequence number
}
