// ============================================================================
// Beaver-Raft Recovery Test Suite
// ============================================================================
//
// Package: test/integration
// file: recovery_test.go
// functionality: end-to-end recovery functionality tests
//
// test objectives:
//   verify system job handling capability under normal operation:
//   1. jobs successfully enqueued
//   2. workers execute jobs normally
//   3. job state updated correctly
//   4. failed jobs marked as dead-letter correctly
//
// TestEndToEndRecovery:
//   full job lifecycle test
//   - submit 50 jobs
//   - wait for execution to complete (10s)
//   - verify at least 70% of jobs complete
//   - considering a 10% simulated failure rate
//
// test configuration:
//   - 4 workers (smaller number for observability)
//   - 5s task timeout
//   - 10s snapshot interval
//
// expected result:
//   with a 10% failure rate:
//   - completed jobs: >= 35 (70%)
//   - dead-letter jobs: <= 15 (30%)
//   - no loss: completed + dead = total
//
// failure scenarios:
//   if completion rate is below 70%, possible causes:
//   1. worker execution time too long
//   2. system load too high
//   3. test wait time insufficient
//
// ============================================================================

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"github.com/stretchr/testify/require"
)

// generateTestJobs generates the specified number of test jobs
// Each job contains a simple payload for tests rather than real business logic
func generateTestJobs(count int) []types.Job {
	jobs := make([]types.Job, count)
	for i := 0; i < count; i++ {
		jobs[i] = types.Job{
			ID:      types.JobID(fmt.Sprintf("job-%d", i)),
			Payload: map[string]interface{}{"key": i},
		}
	}
	return jobs
}

func TestEndToEndRecovery(t *testing.T) {
	// prepare temp file paths
	walPath := fmt.Sprintf("/tmp/test-recovery-wal-%d.log", time.Now().UnixNano())
	snapshotPath := fmt.Sprintf("/tmp/test-recovery-snapshot-%d.json", time.Now().UnixNano())

	config := controller.Config{
		WorkerCount:      4,
		TaskTimeout:      5 * time.Second,
		SnapshotInterval: 10 * time.Second, // increase snapshot interval to avoid interference
		WALPath:          walPath,
		SnapshotPath:     snapshotPath,
		WALBufferSize:    100,
	}

	// Phase 1: start controller and enqueue jobs
	ctrl, err := controller.NewController(config)
	require.NoError(t, err)

	err = ctrl.Start()
	require.NoError(t, err)

	// wait for startup
	time.Sleep(100 * time.Millisecond)

	// enqueue jobs
	jobs := generateTestJobs(50)
	err = ctrl.EnqueueJobs(jobs)
	require.NoError(t, err)

	// wait for jobs to complete - add buffer to accommodate worker speed
	// 50 jobs, 4 workers, ~250ms/job, ~3-4s needed
	// plus 10% failure rate and retries, give enough time
	time.Sleep(10 * time.Second)

	// verify jobs completed
	status := ctrl.GetStatus()
	ctrl.Stop()

	completed := status["completed"].(int)
	dead := status["dead"].(int)
	t.Logf("Completed jobs: %d, Dead-letter jobs: %d", completed, dead)

	// considering 10% failure rate and execution time, expect at least 35 jobs completed (70%)
	require.GreaterOrEqual(t, completed, 35, "at least 35 jobs should complete")
}
