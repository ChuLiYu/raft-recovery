package jobmanager

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ============================================================================
// Test Helper Functions
// ============================================================================

// newTestJobManager creates a test JobManager
func newTestJobManager() *JobManager {
	return NewJobManager()
}

// newTestJob creates a test Job
func newTestJob(id string) types.Job {
	return types.Job{
		ID:      types.JobID(id),
		Payload: map[string]interface{}{"test": "data"},
		Attempt: 0,
	}
}

// assertNoError asserts no error occurred
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// assertError asserts a specific error occurred
func assertError(t *testing.T, err error, want error) {
	t.Helper()
	if err == nil {
		t.Errorf("expected error %v, got nil", want)
		return
	}
	if !errors.Is(err, want) {
		t.Errorf("expected error %v, got %v", want, err)
	}
}

// (removed) assertEqual helper was unused

// assertJobStatus asserts job status
func assertJobStatus(t *testing.T, jm *JobManager, jobID types.JobID, want types.JobStatus) {
	t.Helper()
	job, exists := jm.jobs[jobID]
	if !exists {
		t.Errorf("job %s not found", jobID)
		return
	}
	if job.Status != want {
		t.Errorf("job %s status: got %s, want %s", jobID, job.Status, want)
	}
}

// ============================================================================
// Unit Tests
// ============================================================================

func TestNewJobManager(t *testing.T) {
	jm := NewJobManager()

	// Verify all fields are initialized
	if jm.jobs == nil {
		t.Error("jobs map not initialized")
	}
	if jm.queue == nil {
		t.Error("queue slice not initialized")
	}
	if jm.inFlight == nil {
		t.Error("inFlight map not initialized")
	}
	if jm.completed == nil {
		t.Error("completed map not initialized")
	}
	if jm.dead == nil {
		t.Error("dead map not initialized")
	}

	// Verify initial state
	stats := jm.Stats()
	expectedStats := map[string]int{
		"pending":   0,
		"in_flight": 0,
		"completed": 0,
		"dead":      0,
	}
	for key, value := range expectedStats {
		if stats[key] != value {
			t.Errorf("stats[%s]: got %d, want %d", key, stats[key], value)
		}
	}
}

func TestEnqueue(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		job     types.Job
		wantErr error
	}{
		{
			name:    "Normal single job enqueue",
			setup:   func(jm *JobManager) {},
			job:     newTestJob("task-001"),
			wantErr: nil,
		},
		{
			name:    "Enqueue multiple jobs",
			setup:   func(jm *JobManager) { jm.Enqueue(newTestJob("task-001")) },
			job:     newTestJob("task-002"),
			wantErr: nil,
		},
		{
			name:    "Duplicate ID error",
			setup:   func(jm *JobManager) { jm.Enqueue(newTestJob("task-001")) },
			job:     newTestJob("task-001"),
			wantErr: ErrDuplicateJob,
		},
		{
			name:    "Empty ID handling",
			setup:   func(jm *JobManager) {},
			job:     newTestJob(""),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.Enqueue(tt.job)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// verify job is enqueued
				if _, exists := jm.jobs[tt.job.ID]; !exists {
					t.Errorf("job %s not found in jobs map", tt.job.ID)
				}
				// verifyJob in queue in
				found := false
				for _, id := range jm.queue {
					if id == tt.job.ID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("job %s not found in queue", tt.job.ID)
				}
				// verify state
				assertJobStatus(t, jm, tt.job.ID, types.StatusPending)
			}
		})
	}
}

func TestPopPending(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		wantJob *types.Job
		wantNil bool
	}{
		{
			name:    "Empty queue returns nil",
			setup:   func(jm *JobManager) {},
			wantNil: true,
		},
		{
			name: "FIFO order correct",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.Enqueue(newTestJob("task-002"))
			},
			wantJob: &types.Job{ID: "task-001"},
		},
		{
			name: "Consecutive pop until empty",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			wantJob: &types.Job{ID: "task-001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			job := jm.PopPending()

			if tt.wantNil {
				if job != nil {
					t.Errorf("expected nil, got %v", job)
				}
			} else {
				if job == nil {
					t.Error("expected job, got nil")
					return
				}
				if job.ID != tt.wantJob.ID {
					t.Errorf("got job ID %s, want %s", job.ID, tt.wantJob.ID)
				}
				// verify queue is updated
				if len(jm.queue) == 0 && tt.name == "FIFO order correct" {
					t.Error("queue should not be empty after first pop")
				}
			}
		})
	}
}

