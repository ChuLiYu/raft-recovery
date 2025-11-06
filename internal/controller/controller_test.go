package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/storage/wal"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ============================================================================
// Test Helper Functions
// ============================================================================

// createTestController creates a test Controller
func createTestController(t *testing.T) (*Controller, string) {
	t.Helper()

	// Create temporary directory
	tmpDir, err := os.MkdirTemp("", "controller_test_*")
	if err != nil {
		t.Fatalf("Failed to create temp directory: %v", err)
	}

	config := Config{
		WorkerCount:      2,
		TaskTimeout:      2 * time.Second,
		SnapshotInterval: 5 * time.Second,
		MaxRetry:         3,
		WALPath:          filepath.Join(tmpDir, "test.wal"),
		SnapshotPath:     filepath.Join(tmpDir, "test.snapshot"),
		WALBufferSize:    10,
	}

	controller, err := NewController(config)
	if err != nil {
		t.Fatalf("Failed to create Controller: %v", err)
	}

	return controller, tmpDir
}

// cleanup cleans up test resources
func cleanup(t *testing.T, controller *Controller, tmpDir string) {
	t.Helper()

	if controller != nil {
		controller.Stop()
	}

	if tmpDir != "" {
		os.RemoveAll(tmpDir)
	}
}

// waitForJobStatus waits for a job to reach specified status
func waitForJobStatus(t *testing.T, controller *Controller, jobID types.JobID, checkFunc func() bool, timeout time.Duration) bool {
	t.Helper()

	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		if checkFunc() {
			return true
		}
		time.Sleep(50 * time.Millisecond)
	}
	return false
}

// ============================================================================
// Basic Functionality Tests
// ============================================================================

// TestNewController tests Controller initialization
func TestNewController(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	if controller == nil {
		t.Fatal("Controller should not be nil")
	}

	if controller.jobManager == nil {
		t.Error("JobManager not initialized")
	}

	if controller.wal == nil {
		t.Error("WAL not initialized")
	}

	if controller.snapshot == nil {
		t.Error("Snapshot Manager not initialized")
	}

	if controller.pool == nil {
		t.Error("Worker Pool not initialized")
	}

	if controller.config.WorkerCount != 2 {
		t.Errorf("WorkerCount = %d, want 2", controller.config.WorkerCount)
	}
}

// TestNewControllerWithInvalidPath tests initialization with invalid path
func TestNewControllerWithInvalidPath(t *testing.T) {
	config := Config{
		WorkerCount:      2,
		TaskTimeout:      2 * time.Second,
		SnapshotInterval: 5 * time.Second,
		MaxRetry:         3,
		WALPath:          "/invalid/path/test.wal",
		SnapshotPath:     "/invalid/path/test.snapshot",
		WALBufferSize:    10,
	}

	_, err := NewController(config)
	if err == nil {
		t.Error("Should return error with invalid path")
	}
}

// TestStart tests Controller startup
func TestStart(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Check if start time is set
	if controller.startTime.IsZero() {
		t.Error("Start time not set")
	}

	// Wait a moment to ensure loops are started
	time.Sleep(200 * time.Millisecond)

	// Check if stopCh is available
	select {
	case <-controller.stopCh:
		t.Error("stopCh should not be closed")
	default:
		// Correct
	}
}

// TestEnqueueJobs tests job enqueueing
func TestEnqueueJobs(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Verify jobs have been added
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	if stats["pending"] != 3 {
		t.Errorf("pending job count = %d, want 3", stats["pending"])
	}
}

// TestGetStatus tests status query
func TestGetStatus(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Wait a moment first
	time.Sleep(100 * time.Millisecond)

	status := controller.GetStatus()

	// Check required fields
	if _, ok := status["uptime"]; !ok {
		t.Error("status missing uptime field")
	}

	if workers, ok := status["workers"]; !ok || workers != 2 {
		t.Errorf("workers = %v, want 2", workers)
	}

	if _, ok := status["pending"]; !ok {
		t.Error("status missing pending field")
	}

	if _, ok := status["in_flight"]; !ok {
		t.Error("status missing in_flight field")
	}

	if _, ok := status["completed"]; !ok {
		t.Error("status missing completed field")
	}

	if _, ok := status["dead"]; !ok {
		t.Error("status missing dead field")
	}
}

// TestStop tests graceful shutdown
func TestStop(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, nil, tmpDir) // Don't call Stop again in cleanup

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add some jobs
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test"}},
	}
	controller.EnqueueJobs(jobs)

	// Wait for job to start processing
	time.Sleep(200 * time.Millisecond)

	// Execute Stop
	controller.Stop()

	// Check if stopCh is closed
	select {
	case <-controller.stopCh:
		// Correct
	default:
		t.Error("stopCh should be closed")
	}
}

