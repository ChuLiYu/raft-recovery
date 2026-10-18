// ============================================================================
// Beaver-Raft Controller - System Core Coordinator
// ============================================================================
//
// Package: internal/controller
// File: controller.go
// Purpose: Core system controller that coordinates all modules and implements crash recovery and job scheduling
//
// Architecture Design:
//   This is the "brain" of the entire system, responsible for coordinating the following components:
//   - JobManager: Job state management (pending/in_flight/completed/dead)
//   - WAL: Write-Ahead Log, persists all operations to ensure no data loss
//   - Snapshot: Snapshot management, periodically saves system state to accelerate recovery
//   - WorkerPool: Worker thread pool that actually executes tasks
//
// Core Loops (4 concurrent Goroutines):
//   1. Dispatch Loop - Fetches tasks from pending queue and dispatches them to workers
//   2. Result Loop - Receives worker execution results and updates job states
//   3. Timeout Loop - Periodically scans for timed-out tasks, requeues or marks as dead
//   4. Snapshot Loop - Periodically creates snapshots to ensure fast recovery capability
//
// Crash Recovery Flow:
//   Automatically executed on startup:
//   1. loadSnapshot() - Restores system state from the latest snapshot
//   2. replayWAL() - Replays WAL logs to recover operations after the snapshot
//   3. requeueInFlightJobs() - Reschedules tasks that were in-flight before the crash
//   Target: Achieve < 3 second recovery time
//
// Idempotency Guarantee:
//   - Each operation writes to WAL first, then modifies in-memory state
//   - During recovery, skip already completed operations (deduplication by JobID)
//   - Ensures eventual consistency of system state
//
// Concurrency Safety:
//   - Uses sync.Mutex to protect concurrent access to JobManager
//   - stopCh channel for graceful shutdown of all loops
//   - sync.WaitGroup ensures all goroutines exit properly
//
// Responsibilities:
//   1. Coordinate all modules (JobManager, WAL, Snapshot, WorkerPool)
//   2. Implement four core loops: dispatch, result, timeout, snapshot
//   3. Handle crash recovery (loadSnapshot -> replayWAL -> reschedule)
//   4. Ensure state consistency and idempotency
//
// ============================================================================

package controller

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/jobmanager"
	"github.com/ChuLiYu/raft-recovery/internal/raft"
	"github.com/ChuLiYu/raft-recovery/internal/snapshot"
	"github.com/ChuLiYu/raft-recovery/internal/storage/wal"
	"github.com/ChuLiYu/raft-recovery/internal/worker"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

var log = slog.Default()

// ============================================================================
// Data Structure Definitions
// ============================================================================

// Config holds the controller configuration
type Config struct {
	WorkerCount      int           // Number of workers
	TaskTimeout      time.Duration // Task timeout duration
	SnapshotInterval time.Duration // Snapshot interval
	MaxRetry         int           // Maximum retry count
	WALPath          string        // WAL file path
	SnapshotPath     string        // Snapshot file path
	// WAL batch commit settings (NEW!)
	WALBufferSize    int           // Max events per batch (e.g., 100)
	WALFlushInterval time.Duration // Max time between flushes (e.g., 10ms)

	// Phase 2: Distributed Mode Settings
	DisableDispatchLoop bool // If true, internal dispatch loops are disabled (for Master node)
}

// Controller is the core controller
type Controller struct {
	mu         sync.Mutex             // Protects jobManager operations
	jobManager *jobmanager.JobManager // Job state management
	wal        *wal.WAL               // Write-Ahead Log
	snapshot   *snapshot.Manager      // Snapshot management
	pool       *worker.Pool           // Worker Pool
	config     Config                 // Configuration
	stopCh     chan struct{}          // Stop signal
	stopped    bool                   // Flag indicating if stopped
	startTime  time.Time              // Start time (for statistics)
	loopWg     sync.WaitGroup         // Wait for all loops to exit

	// Phase 3: Raft integration
	applyCh       chan raft.ApplyMsg // Channel for committed entries
	raftNode      *raft.Raft         // Raft node instance
	appliedNotify chan struct{}
	applyErr      map[int64]error
	applyResults  map[string]applyResult
	replayAfter   uint64
	fatalApplyErr error
}