func TestMarkInFlight(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*JobManager)
		jobID    types.JobID
		deadline time.Time
		wantErr  error
	}{
		{
			name: "Mark in-flight (normal path)",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:    "task-001",
			deadline: time.Now().Add(time.Minute),
			wantErr:  nil,
		},
		{
			name:     "Job does not exist error",
			setup:    func(jm *JobManager) {},
			jobID:    "task-001",
			deadline: time.Now().Add(time.Minute),
			wantErr:  ErrJobNotFound,
		},
		{
			name: "Job not in pending state error",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:    "task-001",
			deadline: time.Now().Add(time.Minute),
			wantErr:  fmt.Errorf("job not in pending status"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.MarkInFlight(tt.jobID, tt.deadline)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.wantErr)
				} else if err.Error() != tt.wantErr.Error() {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				assertNoError(t, err)
				// verify inFlight set updated correctly
				if _, exists := jm.inFlight[tt.jobID]; !exists {
					t.Errorf("job %s not found in inFlight", tt.jobID)
				}
				// verify deadline is set correctly
				job := jm.jobs[tt.jobID]
				if job.Deadline == nil {
					t.Error("deadline not set")
				} else if *job.Deadline != tt.deadline.UnixMilli() {
					t.Errorf("deadline: got %d, want %d", *job.Deadline, tt.deadline.UnixMilli())
				}
				// verify state
				assertJobStatus(t, jm, tt.jobID, types.StatusInFlight)
			}
		})
	}
}

func TestMarkCompleted(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantErr error
	}{
		{
			name: "Complete normally",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:   "task-001",
			wantErr: nil,
		},
		{
			name:    "Job does not exist error",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantErr: ErrJobNotFound,
		},
		{
			name: "Job not in flight error",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantErr: ErrNotInFlight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.MarkCompleted(tt.jobID)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// verify removed from inFlight
				if _, exists := jm.inFlight[tt.jobID]; exists {
					t.Errorf("job %s still in inFlight", tt.jobID)
				}
				// verify added to completed
				if _, exists := jm.completed[tt.jobID]; !exists {
					t.Errorf("job %s not found in completed", tt.jobID)
				}
				// verify state
				assertJobStatus(t, jm, tt.jobID, types.StatusCompleted)
			}
		})
	}
}

func TestRequeue(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantErr error
	}{
		{
			name: "Requeue normally",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:   "task-001",
			wantErr: nil,
		},
		{
			name:    "Job does not exist error",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantErr: ErrJobNotFound,
		},
		{
			name: "Job not in flight error",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantErr: ErrNotInFlight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			originalAttempt := 0
			if job, exists := jm.jobs[tt.jobID]; exists {
				originalAttempt = job.Attempt
			}
			err := jm.Requeue(tt.jobID)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// verify attempt incremented
				newAttempt := jm.jobs[tt.jobID].Attempt
				if newAttempt != originalAttempt+1 {
					t.Errorf("attempt: got %d, want %d", newAttempt, originalAttempt+1)
				}
				// verify re-added to end of queue
				if len(jm.queue) == 0 {
					t.Error("queue should not be empty after requeue")
				}
				lastJobID := jm.queue[len(jm.queue)-1]
				if lastJobID != tt.jobID {
					t.Errorf("last job in queue: got %s, want %s", lastJobID, tt.jobID)
				}
				// verify state
				assertJobStatus(t, jm, tt.jobID, types.StatusPending)
			}
		})
	}
}

func TestMarkDead(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantErr error
	}{
		{
			name: "Mark as dead (normal path)",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantErr: nil,
		},
		{
			name:    "Job does not exist error",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantErr: ErrJobNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.MarkDead(tt.jobID)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// verify added to dead set
				if _, exists := jm.dead[tt.jobID]; !exists {
					t.Errorf("job %s not found in dead", tt.jobID)
				}
				// verify stateupdatedcorrect
				assertJobStatus(t, jm, tt.jobID, types.StatusDead)
			}
		})
	}
}