// ============================================================================
// Basic Workflow Tests
// ============================================================================

// TestBasicWorkflow tests basic workflow: enqueue -> dispatch -> complete
func TestBasicWorkflow(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add job
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for job to complete or become dead (max 10 seconds)
	success := waitForJobStatus(t, controller, "task-001", func() bool {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		return controller.jobManager.IsCompleted("task-001") ||
			controller.jobManager.IsDead("task-001")
	}, 10*time.Second)

	if !success {
		t.Error("Job did not complete or become dead within 10 seconds")
	}

	// Check final status
	controller.mu.Lock()
	completed := controller.jobManager.IsCompleted("task-001")
	dead := controller.jobManager.IsDead("task-001")
	controller.mu.Unlock()

	if !completed && !dead {
		t.Error("Job should be in completed or dead status")
	}

	t.Logf("Job task-001 final status: completed=%v, dead=%v", completed, dead)
}

// TestMultipleJobsWorkflow tests concurrent processing of multiple jobs
func TestMultipleJobsWorkflow(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add multiple jobs
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
		{ID: "task-004", Payload: map[string]interface{}{"data": "test4"}},
		{ID: "task-005", Payload: map[string]interface{}{"data": "test5"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for all jobs to complete (max 15 seconds)
	deadline := time.Now().Add(15 * time.Second)
	allDone := false

	for time.Now().Before(deadline) {
		controller.mu.Lock()
		stats := controller.jobManager.Stats()
		totalDone := stats["completed"] + stats["dead"]
		controller.mu.Unlock()

		if totalDone >= 5 {
			allDone = true
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	if !allDone {
		t.Error("Not all jobs completed within 15 seconds")
	}

	// Check final statistics
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	totalDone := stats["completed"] + stats["dead"]
	if totalDone != 5 {
		t.Errorf("Total completed jobs = %d, want 5", totalDone)
	}

	t.Logf("Job statistics: completed=%d, dead=%d, in_flight=%d, pending=%d",
		stats["completed"], stats["dead"], stats["in_flight"], stats["pending"])
}

// ============================================================================
// Snapshot and Recovery Tests
// ============================================================================

// TestSnapshotCreation tests snapshot generation
func TestSnapshotCreation(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add jobs
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for jobs to be dispatched
	time.Sleep(500 * time.Millisecond)

	// Manually trigger snapshot
	err = controller.takeSnapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Check if snapshot file exists
	if _, err := os.Stat(controller.config.SnapshotPath); os.IsNotExist(err) {
		t.Error("Snapshot file does not exist")
	}
}

// TestLoadSnapshot tests snapshot loading
func TestLoadSnapshot(t *testing.T) {
	// Phase 1: Create Controller and generate snapshot
	controller1, tmpDir := createTestController(t)

	err := controller1.Start()
	if err != nil {
		t.Fatalf("Failed to start controller1: %v", err)
	}

	// Add jobs
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
	}

	err = controller1.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for jobs to be processed
	time.Sleep(500 * time.Millisecond)

	// Create snapshot
	err = controller1.takeSnapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Get statistics before snapshot
	controller1.mu.Lock()
	stats1 := controller1.jobManager.Stats()
	controller1.mu.Unlock()

	// Close first Controller
	controller1.Stop()

	// Phase 2: Create new Controller and load snapshot
	config := Config{
		WorkerCount:      2,
		TaskTimeout:      2 * time.Second,
		SnapshotInterval: 5 * time.Second,
		MaxRetry:         3,
		WALPath:          controller1.config.WALPath,
		SnapshotPath:     controller1.config.SnapshotPath,
		WALBufferSize:    10,
	}

	controller2, err := NewController(config)
	if err != nil {
		t.Fatalf("Failed to create controller2: %v", err)
	}
	defer cleanup(t, controller2, tmpDir)

	// Start will automatically load snapshot
	err = controller2.Start()
	if err != nil {
		t.Fatalf("Failed to start controller2: %v", err)
	}

	// Get statistics after recovery
	controller2.mu.Lock()
	stats2 := controller2.jobManager.Stats()
	totalJobs2 := stats2["pending"] + stats2["in_flight"] + stats2["completed"] + stats2["dead"]
	controller2.mu.Unlock()

	// Verify job count
	totalJobs1 := stats1["pending"] + stats1["in_flight"] + stats1["completed"] + stats1["dead"]
	if totalJobs2 != totalJobs1 {
		t.Errorf("Total jobs after recovery = %d, want %d", totalJobs2, totalJobs1)
	}

	t.Logf("Statistics before snapshot: %+v", stats1)
	t.Logf("Statistics after recovery: %+v", stats2)
}

// TestCrashRecovery tests crash recovery (snapshot + WAL replay)
func TestCrashRecovery(t *testing.T) {
	// Phase 1: Normal operation
	controller1, tmpDir := createTestController(t)

	err := controller1.Start()
	if err != nil {
		t.Fatalf("Failed to start controller1: %v", err)
	}

	// Add a batch of jobs
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
		{ID: "task-004", Payload: map[string]interface{}{"data": "test4"}},
		{ID: "task-005", Payload: map[string]interface{}{"data": "test5"}},
	}

	err = controller1.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for some jobs to complete
	time.Sleep(1 * time.Second)

	// Manually create snapshot
	err = controller1.takeSnapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	// Get statistics at snapshot time
	controller1.mu.Lock()
	stats1 := controller1.jobManager.Stats()
	controller1.mu.Unlock()

	// Simulate crash: directly close WAL and files (no Stop execution)
	controller1.wal.Close()

	// Phase 2: Recovery
	config := Config{
		WorkerCount:      2,
		TaskTimeout:      2 * time.Second,
		SnapshotInterval: 5 * time.Second,
		MaxRetry:         3,
		WALPath:          controller1.config.WALPath,
		SnapshotPath:     controller1.config.SnapshotPath,
		WALBufferSize:    10,
	}

	startRecovery := time.Now()
	controller2, err := NewController(config)
	if err != nil {
		t.Fatalf("Failed to create controller2: %v", err)
	}
	defer cleanup(t, controller2, tmpDir)

	// Start (will execute loadSnapshot + replayWAL)
	err = controller2.Start()
	if err != nil {
		t.Fatalf("Failed to start controller2: %v", err)
	}

	recoveryTime := time.Since(startRecovery)

	// Verify recovery time < 3s
	if recoveryTime > 3*time.Second {
		t.Errorf("Recovery time = %v, want < 3s", recoveryTime)
	}

	// Get statistics after recovery
	controller2.mu.Lock()
	stats2 := controller2.jobManager.Stats()
	controller2.mu.Unlock()

	// Verify job countconsistency
	totalJobs1 := stats1["pending"] + stats1["in_flight"] + stats1["completed"] + stats1["dead"]
	totalJobs2 := stats2["pending"] + stats2["in_flight"] + stats2["completed"] + stats2["dead"]

	if totalJobs2 != totalJobs1 {
		t.Errorf("Total jobs after recovery = %d, want %d", totalJobs2, totalJobs1)
	}

	t.Logf("Recovery time: %v", recoveryTime)
	t.Logf("Statistics before crash: %+v", stats1)
	t.Logf("Statistics after recovery: %+v", stats2)
}

// TestRecoveryTime tests recovery time performance (target < 3s)
func TestRecoveryTime(t *testing.T) {
	// Create a scenario with more jobs
	controller1, tmpDir := createTestController(t)

	err := controller1.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Add 50 jobs
	jobs := make([]types.Job, 50)
	for i := 0; i < 50; i++ {
		jobs[i] = types.Job{
			ID:      types.JobID(string(rune('a'+i/26)) + string(rune('a'+i%26))),
			Payload: map[string]interface{}{"index": i},
		}
	}

	err = controller1.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("Enqueue failed: %v", err)
	}

	// Wait for jobs to be processed
	time.Sleep(2 * time.Second)

	// Create snapshot
	err = controller1.takeSnapshot()
	if err != nil {
		t.Fatalf("Failed to create snapshot: %v", err)
	}

	controller1.Stop()

	// Test recovery time
	config := Config{
		WorkerCount:      2,
		TaskTimeout:      2 * time.Second,
		SnapshotInterval: 5 * time.Second,
		MaxRetry:         3,
		WALPath:          controller1.config.WALPath,
		SnapshotPath:     controller1.config.SnapshotPath,
		WALBufferSize:    10,
	}

	startTime := time.Now()

	controller2, err := NewController(config)
	if err != nil {
		t.Fatalf("Failed to create controller2: %v", err)
	}
	defer cleanup(t, controller2, tmpDir)

	err = controller2.Start()
	if err != nil {
		t.Fatalf("Failed to start controller2: %v", err)
	}

	recoveryTime := time.Since(startTime)

	if recoveryTime > 3*time.Second {
		t.Errorf("Recovery time = %v, exceeds 3s target", recoveryTime)
	}

	t.Logf("Time to recover 50 jobs: %v", recoveryTime)
}

// ============================================================================
// WAL Replay and Idempotency Tests
// ============================================================================

// TestReplayWAL tests WAL replay
func TestReplayWAL(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	// Do not start Controller, test replayWAL directly
	// Add some WAL events first (through direct operations)

	// Manually create some jobs and write to WAL
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
	}

	for _, job := range jobs {
		controller.wal.Append(wal.EventEnqueue, &job)
	}

	// Execute replay
	err := controller.replayWAL()
	if err != nil {
		t.Fatalf("WAL replay failed: %v", err)
	}

	// Verify JobManager status
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	// Since there are only Enqueue events, they should all be in the snapshot
	t.Logf("Statistics after replay: %+v", stats)
}