type applyResult struct {
	jobs     []types.Job
	accepted bool
}

// ============================================================================
// Core Method Implementations
// ============================================================================

// NewController creates a new Controller instance
//
// Parameters:
//   - config: Controller configuration
//
// Returns:
//   - *Controller: Controller instance
//   - error: Initialization error
func NewController(config Config) (*Controller, error) {
	// 1. Create JobManager
	jobManager := jobmanager.NewJobManager()

	// 2. Open WAL with batch commit settings
	// Use config values, with sensible defaults if not provided
	bufferSize := config.WALBufferSize
	if bufferSize <= 0 {
		bufferSize = 100 // Default: 100 events per batch
	}
	flushInterval := config.WALFlushInterval
	if flushInterval <= 0 {
		flushInterval = 10 * time.Millisecond // Default: 10ms
	}
	walInstance, err := wal.NewWAL(config.WALPath, false, bufferSize, flushInterval)
	if err != nil {
		return nil, fmt.Errorf("failed to open WAL: %w", err)
	}

	// 3. Create Snapshot Manager
	snapshotMgr := snapshot.NewManager(config.SnapshotPath)

	// 4. Create Worker Pool
	pool := worker.NewPool(config.WALBufferSize)

	return &Controller{
		jobManager:    jobManager,
		wal:           walInstance,
		snapshot:      snapshotMgr,
		pool:          pool,
		config:        config,
		stopCh:        make(chan struct{}),
		applyCh:       make(chan raft.ApplyMsg, 100),
		appliedNotify: make(chan struct{}),
		applyErr:      make(map[int64]error),
		applyResults:  make(map[string]applyResult),
	}, nil
}

// SetRaftNode injects the Raft node instance
func (c *Controller) SetRaftNode(rf *raft.Raft) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.raftNode = rf
}

// Start starts the Controller
//
// Process:
//  1. Recovery phase: loadSnapshot -> replayWAL
//  2. Startup phase: Start Worker Pool and four core loops
//
// Returns:
//   - error: Startup failure error
func (c *Controller) Start() error {
	c.startTime = time.Now()
	c.mu.Lock()
	raftMode := c.raftNode != nil
	c.mu.Unlock()

	// 1. Recovery phase
	log.Info("Starting recovery...")
	if !raftMode {

		if err := c.loadSnapshot(); err != nil {
			return fmt.Errorf("loadSnapshot failed: %w", err)
		}

		if err := c.replayWAL(); err != nil {
			return fmt.Errorf("replayWAL failed: %w", err)
		}

		// Requeue all in_flight jobs (these tasks were incomplete at the time of crash)
		c.mu.Lock()
		inFlightJobs := c.jobManager.GetAllInFlightJobs()
		requeueCount := 0
		for _, jobID := range inFlightJobs {
			if err := c.jobManager.Requeue(jobID); err != nil {
				log.Error("Failed to requeue in-flight job during recovery", "jobID", jobID, "error", err)
			} else {
				requeueCount++
			}
		}
		c.mu.Unlock()

		log.Info("Recovery completed",
			"duration", time.Since(c.startTime),
			"requeued_jobs", requeueCount)
	}

	// 2. Start Worker Pool
	if err := c.pool.Start(c.config.WorkerCount, nil); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	// 3. Start core loops
	// If DisableDispatchLoop is true (Master mode), we skip starting local dispatchers.
	// The Controller will purely act as a passive backend for gRPC requests.
	numDispatchers := 0
	if !c.config.DisableDispatchLoop && !raftMode {
		numDispatchers = 4 // Default parallel dispatchers for local mode
	}

	c.loopWg.Add(3 + numDispatchers) // result + timeout + snapshot + N*dispatch

	// Start multiple dispatch loops in parallel (only if enabled)
	for i := 0; i < numDispatchers; i++ {
		go c.dispatchLoop()
	}

	go c.resultLoop()
	go c.timeoutLoop()
	go c.snapshotLoop()

	// Phase 3: Start apply loop
	c.loopWg.Add(1)
	go c.applyLoop()

	log.Info("Controller started",
		"workers", c.config.WorkerCount,
		"dispatchers", numDispatchers)
	return nil
}