func TestGetExpiredJobs(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	tests := []struct {
		name        string
		setup       func(*JobManager)
		now         time.Time
		wantExpired []types.JobID
	}{
		{
			name:        "No expired jobs",
			setup:       func(jm *JobManager) {},
			now:         now,
			wantExpired: []types.JobID{},
		},
		{
			name: "Single expired job",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", past)
			},
			now:         now,
			wantExpired: []types.JobID{"task-001"},
		},
		{
			name: "Multiple expired jobs",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.PopPending()
				jm.MarkInFlight("task-001", past)
				jm.MarkInFlight("task-002", past)
			},
			now:         now,
			wantExpired: []types.JobID{"task-001", "task-002"},
		},
		{
			name: "Mixed expired and non-expired",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.PopPending()
				jm.MarkInFlight("task-001", past)
				jm.MarkInFlight("task-002", future)
			},
			now:         now,
			wantExpired: []types.JobID{"task-001"},
		},
		{
			name: "Boundary condition (deadline exactly now)",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", now)
			},
			now:         now,
			wantExpired: []types.JobID{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			expired := jm.GetExpiredJobs(tt.now)

			if len(expired) != len(tt.wantExpired) {
				t.Errorf("expired count: got %d, want %d", len(expired), len(tt.wantExpired))
				return
			}

			// verify contains all expected expired jobs
			for _, wantJobID := range tt.wantExpired {
				found := false
				for _, gotJobID := range expired {
					if gotJobID == wantJobID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected expired job %s not found", wantJobID)
				}
			}
		})
	}
}

func TestStats(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*JobManager)
		wantStats map[string]int
	}{
		{
			name:      "Empty state stats",
			setup:     func(jm *JobManager) {},
			wantStats: map[string]int{"pending": 0, "in_flight": 0, "completed": 0, "dead": 0},
		},
		{
			name: "Jobs in all states",
			setup: func(jm *JobManager) {
				// create a simpler test scenario
				// InFlight
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))

				// Completed
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.MarkInFlight("task-002", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-002")

				// Dead
				jm.Enqueue(newTestJob("task-003"))
				jm.PopPending() // remove from queue first
				jm.MarkDead("task-003")
			},
			wantStats: map[string]int{"pending": 0, "in_flight": 1, "completed": 1, "dead": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			stats := jm.Stats()

			for key, wantValue := range tt.wantStats {
				if stats[key] != wantValue {
					t.Errorf("stats[%s]: got %d, want %d", key, stats[key], wantValue)
				}
			}
		})
	}
}

// ============================================================================
// Integration tests
// ============================================================================

func TestJobLifecycle(t *testing.T) {
	jm := newTestJobManager()

	// Test full lifecycle: Enqueue -> PopPending -> MarkInFlight -> MarkCompleted
	job := newTestJob("task-001")

	// Enqueue
	err := jm.Enqueue(job)
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusPending)

	// PopPending
	poppedJob := jm.PopPending()
	if poppedJob == nil || poppedJob.ID != "task-001" {
		t.Error("PopPending failed")
	}

	// MarkInFlight
	deadline := time.Now().Add(time.Minute)
	err = jm.MarkInFlight("task-001", deadline)
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusInFlight)

	// MarkCompleted
	err = jm.MarkCompleted("task-001")
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusCompleted)

	// verify final state
	stats := jm.Stats()
	if stats["completed"] != 1 {
		t.Errorf("expected 1 completed job, got %d", stats["completed"])
	}
}

func TestJobLifecycleWithRetry(t *testing.T) {
	jm := newTestJobManager()

	// Test retry lifecycle: Enqueue -> PopPending -> MarkInFlight -> Requeue -> MarkInFlight -> MarkDead
	job := newTestJob("task-001")

	// Enqueue
	err := jm.Enqueue(job)
	assertNoError(t, err)

	// PopPending
	poppedJob := jm.PopPending()
	if poppedJob == nil || poppedJob.ID != "task-001" {
		t.Error("PopPending failed")
	}

	// MarkInFlight
	deadline := time.Now().Add(time.Minute)
	err = jm.MarkInFlight("task-001", deadline)
	assertNoError(t, err)

	// Requeue (simulate failed retry)
	err = jm.Requeue("task-001")
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusPending)

	// MarkInFlight again
	err = jm.MarkInFlight("task-001", deadline)
	assertNoError(t, err)

	// MarkDead (simulate exceeding retry count)
	err = jm.MarkDead("task-001")
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusDead)

	// verify final state
	stats := jm.Stats()
	if stats["dead"] != 1 {
		t.Errorf("expected 1 dead job, got %d", stats["dead"])
	}
}