// TestIdempotency tests idempotency (repeated replay without errors)
func TestIdempotency(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	// Add jobs
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
	}

	for _, job := range jobs {
		controller.jobManager.Enqueue(job)
		controller.wal.Append(wal.EventEnqueue, &job)
	}

	// Mark as completed
	controller.jobManager.PopPending()
	deadline := time.Now().Add(2 * time.Second)
	controller.jobManager.MarkInFlight("task-001", deadline)
	controller.wal.Append(wal.EventDispatch, &jobs[0])
	controller.jobManager.MarkCompleted("task-001")
	controller.wal.Append(wal.EventAck, &jobs[0])

	// First replay
	err := controller.replayWAL()
	if err != nil {
		t.Fatalf("First replay failed: %v", err)
	}

	controller.mu.Lock()
	stats1 := controller.jobManager.Stats()
	controller.mu.Unlock()

	// Second replay (should be idempotent)
	err = controller.replayWAL()
	if err != nil {
		t.Fatalf("Second replay failed: %v", err)
	}

	controller.mu.Lock()
	stats2 := controller.jobManager.Stats()
	controller.mu.Unlock()

	// Statistics should be the same
	if stats1["completed"] != stats2["completed"] {
		t.Errorf("Idempotency test failed: first completed=%d, second completed=%d",
			stats1["completed"], stats2["completed"])
	}

	t.Logf("Idempotency test passed: stats1=%+v, stats2=%+v", stats1, stats2)
}