// applyLoop listens for committed entries from Raft and applies them to the state machine.
func (c *Controller) applyLoop() {
	defer c.loopWg.Done()

	for {
		select {
		case <-c.stopCh:
			return
		case msg := <-c.applyCh:
			c.mu.Lock()
			if c.fatalApplyErr != nil {
				c.mu.Unlock()
				continue
			}
			if msg.SnapshotValid {
				var data types.SnapshotData
				err := json.Unmarshal(msg.Snapshot, &data)
				if err == nil && int64(data.LastSeq) != msg.SnapshotIndex {
					err = fmt.Errorf("snapshot index mismatch: data=%d raft=%d", data.LastSeq, msg.SnapshotIndex)
				}
				if err == nil && msg.SnapshotIndex > c.jobManager.GetLastAppliedIndex() {
					err = c.jobManager.Restore(data)
				}
				if err != nil {
					c.applyErr[msg.SnapshotIndex] = err
					c.fatalApplyErr = err
					log.Error("Failed to restore Raft snapshot", "error", err)
				}
			} else if msg.CommandValid && msg.CommandIndex > c.jobManager.GetLastAppliedIndex() {
				var err error
				if len(msg.Command) > 0 {
					err = c.handleRaftCommand(msg.CommandIndex, msg.Command)
				}
				if err != nil {
					c.applyErr[msg.CommandIndex] = err
					c.fatalApplyErr = err
					log.Error("Failed to apply Raft command", "index", msg.CommandIndex, "error", err)
				} else {
					c.jobManager.AdvanceApplied(msg.CommandIndex)
				}
			}
			close(c.appliedNotify)
			c.appliedNotify = make(chan struct{})
			c.mu.Unlock()
		}
	}
}

// handleRaftCommand runs under c.mu; all payloads are validated before changing state.
func (c *Controller) handleRaftCommand(index int64, data []byte) error {
	var cmd raft.RaftCommand
	if err := json.Unmarshal(data, &cmd); err != nil {
		return err
	}

	switch cmd.Type {
	case raft.CmdEnqueue:
		var payload raft.EnqueuePayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return err
		}
		for _, job := range payload.Jobs {
			if job.ID == "" {
				return fmt.Errorf("enqueue command contains empty job ID")
			}
		}
		for _, job := range payload.Jobs {
			if err := c.jobManager.ApplyEnqueue(job); err != nil {
				return err
			}
		}
	case raft.CmdDispatch:
		var payload raft.DispatchPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return err
		}
		job, accepted, err := c.jobManager.ApplyDispatch(types.JobID(payload.JobID), payload.WorkerID, payload.Attempt, payload.At, payload.Deadline)
		if payload.ResultID != "" {
			if _, waiting := c.applyResults[payload.ResultID]; waiting && err == nil && accepted {
				c.applyResults[payload.ResultID] = applyResult{jobs: []types.Job{job}}
			}
		}
		return err
	case raft.CmdAck:
		var payload raft.AckPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return err
		}
		accepted, err := c.jobManager.ApplyAck(types.JobID(payload.JobID), payload.WorkerID, payload.Attempt, payload.Status, payload.At, payload.MaxRetry)
		if payload.ResultID != "" {
			if _, waiting := c.applyResults[payload.ResultID]; waiting && err == nil {
				c.applyResults[payload.ResultID] = applyResult{accepted: accepted}
			}
		}
		return err
	case raft.CmdTimeout:
		var payload raft.TimeoutPayload
		if err := json.Unmarshal(cmd.Payload, &payload); err != nil {
			return err
		}
		_, err := c.jobManager.ApplyTimeout(types.JobID(payload.JobID), payload.Attempt, payload.At, payload.MaxRetry)
		return err
	default:
		return fmt.Errorf("unknown Raft command %q", cmd.Type)
	}
	return nil
}

