// ============================================================================
// Beaver-Raft Job Manager - State Machine Implementation
// ============================================================================
//
// Package: internal/jobmanager
// File: job_manager.go
// Purpose: Complete job lifecycle and state transition management
//
// Design Philosophy:
//   Hybrid design balancing performance and consistency:
//   1. jobs map - Unified storage as Single Source of Truth
//   2. State indexes - pending queue, inFlight/completed/dead maps for fast queries
//   3. Pointer-based synchronization ensures consistency
//
// Job State Machine:
//   Pending (Waiting)
//      ↓ PopPending() + MarkInFlight()
//   InFlight (Executing)
//      ↓ MarkCompleted() or timeout Requeue()
//   Completed / Dead (Failed)
//
// State Transitions:
//   - Pending → InFlight: via PopPending() + MarkInFlight()
//   - InFlight → Completed: via MarkCompleted()
//   - InFlight → Pending: via Requeue() (retry on failure)
//   - InFlight → Dead: via MarkDead() (max retries exceeded)
//
// Data Structure:
//   jobs map[JobID]*Job - Primary storage for all jobs
//   ├─ Job.Status field identifies current state
//   └─ Fast filtering by status field
//
//   Secondary indexes (performance boost):
//   - queue []JobID - pending queue, FIFO order
//   - inFlight map - executing jobs index
//   - completed map - finished jobs index
//   - dead map - failed jobs index
//
// Concurrency:
//   - sync.RWMutex protects all data structures
//   - RLock for reads, Lock for writes
//   - Safe for multi-goroutine access
//
// Snapshot Support:
//   - Snapshot() - Serialize current job state
//   - Restore() - Recover state from snapshot
//   - Enables crash recovery and system migration
//
// Responsibilities:
//   1. Unified state management (single jobs map)
//   2. State transition integrity (Pending -> InFlight -> Completed/Dead)
//   3. Job lifecycle management
//   4. Snapshot serialization/deserialization
//
// ============================================================================

package jobmanager

import (
	"errors"
	"sync"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ============================================================================
// Error Definitions
// ============================================================================

var (
	ErrDuplicateJob = errors.New("job already exists") // Duplicate job ID error
	ErrNotInFlight  = errors.New("job not in flight")  // Job not in executing state
	ErrJobNotFound  = errors.New("job not found")      // Job does not exist
)

// Status constants defined in pkg/types

// ============================================================================
// Data Structure Definitions
// ============================================================================

// Domain models defined in pkg/types

// JobManager manages job lifecycle using hybrid design for efficiency
type JobManager struct {
	mu        sync.RWMutex
	jobs      map[types.JobID]*types.Job // Unified storage, status differentiated by Status field
	queue     []types.JobID              // Pending queue
	inFlight  map[types.JobID]*types.Job // Executing jobs
	completed map[types.JobID]*types.Job // Completed jobs
	dead      map[types.JobID]*types.Job // Dead letter jobs
}

// SnapShotData contains complete job state for persistence
type SnapShotData struct {
	Jobs      map[types.JobID]*types.Job `json:"jobs"`           // Complete job data
	SchemaVer int                        `json:"schema_version"` // Schema version
}

// ============================================================================
// Core Methods
// ============================================================================

// NewJobManager creates a new job manager instance
//
// Returns:
//   - *JobManager: Initialized job manager
//
// Example:
//
//	jm := NewJobManager()
//	job := Job{ID: "task-001", Payload: map[string]interface{}{"key": "value"}}
//	err := jm.Enqueue(job)
//
// Concurrency: Returned instance is thread-safe
func NewJobManager() *JobManager {
	return &JobManager{
		jobs:      make(map[types.JobID]*types.Job),
		queue:     make([]types.JobID, 0),
		inFlight:  make(map[types.JobID]*types.Job),
		completed: make(map[types.JobID]*types.Job),
		dead:      make(map[types.JobID]*types.Job),
	}
}

// Enqueue adds a new job to the system in pending state
//
// Parameters:
//   - job: Job to add, must have unique ID
//
// Returns:
//   - error: ErrDuplicateJob if ID already exists
//
// Example:
//
//	job := Job{ID: "task-001", Payload: map[string]interface{}{"key": "value"}}
//	err := jm.Enqueue(job)
//	if err != nil {
//	    log.Printf("Failed to enqueue: %v", err)
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) Enqueue(job types.Job) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Check if job ID already exists
	if _, exists := jm.jobs[job.ID]; exists {
		return ErrDuplicateJob
	}

	// Set job status and timestamps
	now := time.Now().UnixMilli()
	job.Status = types.StatusPending
	job.CreatedAt = now
	job.UpdatedAt = now

	// Add to system
	jm.jobs[job.ID] = &job
	jm.queue = append(jm.queue, job.ID)
	return nil
}