func TestStateInvariants(t *testing.T) {
	jm := newTestJobManager()

	// Add multiple jobs and perform various operations
	jm.Enqueue(newTestJob("task-001"))
	jm.Enqueue(newTestJob("task-002"))
	jm.Enqueue(newTestJob("task-003"))

	jm.PopPending()
	jm.MarkInFlight("task-001", time.Now().Add(time.Minute))

	jm.PopPending()
	jm.MarkInFlight("task-002", time.Now().Add(time.Minute))
	jm.MarkCompleted("task-002")

	jm.MarkDead("task-003")

	// verify immutability: each job belongs to exactly one set
	allJobIDs := make(map[types.JobID]bool)

	// collect all job IDs
	for jobID := range jm.jobs {
		allJobIDs[jobID] = true
	}

	// verify jobs map contains all jobs
	if len(allJobIDs) != 3 {
		t.Errorf("expected 3 jobs in jobs map, got %d", len(allJobIDs))
	}

	// verify state transition consistency
	// Note: PopPending removes job from queue, but job remains in jobs map
	// therefore we need to check actual job state
	actualTotal := 0
	for _, job := range jm.jobs {
		switch job.Status {
		case types.StatusPending:
			actualTotal++
		case types.StatusInFlight:
			actualTotal++
		case types.StatusCompleted:
			actualTotal++
		case types.StatusDead:
			actualTotal++
		}
	}
	if actualTotal != len(allJobIDs) {
		t.Errorf("actual total jobs (%d) != jobs map size (%d)", actualTotal, len(allJobIDs))
	}
}

// ============================================================================
// Concurrent tests
// ============================================================================

func TestConcurrentEnqueue(t *testing.T) {
	jm := newTestJobManager()

	const numGoroutines = 10
	const jobsPerGoroutine = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*jobsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				jobID := types.JobID(fmt.Sprintf("task-%d-%d", goroutineID, j))
				job := types.Job{ID: jobID, Payload: map[string]interface{}{"goroutine": goroutineID}}
				if err := jm.Enqueue(job); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// ensure no errors
	for err := range errors {
		t.Errorf("concurrent enqueue error: %v", err)
	}

	// verify all jobs were added
	stats := jm.Stats()
	expectedTotal := numGoroutines * jobsPerGoroutine
	if stats["pending"] != expectedTotal {
		t.Errorf("expected %d pending jobs, got %d", expectedTotal, stats["pending"])
	}
}

func TestConcurrentOperations(t *testing.T) {
	jm := newTestJobManager()

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*10)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				jobID := types.JobID(fmt.Sprintf("task-%d-%d", goroutineID, j))
				job := types.Job{ID: jobID, Payload: map[string]interface{}{"goroutine": goroutineID}}

				// Enqueue
				if err := jm.Enqueue(job); err != nil {
					errors <- err
					continue
				}

				// PopPending
				poppedJob := jm.PopPending()
				if poppedJob == nil {
					errors <- fmt.Errorf("PopPending returned nil")
					continue
				}

				// MarkInFlight
				if err := jm.MarkInFlight(poppedJob.ID, time.Now().Add(time.Minute)); err != nil {
					errors <- err
					continue
				}

				// MarkCompleted
				if err := jm.MarkCompleted(poppedJob.ID); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// ensure no errors
	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
	}

	// verify data consistency
	stats := jm.Stats()
	if stats["pending"] != 0 {
		t.Errorf("expected 0 pending jobs, got %d", stats["pending"])
	}
	if stats["completed"] != numGoroutines*10 {
		t.Errorf("expected %d completed jobs, got %d", numGoroutines*10, stats["completed"])
	}
}

// ============================================================================
// Performance tests (Benchmarks)
// ============================================================================

func BenchmarkEnqueue(b *testing.B) {
	jm := NewJobManager()
	job := newTestJob("benchmark-job")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job.ID = types.JobID(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
	}
}

func BenchmarkPopPending(b *testing.B) {
	jm := NewJobManager()

	// prefill
	for i := 0; i < b.N; i++ {
		job := newTestJob(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jm.PopPending()
	}
}

func BenchmarkMarkInFlight(b *testing.B) {
	jm := NewJobManager()
	deadline := time.Now().Add(time.Minute)

	// prefill
	for i := 0; i < b.N; i++ {
		job := newTestJob(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
		jm.PopPending()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jm.MarkInFlight(types.JobID(fmt.Sprintf("job-%d", i)), deadline)
	}
}

func BenchmarkConcurrentEnqueue(b *testing.B) {
	jm := NewJobManager()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			job := newTestJob(fmt.Sprintf("job-%d", i))
			jm.Enqueue(job)
			i++
		}
	})
}