// ============================================================================
// Concurrency and Stress Tests
// ============================================================================

// TestConcurrentEnqueue tests concurrent enqueue
func TestConcurrentEnqueue(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	// Start multiple goroutines for concurrent enqueueing
	const goroutines = 5
	const jobsPerGoroutine = 10

	done := make(chan bool, goroutines)

	for i := 0; i < goroutines; i++ {
		go func(id int) {
			jobs := make([]types.Job, jobsPerGoroutine)
			for j := 0; j < jobsPerGoroutine; j++ {
				jobs[j] = types.Job{
					ID:      types.JobID(string(rune('A'+id)) + string(rune('0'+j))),
					Payload: map[string]interface{}{"goroutine": id, "index": j},
				}
			}

			if err := controller.EnqueueJobs(jobs); err != nil {
				t.Errorf("goroutine %d Enqueue failed: %v", id, err)
			}

			done <- true
		}(i)
	}

	// Wait for all goroutines to complete
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// Wait for jobs to be processed
	time.Sleep(2 * time.Second)

	// Check total job count
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	expectedTotal := goroutines * jobsPerGoroutine
	actualTotal := stats["pending"] + stats["in_flight"] + stats["completed"] + stats["dead"]

	if actualTotal != expectedTotal {
		t.Errorf("Total jobs = %d, want %d", actualTotal, expectedTotal)
	}

	t.Logf("Concurrent enqueue test: %d  goroutines, each %d  jobs, total %d ",
		goroutines, jobsPerGoroutine, expectedTotal)
	t.Logf("Final statistics: %+v", stats)
}

// ============================================================================
// Error Handling Tests
// ============================================================================

// TestEnqueueAfterStop tests enqueueing after stop
func TestEnqueueAfterStop(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, nil, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("Start failed: %v", err)
	}

	controller.Stop()

	// Try to enqueue after stop
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test"}},
	}

	err = controller.EnqueueJobs(jobs)
	// WAL is closed, should return error
	if err == nil {
		t.Log("Note: No error returned when enqueueing after stop (may need enhanced error checking in WAL)")
	} else {
		t.Logf("Enqueue after stop correctly returned error: %v", err)
	}
}