// PopPending retrieves a pending job without changing its state
//
// Returns:
//   - *Job: First pending job pointer, nil if queue is empty
//
// Example:
//
//	job := state.PopPending()
//	if job != nil {
//	    log.Printf("Popped job: %s", job.ID)
//	    // Call MarkInFlight after processing to change state
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) PopPending() *types.Job {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if len(jm.queue) == 0 {
		return nil
	}

	jobID := jm.queue[0]
	jm.queue = jm.queue[1:] // Remove first element

	return jm.jobs[jobID] // O(1) lookup
}

// MarkInFlight marks a job as in-flight status and sets the deadline
//
// Parameters:
//   - jobID: ID of the job to mark
//   - deadline: Deadline for the job
//
// Returns:
//   - error: Returns error if job does not exist or status is incorrect
//
// Error handling:
//   - ErrJobNotFound: Job does not exist in the system
//   - Custom error: Job status is not StatusPending
//
// Example:
//
//	deadline := time.Now().Add(30 * time.Second)
//	err := state.MarkInFlight("task-001", deadline)
//	if err != nil {
//	    log.Printf("Failed to mark in-flight: %v", err)
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) MarkInFlight(jobID types.JobID, deadline time.Time) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Check if job exists
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// Check if job status is pending
	if job.Status != types.StatusPending {
		return errors.New("job not in pending status")
	}

	// Update job status
	deadlineMs := deadline.UnixMilli()
	job.Status = types.StatusInFlight
	job.Deadline = &deadlineMs
	job.UpdatedAt = time.Now().UnixMilli()

	// Add to inFlight set
	jm.inFlight[jobID] = job

	return nil
}

// MarkCompleted marks a job as completed status
//
// Parameters:
//   - jobID: ID of the job to mark as completed
//
// Returns:
//   - error: Returns error if job does not exist or status is incorrect
//
// Error handling:
//   - ErrJobNotFound: Job does not exist in the system
//   - ErrNotInFlight: Job is not in in-flight status
//
// Example:
//
//	err := state.MarkCompleted("task-001")
//	if err != nil {
//	    log.Printf("Failed to mark completed: %v", err)
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) MarkCompleted(jobID types.JobID) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Check if job exists
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// Check if job status is in-flight
	if job.Status != types.StatusInFlight {
		return ErrNotInFlight
	}

	// Update job status
	job.Status = types.StatusCompleted
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = time.Now().UnixMilli()

	// Remove from inFlight, add to completed
	delete(jm.inFlight, jobID)
	jm.completed[jobID] = job

	return nil
}

// Requeue requeues an in-flight job and increments retry count
//
// Parameters:
//   - jobID: ID of the job to requeue
//
// Returns:
//   - error: Returns error if job does not exist or status is incorrect
//
// Error handling:
//   - ErrJobNotFound: Job does not exist in the system
//   - ErrNotInFlight: Job is not in in-flight status
//
// Example:
//
//	err := state.Requeue("task-001")
//	if err != nil {
//	    log.Printf("Failed to requeue: %v", err)
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) Requeue(jobID types.JobID) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Check if job exists
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// Check if job status is in-flight
	if job.Status != types.StatusInFlight {
		return ErrNotInFlight
	}

	// Increment retry count and requeue
	job.Attempt++
	job.Status = types.StatusPending
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = time.Now().UnixMilli()

	// Remove from inFlight, add back to queue
	delete(jm.inFlight, jobID)
	jm.queue = append(jm.queue, jobID)

	return nil
}

// MarkDead marks a job as dead status (failed after exceeding retry limit)
//
// Parameters:
//   - jobID: ID of the job to mark as dead
//
// Returns:
//   - error: Returns error if job does not exist
//
// Example:
//
//	err := jm.MarkDead("task-001")
//	if err != nil {
//	    log.Printf("Failed to mark dead: %v", err)
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) MarkDead(jobID types.JobID) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Check if job exists
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// Update job status
	job.Status = types.StatusDead
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = time.Now().UnixMilli()

	// Remove from inFlight, add to dead
	delete(jm.inFlight, jobID)
	jm.dead[jobID] = job

	return nil
}

// GetExpiredJobs retrieves expired in-flight jobs
//
// Parameters:
//   - now: Current time
//
// Returns:
//   - []JobID: List of expired job IDs
//
// Example:
//
//	expiredJobs := jm.GetExpiredJobs(time.Now())
//	for _, jobID := range expiredJobs {
//	    log.Printf("Job %s has expired", jobID)
//	}
//
// Concurrency: Protected by read lock
func (jm *JobManager) GetExpiredJobs(now time.Time) []types.JobID {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	var expired []types.JobID
	nowMs := now.UnixMilli()

	for jobID, job := range jm.inFlight {
		if job.Deadline != nil && *job.Deadline < nowMs {
			expired = append(expired, jobID)
		}
	}

	return expired
}