func BenchmarkConcurrentMixed(b *testing.B) {
	jm := NewJobManager()
	deadline := time.Now().Add(time.Minute)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			jobID := types.JobID(fmt.Sprintf("job-%d", i))
			job := newTestJob(string(jobID))

			// mixed operations
			jm.Enqueue(job)
			if poppedJob := jm.PopPending(); poppedJob != nil {
				jm.MarkInFlight(poppedJob.ID, deadline)
				jm.MarkCompleted(poppedJob.ID)
			}
			i++
		}
	})
}

func BenchmarkStats(b *testing.B) {
	jm := NewJobManager()

	// prefill
	for i := 0; i < 1000; i++ {
		job := newTestJob(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jm.Stats()
	}
}

// ============================================================================
// New methods tests (Snapshot, Restore, IsCompleted, IsDead, GetJob)
// ============================================================================

func TestSnapshot(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*JobManager)
		want  func(types.SnapshotData) bool
	}{
		{
			name:  "Empty state snapshot",
			setup: func(jm *JobManager) {},
			want: func(data types.SnapshotData) bool {
				return len(data.Jobs) == 0 && data.SchemaVer == 1
			},
		},
		{
			name: "Snapshot with various states",
			setup: func(jm *JobManager) {
				// Pending
				jm.Enqueue(newTestJob("task-001"))

				// InFlight
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.MarkInFlight("task-002", time.Now().Add(time.Minute))

				// Completed
				jm.Enqueue(newTestJob("task-003"))
				jm.PopPending()
				jm.MarkInFlight("task-003", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-003")

				// Dead
				jm.Enqueue(newTestJob("task-004"))
				jm.MarkDead("task-004")
			},
			want: func(data types.SnapshotData) bool {
				if len(data.Jobs) != 4 {
					return false
				}
				if data.SchemaVer != 1 {
					return false
				}
				// verify each job's state
				if data.Jobs["task-001"].Status != types.StatusPending {
					return false
				}
				if data.Jobs["task-002"].Status != types.StatusInFlight {
					return false
				}
				if data.Jobs["task-003"].Status != types.StatusCompleted {
					return false
				}
				if data.Jobs["task-004"].Status != types.StatusDead {
					return false
				}
				return true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			data := jm.Snapshot()

			if !tt.want(data) {
				t.Errorf("Snapshot validation failed")
			}
		})
	}
}