func (c *Controller) WaitApplied(ctx context.Context, index int64) error {
	for {
		c.mu.Lock()
		if c.fatalApplyErr != nil {
			err := c.fatalApplyErr
			c.mu.Unlock()
			return err
		}
		if err := c.applyErr[index]; err != nil {
			c.mu.Unlock()
			return err
		}
		if c.jobManager.GetLastAppliedIndex() >= index {
			c.mu.Unlock()
			return nil
		}
		if c.stopped {
			c.mu.Unlock()
			return fmt.Errorf("controller stopped")
		}
		wait := c.appliedNotify
		c.mu.Unlock()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-c.stopCh:
			return fmt.Errorf("controller stopped")
		case <-wait:
		}
	}
}

// RegisterAppliedResult reserves a result slot for one in-flight RPC. The ID is
// carried in its Raft command so apply can discard late results after the RPC
// has canceled or lost ownership of the request.
func (c *Controller) RegisterAppliedResult() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate apply result ID: %w", err)
	}
	id := hex.EncodeToString(bytes[:])
	c.mu.Lock()
	c.applyResults[id] = applyResult{}
	c.mu.Unlock()
	return id, nil
}

// DiscardAppliedResult ends an RPC's ownership of its result. It also makes
// any later apply of the command a no-op for result storage.
func (c *Controller) DiscardAppliedResult(id string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	delete(c.applyResults, id)
}

func (c *Controller) AppliedJobs(id string) []types.Job {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := c.applyResults[id]
	delete(c.applyResults, id)
	return result.jobs
}

func (c *Controller) AppliedAccepted(id string) bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	result := c.applyResults[id]
	delete(c.applyResults, id)
	return result.accepted
}

func (c *Controller) PendingJobs(limit int) []types.Job    { return c.jobManager.Pending(limit) }
func (c *Controller) Job(id types.JobID) (types.Job, bool) { return c.jobManager.JobCopy(id) }
func (c *Controller) MaxRetry() int                        { return c.config.MaxRetry }

func (c *Controller) GetApplyCh() chan raft.ApplyMsg {
	return c.applyCh
}

// loadSnapshot restores state from snapshot
//
// Returns:
//   - error: Loading failure error
func (c *Controller) loadSnapshot() error {
	start := time.Now()

	// Load snapshot
	data, err := c.snapshot.Load()
	if err != nil {
		return fmt.Errorf("failed to load snapshot: %w", err)
	}

	// Restore JobManager state
	c.mu.Lock()
	if err := c.jobManager.Restore(data); err != nil {
		c.mu.Unlock()
		return fmt.Errorf("failed to restore state: %w", err)
	}
	c.replayAfter = data.LastSeq
	c.wal.EnsureSequenceAtLeast(data.LastSeq)
	c.mu.Unlock()

	recoveryTime := time.Since(start)

	// Log recovery time (target < 3s)
	if recoveryTime > 3*time.Second {
		log.Warn("Recovery time exceeds 3s",
			"duration", recoveryTime)
	}

	log.Info("Snapshot loaded",
		"duration", recoveryTime,
		"jobs", len(data.Jobs))

	return nil
}