// GetAllInFlightJobs retrieves all in-flight job IDs
//
// Returns:
//   - []types.JobID: List of all in-flight job IDs
//
// Purpose: Mainly used for rescheduling all in-flight jobs during recovery
//
// Concurrency: Protected by read lock
func (jm *JobManager) GetAllInFlightJobs() []types.JobID {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	var inFlightJobs []types.JobID
	for jobID := range jm.inFlight {
		inFlightJobs = append(inFlightJobs, jobID)
	}

	return inFlightJobs
}

// Stats retrieves statistics of jobs in each status
//
// Returns:
//   - map[string]int: Count of jobs in each status
//
// Example:
//
//	stats := jm.Stats()
//	log.Printf("Pending: %d, In-flight: %d, Completed: %d, Dead: %d",
//	    stats["pending"], stats["in_flight"], stats["completed"], stats["dead"])
//
// Concurrency: Protected by read lock
func (jm *JobManager) Stats() map[string]int {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	return map[string]int{
		"pending":   len(jm.queue),
		"in_flight": len(jm.inFlight),
		"completed": len(jm.completed),
		"dead":      len(jm.dead),
	}
}

// ============================================================================
// Snapshot and Restore Methods
// ============================================================================

// Restore restores state from snapshot
//
// Parameters:
//   - data: Snapshot data
//
// Returns:
//   - error: Error when restore fails
//
// Example:
//
//	data, _ := snapshot.Load()
//	err := jm.Restore(data)
//	if err != nil {
//	    log.Printf("Restore failed: %v", err)
//	}
//
// Concurrency: Protected by mutex
func (jm *JobManager) Restore(data types.SnapshotData) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// Clear existing state
	jm.jobs = make(map[types.JobID]*types.Job)
	jm.queue = make([]types.JobID, 0)
	jm.inFlight = make(map[types.JobID]*types.Job)
	jm.completed = make(map[types.JobID]*types.Job)
	jm.dead = make(map[types.JobID]*types.Job)

	// Restore all jobs
	for jobID, job := range data.Jobs {
		jm.jobs[jobID] = job

		// Categorize by status
		switch job.Status {
		case types.StatusPending:
			jm.queue = append(jm.queue, jobID)
		case types.StatusInFlight:
			jm.inFlight[jobID] = job
		case types.StatusCompleted:
			jm.completed[jobID] = job
		case types.StatusDead:
			jm.dead[jobID] = job
		}
	}

	return nil
}

// Snapshot generates snapshot data
//
// Returns:
//   - types.SnapshotData: Snapshot of current state
//
// Example:
//
//	data := jm.Snapshot()
//	snapshot.Write(data)
//
// Concurrency: Protected by read lock
func (jm *JobManager) Snapshot() types.SnapshotData {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	// Deep copy all jobs
	jobsCopy := make(map[types.JobID]*types.Job, len(jm.jobs))
	for id, job := range jm.jobs {
		jobCopy := *job
		jobsCopy[id] = &jobCopy
	}

	return types.SnapshotData{
		Jobs:      jobsCopy,
		SchemaVer: 1,
	}
}

// ============================================================================
// Query Methods
// ============================================================================

// IsCompleted checks if a job is completed
//
// Parameters:
//   - jobID: Job ID
//
// Returns:
//   - bool: Whether the job is completed
//
// Concurrency: Protected by read lock
func (jm *JobManager) IsCompleted(jobID types.JobID) bool {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	_, exists := jm.completed[jobID]
	return exists
}

// IsDead checks if a job is dead
//
// Parameters:
//   - jobID: Job ID
//
// Returns:
//   - bool: Whether the job is dead
//
// Concurrency: Protected by read lock
func (jm *JobManager) IsDead(jobID types.JobID) bool {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	_, exists := jm.dead[jobID]
	return exists
}

// GetJob retrieves a job
//
// Parameters:
//   - jobID: Job ID
//
// Returns:
//   - *types.Job: Job pointer, returns nil if not exists
//
// Concurrency: Protected by read lock
func (jm *JobManager) GetJob(jobID types.JobID) *types.Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.jobs[jobID]
}

// GetTotalJobs returns the total number of jobs
//
// Returns:
//   - int: Total job count
//
// Concurrency: Protected by read lock
func (jm *JobManager) GetTotalJobs() int {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return len(jm.jobs)
}
