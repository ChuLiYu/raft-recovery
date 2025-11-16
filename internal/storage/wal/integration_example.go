package wal

// ============================================================================
// WAL Integration Example (For reference only, not compilable)
// Demonstrates how Controller uses the WAL module
// ============================================================================

/*

// ============================================================================
// Example 1: Restore state during Controller initialization
// ============================================================================

type Controller struct {
    wal   *WAL
    state *State
    // ... other fields
}

func NewController(walPath string) (*Controller, error) {
    // 1. Create WAL instance
    wal, err := NewWAL(walPath)
    if err != nil {
        return nil, fmt.Errorf("failed to create WAL: %w", err)
    }

    // 2. Create blank state
    jobManager := jobmanager.NewJobManager()

    // 3. Replay WAL to restore state
    handler := func(event Event) error {
        // TODO: Apply to state based on event type
        switch event.Type {
        case EventEnqueue:
            // Where to get complete Job data?
            // WAL only records JobID, need to get it from Snapshot or elsewhere

        case EventDispatch:
            // Mark as in-flight (need to recalculate deadline)
            jobManager.MarkInFlight(event.JobID, time.Now().Add(timeout))

        case EventAck:
            // Idempotent handling: skip if already completed
            if !jobManager.IsCompleted(event.JobID) {
                jobManager.MarkCompleted(event.JobID)
            }

        case EventRetry:
            // Requeue
            job := jobManager.GetJob(event.JobID)
            if job != nil {
                jobManager.Requeue(*job)
            }

        case EventDead:
            // Mark as failed
            jobManager.MarkDead(event.JobID)
        }
        return nil
    }

    err = wal.Replay(handler)
    if err != nil {
        return nil, fmt.Errorf("failed to replay WAL: %w", err)
    }

    return &Controller{
        wal:   wal,
        state: state,
    }, nil
}

// ============================================================================
// Example 2: Write to WAL during Controller operations
// ============================================================================

// Enqueue adds a task
func (c *Controller) Enqueue(job Job) error {
    // TODO: Think about the order of writing WAL and modifying state
    // Option A: Write WAL first, then modify state (Write-Ahead)
    //   Pros: Won't lose committed operations during crash
    //   Cons: State unchanged when WAL write fails, need to rollback or reject
    //
    // Option B: Modify state first, then write WAL
    //   Pros: Simple
    //   Cons: During crash, state may have changed but WAL not recorded

    // Option A implementation (recommended):
    err := c.wal.Append(EventEnqueue, job.ID)
    if err != nil {
        return fmt.Errorf("failed to write WAL: %w", err)
    }

    err = c.jobManager.Enqueue(job)
    if err != nil {
        // TODO: WAL already written but state modification failed, how to handle?
        // Option: Log error, will attempt to Enqueue again during replay
        return err
    }

    return nil
}

// Dispatch assigns a task to a Worker
func (c *Controller) Dispatch(jobID string) error {
    err := c.wal.Append(EventDispatch, jobID)
    if err != nil {
        return err
    }

    deadline := time.Now().Add(c.taskTimeout)
    return c.jobManager.MarkInFlight(jobID, deadline)
}

// HandleAck handles Worker completion acknowledgment
func (c *Controller) HandleAck(jobID string) error {
    err := c.wal.Append(EventAck, jobID)
    if err != nil {
        return err
    }

    return c.jobManager.MarkCompleted(jobID)
}

// ============================================================================
// Example 3: Working with Snapshot
// ============================================================================

// TakeSnapshot creates a snapshot and rotates WAL
func (c *Controller) TakeSnapshot() error {
    // 1. Lock for protection (avoid state changes)
    c.mu.Lock()
    defer c.mu.Unlock()

    // 2. Get current state
    snapshot := c.jobManager.Snapshot()

    // 3. Record WAL's last_seq
    snapshot.LastSeq = c.wal.GetLastSeq()

    // 4. Write snapshot file (atomically)
    err := c.snapshotManager.Write(snapshot)
    if err != nil {
        return fmt.Errorf("failed to write snapshot: %w", err)
    }

    // 5. Rotate WAL (clear logs)
    err = c.wal.Rotate()
    if err != nil {
        // TODO: Snapshot written but WAL rotation failed, how to handle?
        // Can continue using old WAL, try again at next snapshot
        return fmt.Errorf("failed to rotate WAL: %w", err)
    }

    return nil
}

// LoadFromSnapshot restores from snapshot (optimized recovery flow)
func (c *Controller) LoadFromSnapshot() error {
    // 1. Load snapshot
    snapshot, err := c.snapshotManager.Load()
    if err != nil {
        if errors.Is(err, ErrSnapshotNotFound) {
            // No snapshot, start from blank state, replay entire WAL
            return c.replayFullWAL()
        }
        return err
    }

    // 2. Restore state
    err = c.jobManager.Restore(snapshot)
    if err != nil {
        return err
    }

    // 3. Replay WAL events after snapshot
    // Question: How to know which events are after snapshot?
    // Option A: WAL file already rotated, replay current WAL directly (all events after snapshot)
    // Option B: Compare event.Seq with snapshot.LastSeq (need to keep old WAL)

    err = c.wal.Replay(c.buildReplayHandler())
    if err != nil {
        return err
    }

    return nil
}

// ============================================================================
// Example 4: Batch operation optimization
// ============================================================================

// EnqueueBatch adds tasks in batch
func (c *Controller) EnqueueBatch(jobs []Job) error {
    // Use batch writer to improve performance
    bw := NewBatchWriter(c.wal, 100, 10*time.Millisecond)
    defer bw.Close()

    for _, job := range jobs {
        err := bw.Append(EventEnqueue, job.ID)
        if err != nil {
            return err
        }

        err = c.jobManager.Enqueue(job)
        if err != nil {
            return err
        }
    }

    // Ensure all events are written
    return bw.Flush()
}

// ============================================================================
// Example 5: Error recovery and degradation
// ============================================================================

// RecoverFromCorruption recovers from WAL corruption
func (c *Controller) RecoverFromCorruption(walPath string) error {
    // 1. Validate WAL
    err := ValidateWAL(walPath)
    if err != nil {
        log.Printf("WAL validation failed: %v", err)

        // 2. Attempt repair
        repairedPath := walPath + ".repaired"
        err = RepairWAL(walPath, repairedPath)
        if err != nil {
            // 3. Repair failed, degrade to using Snapshot only
            log.Println("WAL repair failed, loading from snapshot only")
            return c.LoadFromSnapshotOnly()
        }

        // 4. Use repaired WAL
        log.Printf("WAL repaired, using %s", repairedPath)
        // ... use repairedPath
    }

    return nil
}

// ============================================================================
// TODO: Implementation Considerations
// ============================================================================

TODO 1: Transactionality of WAL and state modification
  Problem: How to ensure atomicity of WAL write and state modification?
  Solution:
    - Write WAL first (persist commitment)
    - Modify state later (in-memory operation, fast)
    - If state modification fails, will retry during replay

TODO 2: Problem with WAL only recording JobID
  Problem: How to restore complete Job data during Replay?
  Solution:
    - Snapshot contains complete Job data
    - WAL only records state transitions (ID + Type)
    - Recovery = Load Snapshot + Replay WAL

TODO 3: Synchronization between Snapshot and WAL
  Problem: How to ensure consistency between Snapshot and WAL?
  Solution:
    - Snapshot records LastSeq
    - Rotate clears WAL
    - During recovery: Load Snapshot + Replay new WAL

TODO 4: Concurrency control
  Problem: Multiple goroutines writing to WAL and state simultaneously?
  Solution:
    - Controller uses single mutex for protection
    - WAL.Append already locked internally
    - Simple but may limit concurrency

TODO 5: Performance vs reliability trade-off
  Problem: Syncing every Append is slow, how to optimize?
  Solution:
    - Default: Sync every time (reliability priority)
    - Advanced: Batch Sync (performance priority)
    - Let user choose?

*/