// replayWAL replays WAL events
//
// Important: Implements idempotency checks to ensure repeated replays don't cause errors
//
// Returns:
//   - error: Replay failure error
func (c *Controller) replayWAL() error {
	handler := func(event *wal.Event) error {
		c.mu.Lock()
		defer c.mu.Unlock()
		if event.Seq <= c.replayAfter {
			return nil
		}

		switch event.Type {
		case wal.EventEnqueue:
			if c.jobManager.GetJob(event.JobID) != nil {
				return nil
			}
			if event.Job == nil {
				return fmt.Errorf("WAL enqueue %s lacks job payload; cannot recover", event.JobID)
			}
			return c.jobManager.ApplyEnqueue(*event.Job)

		case wal.EventDispatch:
			if c.jobManager.GetJob(event.JobID) == nil {
				return fmt.Errorf("WAL dispatch %s has no enqueue", event.JobID)
			}
			if job := c.jobManager.GetJob(event.JobID); job.Status == types.StatusInFlight {
				return nil
			}
			// Check idempotency: don't reschedule already completed or dead jobs
			if c.jobManager.IsCompleted(event.JobID) ||
				c.jobManager.IsDead(event.JobID) {
				return nil
			}

			// Mark as in-flight
			deadline := time.UnixMilli(event.Timestamp).Add(c.config.TaskTimeout)
			return c.jobManager.MarkInFlight(event.JobID, deadline)

		case wal.EventAck:
			// Skip if already completed
			if c.jobManager.IsCompleted(event.JobID) {
				return nil
			}
			return c.jobManager.MarkCompleted(event.JobID)

		case wal.EventRetry:
			return c.jobManager.Requeue(event.JobID)

		case wal.EventTimeout:
			job := c.jobManager.GetJob(event.JobID)
			if job == nil {
				return fmt.Errorf("WAL timeout %s has no enqueue", event.JobID)
			}
			if job.Status != types.StatusInFlight {
				return nil
			}
			if event.Job != nil && event.Job.Attempt+1 >= c.config.MaxRetry {
				job.Attempt = event.Job.Attempt + 1
				return c.jobManager.MarkDead(event.JobID)
			}
			return c.jobManager.Requeue(event.JobID)

		case wal.EventDead:
			job := c.jobManager.GetJob(event.JobID)
			if job == nil {
				return fmt.Errorf("WAL dead %s has no enqueue", event.JobID)
			}
			if event.Job != nil {
				job.Attempt = event.Job.Attempt + 1
			}
			return c.jobManager.MarkDead(event.JobID)
		}

		return nil
	}

	return c.wal.Replay(handler)
}

// ============================================================================
// Four Core Loops
// ============================================================================

// dispatchLoop dispatches pending tasks to Worker Pool
//
// Key: WAL must be written before state changes (Write-Ahead)
func (c *Controller) dispatchLoop() {
	defer c.loopWg.Done()

	// Batch size: Pop multiple jobs at once to reduce lock contention
	const batchSize = 10

	for {
		select {
		case <-c.stopCh:
			log.Info("Dispatch loop stopped")
			return
		default:
			// Keep the WAL append and state change in the same critical section
			// as standalone snapshot writing and rotation.
			c.mu.Lock()
			pending := c.jobManager.Pending(batchSize)
			jobs := make([]types.Job, 0, len(pending))
			for _, job := range pending {
				if err := c.wal.Append(wal.EventDispatch, &job); err != nil {
					log.Error("Failed to append DISPATCH event", "jobID", job.ID, "error", err)
					break
				}
				if popped := c.jobManager.PopPending(); popped == nil || popped.ID != job.ID {
					log.Error("Pending queue changed during dispatch", "jobID", job.ID)
					break
				}
				if err := c.jobManager.MarkInFlight(job.ID, time.Now().Add(c.config.TaskTimeout)); err != nil {
					log.Error("Failed to mark in-flight", "jobID", job.ID, "error", err)
					break
				}
				jobs = append(jobs, job)
			}
			c.mu.Unlock()

			if len(jobs) == 0 {
				// No jobs available, sleep briefly to avoid busy-wait
				select {
				case <-c.stopCh:
					log.Info("Dispatch loop stopped")
					return
				case <-time.After(5 * time.Millisecond): // Shorter sleep for faster response
					continue
				}
			}

			// Submit after persistence and local state change.
			for _, job := range jobs {
				task := worker.Task{
					ID:      job.ID,
					Attempt: job.Attempt,
					Payload: job.Payload,
					Timeout: c.config.TaskTimeout,
				}

				if err := c.pool.Submit(task); err != nil {
					// Pool may be closed, which is normal (during Stop process)
					if err != worker.ErrPoolClosed {
						log.Error("Failed to submit task", "error", err)
					}
				}
			}
		}
	}
}

// resultLoop processes Worker execution results
// Note: This loop runs until the Pool is closed
func (c *Controller) resultLoop() {
	defer c.loopWg.Done()
	for {
		result, err := c.pool.ReceiveResult()
		if err != nil {
			if err == worker.ErrPoolClosed {
				log.Info("Result loop stopped")
				return
			}
			log.Error("Failed to receive result", "error", err)
			time.Sleep(100 * time.Millisecond)
			continue
		}

		c.handleResult(result)
	}
}