func TestRestore(t *testing.T) {
	tests := []struct {
		name    string
		data    types.SnapshotData
		wantErr bool
		verify  func(*testing.T, *JobManager)
	}{
		{
			name: "Restore empty snapshot",
			data: types.SnapshotData{
				Jobs:      make(map[types.JobID]*types.Job),
				SchemaVer: 1,
			},
			wantErr: false,
			verify: func(t *testing.T, jm *JobManager) {
				stats := jm.Stats()
				if stats["pending"] != 0 || stats["in_flight"] != 0 ||
					stats["completed"] != 0 || stats["dead"] != 0 {
					t.Errorf("expected empty state after restore, got %v", stats)
				}
			},
		},
		{
			name: "Restore snapshot with various states",
			data: types.SnapshotData{
				Jobs: map[types.JobID]*types.Job{
					"task-001": {
						ID:      "task-001",
						Status:  types.StatusPending,
						Payload: map[string]interface{}{"test": "data"},
					},
					"task-002": {
						ID:      "task-002",
						Status:  types.StatusInFlight,
						Payload: map[string]interface{}{"test": "data"},
					},
					"task-003": {
						ID:      "task-003",
						Status:  types.StatusCompleted,
						Payload: map[string]interface{}{"test": "data"},
					},
					"task-004": {
						ID:      "task-004",
						Status:  types.StatusDead,
						Payload: map[string]interface{}{"test": "data"},
					},
				},
				SchemaVer: 1,
			},
			wantErr: false,
			verify: func(t *testing.T, jm *JobManager) {
				stats := jm.Stats()
				if stats["pending"] != 1 {
					t.Errorf("expected 1 pending job, got %d", stats["pending"])
				}
				if stats["in_flight"] != 1 {
					t.Errorf("expected 1 in_flight job, got %d", stats["in_flight"])
				}
				if stats["completed"] != 1 {
					t.Errorf("expected 1 completed job, got %d", stats["completed"])
				}
				if stats["dead"] != 1 {
					t.Errorf("expected 1 dead job, got %d", stats["dead"])
				}

				// verify each job's state
				if job := jm.GetJob("task-001"); job == nil || job.Status != types.StatusPending {
					t.Error("task-001 status incorrect")
				}
				if job := jm.GetJob("task-002"); job == nil || job.Status != types.StatusInFlight {
					t.Error("task-002 status incorrect")
				}
				if job := jm.GetJob("task-003"); job == nil || job.Status != types.StatusCompleted {
					t.Error("task-003 status incorrect")
				}
				if job := jm.GetJob("task-004"); job == nil || job.Status != types.StatusDead {
					t.Error("task-004 status incorrect")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()

			err := jm.Restore(tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("Restore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.verify != nil {
				tt.verify(t, jm)
			}
		})
	}
}

func TestSnapshotAndRestore(t *testing.T) {
	// Create first JobManager and add data
	jm1 := newTestJobManager()

	// Add jobs in various states (ensure consistency)
	// Pending
	jm1.Enqueue(newTestJob("task-001"))

	// InFlight
	jm1.Enqueue(newTestJob("task-002"))
	jm1.PopPending()
	jm1.MarkInFlight("task-002", time.Now().Add(time.Minute))

	// Completed
	jm1.Enqueue(newTestJob("task-003"))
	jm1.PopPending()
	jm1.MarkInFlight("task-003", time.Now().Add(time.Minute))
	jm1.MarkCompleted("task-003")

	// Dead
	jm1.Enqueue(newTestJob("task-004"))
	jm1.PopPending() // remove from queue first
	jm1.MarkDead("task-004")

	// Generate snapshot
	snapshot := jm1.Snapshot()

	// Create second JobManager and restore
	jm2 := newTestJobManager()
	err := jm2.Restore(snapshot)
	assertNoError(t, err)

	// Compare the state of the two JobManagers
	stats1 := jm1.Stats()
	stats2 := jm2.Stats()

	for key, value := range stats1 {
		if stats2[key] != value {
			t.Errorf("stats[%s]: jm1=%d, jm2=%d", key, value, stats2[key])
		}
	}

	// verify each job's details
	for jobID, job1 := range jm1.jobs {
		job2 := jm2.GetJob(jobID)
		if job2 == nil {
			t.Errorf("job %s not found in jm2", jobID)
			continue
		}
		if job1.Status != job2.Status {
			t.Errorf("job %s status: jm1=%s, jm2=%s", jobID, job1.Status, job2.Status)
		}
		if job1.Attempt != job2.Attempt {
			t.Errorf("job %s attempt: jm1=%d, jm2=%d", jobID, job1.Attempt, job2.Attempt)
		}
	}
}

func TestIsCompleted(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*JobManager)
		jobID types.JobID
		want  bool
	}{
		{
			name:  "Job does not exist",
			setup: func(jm *JobManager) {},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in Pending state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in InFlight state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in Completed state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-001")
			},
			jobID: "task-001",
			want:  true,
		},
		{
			name: "job in Dead state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.MarkDead("task-001")
			},
			jobID: "task-001",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			got := jm.IsCompleted(tt.jobID)

			if got != tt.want {
				t.Errorf("IsCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDead(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*JobManager)
		jobID types.JobID
		want  bool
	}{
		{
			name:  "Job does not exist",
			setup: func(jm *JobManager) {},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in Pending state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in InFlight state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in Completed state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-001")
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "job in Dead state",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.MarkDead("task-001")
			},
			jobID: "task-001",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			got := jm.IsDead(tt.jobID)

			if got != tt.want {
				t.Errorf("IsDead() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetJob(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantNil bool
		verify  func(*testing.T, *types.Job)
	}{
		{
			name:    "Job does not exist",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantNil: true,
		},
		{
			name: "Get Pending job",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusPending {
					t.Errorf("expected status Pending, got %s", job.Status)
				}
			},
		},
		{
			name: "Get InFlight job",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusInFlight {
					t.Errorf("expected status InFlight, got %s", job.Status)
				}
				if job.Deadline == nil {
					t.Error("expected Deadline to be set")
				}
			},
		},
		{
			name: "Get Completed job",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-001")
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusCompleted {
					t.Errorf("expected status Completed, got %s", job.Status)
				}
			},
		},
		{
			name: "Get Dead job",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.MarkDead("task-001")
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusDead {
					t.Errorf("expected status Dead, got %s", job.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			job := jm.GetJob(tt.jobID)

			if tt.wantNil {
				if job != nil {
					t.Errorf("expected nil, got %v", job)
				}
			} else {
				if job == nil {
					t.Error("expected job, got nil")
					return
				}
				if tt.verify != nil {
					tt.verify(t, job)
				}
			}
		})
	}
}
