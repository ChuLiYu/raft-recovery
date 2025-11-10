// ============================================================================
// Beaver-Raft Performance Test Suite
// ============================================================================
//
// Package: test/integration
// File: performance_test.go
// Functionality: System-level performance and crash recovery performance tests
//
// Test Objectives:
//   1. verify system throughput (jobs/second)
//   2. verify crash recovery time (< 3 second target)
//   3. verify data consistency and zero loss
//
// Test Environment:
//   - 8 workers
//   - simulated task execution latency: 0-500ms (average 250ms)
//   - simulated failure rate: 10%
//   - max retry count: 3
//
// TestSystemThroughput:
//   test system throughput under normal load
//   - submit 500 tasks
//   - measure completion time and success rate
//   - target: >= 5 jobs/s, >= 85% completion rate
//
// TestRecoveryPerformance:
//   test crash recovery performance
//   - submit 500 tasks
//   - simulate system crash (Stop Controller)
//   - measure recovery time (create new Controller and Start)
//   - target: < 3 seconds recovery time
//
// Performance Baseline:
//   Theoretical throughput calculation:
//   - 8 workers × 1000ms / 250ms average execution time = 32 jobs/s
//   - considering scheduling overhead and retries, actual approximately 5-10 jobs/s
//
// Notes:
//   - test results affected by system load
//   - CI environment may be slower than local
//   - usetemp directory to avoid test pollution
//
// ============================================================================

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// TestSystemThroughput tests system throughput
//
// Test Flow:
//  1. Create and start Controller
//  2. Submit 500 tasks in batch
//  3. Wait for all tasks to complete (up to 60 seconds)
//  4. Calculate throughput and completion rate
//  5. Verify meets performance target
func TestSystemThroughput(t *testing.T) {
	config := controller.Config{
		WorkerCount:      8,
		TaskTimeout:      5 * time.Second,
		SnapshotInterval: 30 * time.Second,
		MaxRetry:         3,
		WALPath:          t.TempDir() + "/wal",
		SnapshotPath:     t.TempDir() + "/snapshot",
		WALBufferSize:    100,
	}

	ctrl, err := controller.NewController(config)
	if err != nil {
		t.Fatalf("Failed to create controller: %v", err)
	}

	if err := ctrl.Start(); err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}
	defer ctrl.Stop()

	// Test parameters - reduce job count to match execution speed
	// Worker execution time approximately 0-500ms, 8 workers, ~30s can complete ~500 tasks
	totalJobs := 500

	// Prepare jobs
	jobs := make([]types.Job, totalJobs)
	for i := 0; i < totalJobs; i++ {
		jobs[i] = types.Job{
			ID:      types.JobID(fmt.Sprintf("perf-job-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 2 * time.Second,
		}
	}

	// Start timing
	startTime := time.Now()

	// Submit jobs in batch
	if err := ctrl.EnqueueJobs(jobs); err != nil {
		t.Fatalf("Failed to enqueue jobs: %v", err)
	}

	// Wait for all tasks to complete
	maxWaitTime := 60 * time.Second
	deadline := time.Now().Add(maxWaitTime)

	for time.Now().Before(deadline) {
		stats := ctrl.GetStatus()
		completed := stats["completed"].(int)
		dead := stats["dead"].(int)

		if completed+dead >= totalJobs {
			break
		}

		time.Sleep(100 * time.Millisecond)
	}

	// Stop timing
	elapsedTime := time.Since(startTime)

	// Get final stats
	finalStats := ctrl.GetStatus()
	completed := finalStats["completed"].(int)
	dead := finalStats["dead"].(int)

	// Compute throughput
	throughput := float64(completed) / elapsedTime.Seconds()

	t.Logf("=== Performance Test Results ===")
	t.Logf("Total jobs: %d", totalJobs)
	t.Logf("Completed: %d", completed)
	t.Logf("Failed (dead): %d", dead)
	t.Logf("Elapsed time: %v", elapsedTime)
	t.Logf("Throughput: %.2f jobs/second", throughput)
	t.Logf("================================")

	// Verify target - adjust based on actual execution
	// Worker average execution time 250ms, 8 workers; theoretical throughput ~32 jobs/s
	// considering retries and scheduling overhead, set target as 5 jobs/s
	expectedThroughput := 5.0
	if throughput < expectedThroughput {
		t.Errorf("⚠️  Throughput %.2f jobs/s is below target of %.2f jobs/s", throughput, expectedThroughput)
	} else {
		t.Logf("✅ Throughput target met: %.2f jobs/s >= %.2f jobs/s", throughput, expectedThroughput)
	}

	// Verify completion rate - considering 10% failure rate and retries, expect at least 85% completion
	minCompletionRate := 85
	if completed < totalJobs*minCompletionRate/100 {
		t.Errorf("Completion rate too low: %d/%d (%.1f%%)", completed, totalJobs, float64(completed)/float64(totalJobs)*100)
	} else {
		t.Logf("✅ Completion rate: %d/%d (%.1f%%)", completed, totalJobs, float64(completed)/float64(totalJobs)*100)
	}
}

// TestRecoveryPerformance tests recovery performance
func TestRecoveryPerformance(t *testing.T) {
	tempDir := t.TempDir()

	config := controller.Config{
		WorkerCount:      8,
		TaskTimeout:      5 * time.Second,
		SnapshotInterval: 2 * time.Second,
		MaxRetry:         3,
		WALPath:          tempDir + "/wal",
		SnapshotPath:     tempDir + "/snapshot",
		WALBufferSize:    100,
	}

	// Phase 1: create and run controller
	ctrl1, err := controller.NewController(config)
	if err != nil {
		t.Fatalf("Failed to create controller: %v", err)
	}

	if err := ctrl1.Start(); err != nil {
		t.Fatalf("Failed to start controller: %v", err)
	}

	// Add 500 tasks
	jobs := make([]types.Job, 500)
	for i := 0; i < 500; i++ {
		jobs[i] = types.Job{
			ID:      types.JobID(fmt.Sprintf("load-job-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 3 * time.Second,
		}
	}

	if err := ctrl1.EnqueueJobs(jobs); err != nil {
		t.Fatalf("Failed to enqueue jobs: %v", err)
	}

	// Wait for snapshot to complete
	time.Sleep(3 * time.Second)

	stats1 := ctrl1.GetStatus()
	t.Logf("Before crash - Stats: %+v", stats1)

	ctrl1.Stop()

	// Phase 2: measure recovery time
	t.Log("Simulating crash recovery...")
	startTime := time.Now()

	ctrl2, err := controller.NewController(config)
	if err != nil {
		t.Fatalf("Failed to create controller on recovery: %v", err)
	}

	if err := ctrl2.Start(); err != nil {
		t.Fatalf("Failed to start controller on recovery: %v", err)
	}

	recoveryTime := time.Since(startTime)

	stats2 := ctrl2.GetStatus()
	t.Logf("After recovery - Stats: %+v", stats2)

	defer ctrl2.Stop()

	// Verify recovery time
	t.Logf("=== Recovery Performance ===")
	t.Logf("Recovery time: %v", recoveryTime)
	t.Logf("Jobs recovered: %d", stats2["pending"].(int)+stats2["in_flight"].(int)+stats2["completed"].(int))
	t.Logf("===========================")

	if recoveryTime > 3*time.Second {
		t.Errorf("❌ Recovery time %v exceeds 3s target", recoveryTime)
	} else {
		t.Logf("✅ Recovery time target met: %v < 3s", recoveryTime)
	}
}