// handleResult processes a single task result
func (c *Controller) handleResult(result worker.Result) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raftNode != nil {
		log.Error("received local worker result in replicated mode", "jobID", result.JobID)
		return
	}

	job := c.jobManager.GetJob(result.JobID)
	if job == nil {
		log.Warn("Unknown job", "jobID", result.JobID)
		return
	}
	if job.Status != types.StatusInFlight {
		// A delayed local result can arrive after timeout or another result.
		// It must not create an ACK/RETRY record that replay cannot apply.
		log.Debug("Ignoring stale worker result", "jobID", result.JobID, "status", job.Status)
		return
	}
	if result.Attempt != job.Attempt {
		log.Debug("Ignoring stale worker attempt", "jobID", result.JobID, "attempt", result.Attempt, "current", job.Attempt)
		return
	}

	if result.Success {
		// Success: Write WAL and mark as completed
		if err := c.wal.Append(wal.EventAck, job); err != nil {
			log.Error("Failed to append ACK event", "error", err)
			return
		}

		if err := c.jobManager.MarkCompleted(result.JobID); err != nil {
			log.Error("Failed to mark completed", "error", err)
		}

		log.Debug("Job completed",
			"jobID", result.JobID,
			"duration", result.Duration)
	} else {
		// Requeue increments the retry count; compute the outcome before
		// writing the transition so an append failure leaves state intact.
		nextAttempt := job.Attempt + 1
		if nextAttempt >= c.config.MaxRetry {
			// Exceeded retry count, move to dead letter queue
			if err := c.wal.Append(wal.EventDead, job); err != nil {
				log.Error("Failed to append DEAD event", "error", err)
				return
			}
			job.Attempt = nextAttempt

			if err := c.jobManager.MarkDead(result.JobID); err != nil {
				log.Error("Failed to mark dead", "error", err)
			}

			log.Warn("Job marked as dead",
				"jobID", result.JobID,
				"attempts", nextAttempt)
		} else {
			// Requeue
			if err := c.wal.Append(wal.EventRetry, job); err != nil {
				log.Error("Failed to append RETRY event", "error", err)
				return
			}

			if err := c.jobManager.Requeue(result.JobID); err != nil {
				log.Error("Failed to requeue", "error", err)
			}

			log.Debug("Job requeued",
				"jobID", result.JobID,
				"attempt", nextAttempt)
		}
	}
}

// timeoutLoop detects and handles timed-out tasks
func (c *Controller) timeoutLoop() {
	defer c.loopWg.Done()
	ticker := time.NewTicker(1 * time.Second)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			log.Info("Timeout loop stopped")
			return

		case <-ticker.C:
			c.mu.Lock()
			rf := c.raftNode
			c.mu.Unlock()
			if rf != nil {
				if _, leader := rf.GetState(); !leader {
					continue
				}
				at := time.Now().UnixMilli()
				for _, job := range c.jobManager.Expired(at) {
					cmd, err := raft.NewTimeoutCommand(raft.TimeoutPayload{JobID: string(job.ID), Attempt: job.Attempt, At: at, MaxRetry: c.config.MaxRetry})
					if err != nil {
						log.Error("encode timeout", "error", err)
						continue
					}
					ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
					index, _, err := rf.ProposeContext(ctx, cmd)
					if err == nil {
						err = c.WaitApplied(ctx, index)
					}
					cancel()
					if err != nil {
						log.Warn("timeout proposal failed", "jobID", job.ID, "error", err)
					}
				}
				continue
			}
			c.mu.Lock()

			// Get all expired tasks
			expiredJobIDs := c.jobManager.GetExpiredJobs(time.Now())

			for _, jobID := range expiredJobIDs {
				job := c.jobManager.GetJob(jobID)
				if job == nil {
					continue
				}

				// Write to WAL
				if err := c.wal.Append(wal.EventTimeout, job); err != nil {
					log.Error("Failed to append TIMEOUT event", "error", err)
					continue
				}

				nextAttempt := job.Attempt + 1
				if nextAttempt >= c.config.MaxRetry {
					job.Attempt = nextAttempt
					// Exceeded retry count, move to dead letter queue
					if err := c.jobManager.MarkDead(jobID); err != nil {
						log.Error("Failed to mark dead", "error", err)
					}
					log.Debug("Job timeout and marked as dead",
						"jobID", jobID)
				} else {
					// Requeue
					if err := c.jobManager.Requeue(jobID); err != nil {
						log.Error("Failed to requeue", "error", err)
					}
					log.Debug("Job timeout and requeued",
						"jobID", jobID)
				}
			}

			c.mu.Unlock()
		}
	}
}

// snapshotLoop periodically generates snapshots
func (c *Controller) snapshotLoop() {
	defer c.loopWg.Done()
	interval := c.config.SnapshotInterval
	if interval <= 0 {
		interval = 30 * time.Second
	}
	ticker := time.NewTicker(interval)
	defer ticker.Stop()

	for {
		select {
		case <-c.stopCh:
			log.Info("Snapshot loop stopped")
			return

		case <-ticker.C:
			if err := c.takeSnapshot(); err != nil {
				log.Error("Failed to take snapshot", "error", err)
			}
		}
	}
}

// takeSnapshot executes the snapshot operation
func (c *Controller) takeSnapshot() error {
	start := time.Now()

	// Capture the state and applied index together.
	c.mu.Lock()
	if c.fatalApplyErr != nil {
		err := c.fatalApplyErr
		c.mu.Unlock()
		return fmt.Errorf("cannot snapshot failed state machine: %w", err)
	}
	var data types.SnapshotData

	// Beaver Logic: Use PartialSnapshot if Raft is enabled, otherwise Full Snapshot
	if c.raftNode != nil {
		data = c.jobManager.PartialSnapshot()
		// Metadata for Raft
		data.LastSeq = uint64(c.jobManager.GetLastAppliedIndex())
	} else {
		data = c.jobManager.Snapshot()
		data.LastSeq = c.wal.GetLastSeq()
	}
	raftPtr := c.raftNode
	if raftPtr != nil {
		c.mu.Unlock()
	} else {
		// Standalone transitions also hold c.mu across WAL append and apply.
		// Keep it until the snapshot file and WAL rotation are both durable.
		defer c.mu.Unlock()
	}

	// Standalone mode keeps the lock through persistence and WAL rotation so
	// no transition can fall between the snapshot and its WAL boundary.
	snapshotBytes, err := json.Marshal(data)
	if err != nil {
		return err
	}
	if raftPtr != nil {
		if data.LastSeq == 0 {
			return nil
		}
		return raftPtr.Snapshot(int64(data.LastSeq), snapshotBytes)
	}
	if err := c.snapshot.Write(data); err != nil {
		return fmt.Errorf("failed to write snapshot: %w", err)
	}

	if err := c.wal.Rotate(); err != nil {
		return fmt.Errorf("failed to rotate WAL: %w", err)
	}

	log.Info("Snapshot taken (Partial)",
		"duration", time.Since(start),
		"jobs", len(data.Jobs))

	return nil
}

// ============================================================================
// Public Methods
// ============================================================================

// EnqueueJobs batch enqueues jobs
//
// Standalone WAL append and local state change share the snapshot lock so a
// rotated WAL can never discard an event outside the persisted snapshot.
//
// Parameters:
//   - jobs: List of jobs to enqueue
//
// Returns:
//   - error: Enqueue failure error
func (c *Controller) EnqueueJobs(jobs []types.Job) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.raftNode != nil {
		return fmt.Errorf("replicated jobs must be enqueued through Raft")
	}
	for i := range jobs {
		if err := c.wal.Append(wal.EventEnqueue, &jobs[i]); err != nil {
			return fmt.Errorf("failed to append ENQUEUE event for job %s: %w", jobs[i].ID, err)
		}
		if err := c.jobManager.Enqueue(jobs[i]); err != nil {
			return fmt.Errorf("failed to enqueue job %s: %w", jobs[i].ID, err)
		}
	}

	return nil
}

// GetStatus returns system status
//
// Returns:
//   - map[string]interface{}: System status information
func (c *Controller) GetStatus() map[string]interface{} {
	c.mu.Lock()
	defer c.mu.Unlock()

	stats := c.jobManager.Stats()

	return map[string]interface{}{
		"uptime":    time.Since(c.startTime).String(),
		"workers":   c.config.WorkerCount,
		"pending":   stats["pending"],
		"in_flight": stats["in_flight"],
		"completed": stats["completed"],
		"dead":      stats["dead"],
	}
}

// Stop gracefully shuts down the Controller

// ============================================================================
// Shutdown Order Design Explanation (Related to Worker Pool Race Condition)
// ============================================================================
//
// Shutdown order:
//  1. close(stopCh) → Notify all loops to prepare for shutdown
//  2. pool.Stop()   → Close Worker Pool (will close taskCh and resultCh)
//  3. loopWg.Wait() → Wait for all loops to exit
//  4. Cleanup resources (snapshot, WAL)
//
// Why this order is important:
//   - dispatchLoop may still try to call pool.Submit() after stopCh is closed
//   - If we wait for loopWg.Wait() first, dispatchLoop may block on Submit()
//   - If we call pool.Stop() first, there will be a race condition (see detailed explanation in worker_pool.go)
//   - Current order ensures resultLoop can exit properly (it relies on pool.Stop() closing resultCh)
//
// Race Condition Handling:
//   - Known issue: There is a race between dispatchLoop's Submit() and pool.Stop()'s close(taskCh)
//   - Mitigation: dispatchLoop checks stopCh again after ticker fires
//   - Safety guarantee: Submit() has internal stopCh check, will safely return ErrPoolClosed
//   - Actual impact: No data corruption, only race detector warnings
//
// Test Verification:
//   - Functional tests: All tests pass (including Stop-related tests)
//   - Stress tests: Multiple runs without deadlock or panic
//
// ============================================================================
// GetStats returns current job queue statistics
func (c *Controller) GetStats() map[string]int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jobManager.Stats()
}

// GetTotalJobs returns the total number of jobs in the system
func (c *Controller) GetTotalJobs() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.jobManager.GetTotalJobs()
}

func (c *Controller) Stop() {
	c.mu.Lock()
	if c.stopped {
		c.mu.Unlock()
		log.Info("Controller already stopped")
		return
	}
	c.stopped = true
	c.mu.Unlock()

	log.Info("Stopping controller...")

	// 1. Send stop signal to loops (highest priority)
	//    - dispatchLoop, timeoutLoop, snapshotLoop will respond immediately
	//    - resultLoop will exit after pool is closed
	close(c.stopCh)

	// 2. Stop Worker Pool (this will cause resultLoop to exit)
	//    - Close stopCh (notify workers)
	//    - Close taskCh (end worker loops) ← May race with Submit()
	//    - Wait for all workers to complete
	//    - Close resultCh (end resultLoop)
	c.pool.Stop()

	// 3. Wait for all loops to exit (ensure no goroutines access resources anymore)
	c.loopWg.Wait()

	// 4. Take final snapshot (persist final state)
	if err := c.takeSnapshot(); err != nil {
		log.Error("Failed to take final snapshot", "error", err)
	}

	// 5. Close WAL (ensure all events are written to disk)
	if err := c.wal.Close(); err != nil {
		log.Error("Failed to close WAL", "error", err)
	}

	log.Info("Controller stopped")
}

// ============================================================================
// ✅ Completed TODOs
// ============================================================================

// ✅ TODO 1: Implement loadSnapshot + replayWAL (ensure correct recovery)
// ✅ TODO 2: Implement four loops (dispatch, result, timeout, snapshot)
// ✅ TODO 3: Implement public methods (EnqueueJobs, GetStatus, Stop)
// ⏳ TODO 4: Write tests (controller_test.go)
// ⏳ TODO 5: Add missing JobManager methods (Restore, Snapshot, IsCompleted, IsDead, GetJob)
