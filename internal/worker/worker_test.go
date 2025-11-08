package worker

// ============================================================================
// Worker Pool Test File
// Purpose: Verify concurrent execution, timeout mechanism, graceful shutdown
// ============================================================================

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// Basic Functionality Tests
// ============================================================================

// TestNewPool tests creating Worker Pool
func TestNewPool(t *testing.T) {
	pool := NewPool(10)
	assert.NotNil(t, pool)
	assert.Equal(t, 0, pool.GetWorkerCount())
	assert.False(t, pool.IsStarted())
}

// TestPoolStart tests starting Worker Pool
func TestPoolStart(t *testing.T) {
	pool := NewPool(10)

	// Start 8 Workers
	err := pool.Start(8)
	require.NoError(t, err)
	assert.Equal(t, 8, pool.GetWorkerCount())
	assert.True(t, pool.IsStarted())

	// Try to start again
	err = pool.Start(4)
	assert.Error(t, err)

	pool.Stop()
}

// TestWorkerExecution tests Worker job execution
func TestWorkerExecution(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(1) // Single Worker
	require.NoError(t, err)

	// Submit 10 tasks
	taskCount := 10
	for i := 0; i < taskCount; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 1 * time.Second,
		}
		err := pool.Submit(task)
		require.NoError(t, err)
	}

	// Collect results
	results := make(map[types.JobID]Result)
	for i := 0; i < taskCount; i++ {
		result, err := pool.ReceiveResult()
		require.NoError(t, err)
		results[result.JobID] = result
	}

	// Verify all tasks received results
	assert.Equal(t, taskCount, len(results))

	pool.Stop()
}

// TestTimeout tests job timeout mechanism
func TestTimeout(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(1)
	require.NoError(t, err)

	// Submit timeout task (with very short timeout)
	task := Task{
		ID:      types.JobID("timeout-task"),
		Payload: map[string]interface{}{},
		Timeout: 1 * time.Millisecond, // Very short timeout
	}
	err = pool.Submit(task)
	require.NoError(t, err)

	// Receive result
	result, err := pool.ReceiveResult()
	require.NoError(t, err)

	// Verify task failed due to timeout
	assert.False(t, result.Success)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "deadline exceeded")

	pool.Stop()
}

// ============================================================================
// Concurrency Tests
// ============================================================================

// TestConcurrency tests concurrent execution
func TestConcurrency(t *testing.T) {
	pool := NewPool(100)
	workerCount := 8
	taskCount := 100

	err := pool.Start(workerCount)
	require.NoError(t, err)

	start := time.Now()

	// Quickly submit 100 tasks
	for i := 0; i < taskCount; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 2 * time.Second,
		}
		err := pool.Submit(task)
		require.NoError(t, err)
	}

	// Collect all results
	successCount := 0
	failCount := 0
	for i := 0; i < taskCount; i++ {
		result, err := pool.ReceiveResult()
		require.NoError(t, err)
		if result.Success {
			successCount++
		} else {
			failCount++
		}
	}

	duration := time.Since(start)

	// Verify results
	assert.Equal(t, taskCount, successCount+failCount)
	t.Logf("Processed %d tasks in %v with %d workers", taskCount, duration, workerCount)
	t.Logf("Success: %d, Failed: %d", successCount, failCount)

	// Concurrent execution should be much faster than serial
	// Assuming avg 250ms per task, serial would take 25s, concurrent should be < 10s
	assert.Less(t, duration, 10*time.Second)

	pool.Stop()
}

// TestConcurrentSubmit tests concurrent job submission
func TestConcurrentSubmit(t *testing.T) {
	pool := NewPool(100)
	err := pool.Start(4)
	require.NoError(t, err)

	taskCount := 50
	var wg sync.WaitGroup
	wg.Add(taskCount)

	// Concurrently submit tasks
	for i := 0; i < taskCount; i++ {
		go func(index int) {
			defer wg.Done()
			task := Task{
				ID:      types.JobID(fmt.Sprintf("task-%d", index)),
				Payload: map[string]interface{}{"index": index},
				Timeout: 1 * time.Second,
			}
			err := pool.Submit(task)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// Collect all results
	for i := 0; i < taskCount; i++ {
		_, err := pool.ReceiveResult()
		require.NoError(t, err)
	}

	pool.Stop()
}

// ============================================================================
// Graceful Shutdown Tests
// ============================================================================

// TestGracefulShutdown tests graceful shutdown
func TestGracefulShutdown(t *testing.T) {
	pool := NewPool(50)
	err := pool.Start(4)
	require.NoError(t, err)

	// Submit 50 tasks
	taskCount := 50
	for i := 0; i < taskCount; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 1 * time.Second,
		}
		err := pool.Submit(task)
		require.NoError(t, err)
	}

	// Wait for some tasks to complete
	completedCount := 10
	for i := 0; i < completedCount; i++ {
		_, err := pool.ReceiveResult()
		require.NoError(t, err)
	}

	// Record goroutine count before shutdown
	goroutinesBefore := runtime.NumGoroutine()

	// Graceful shutdown
	pool.Stop()

	// Verify all Worker goroutines have exited
	// Give some time for goroutine cleanup
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()

	// Worker goroutines should decrease
	assert.LessOrEqual(t, goroutinesAfter, goroutinesBefore)

	t.Logf("Goroutines before: %d, after: %d", goroutinesBefore, goroutinesAfter)
}

// TestStopBeforeStart tests stopping before starting
func TestStopBeforeStart(t *testing.T) {
	pool := NewPool(10)

	// Stopping before starting should not panic
	assert.NotPanics(t, func() {
		pool.Stop()
	})
}

// TestSubmitAfterStop tests submitting jobs after shutdown
func TestSubmitAfterStop(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(2)
	require.NoError(t, err)

	pool.Stop()

	// Submitting tasks after shutdown should return error
	task := Task{
		ID:      types.JobID("task-after-stop"),
		Payload: map[string]interface{}{},
		Timeout: 1 * time.Second,
	}
	err = pool.Submit(task)
	assert.Error(t, err)
	assert.Equal(t, ErrPoolClosed, err)
}

// ============================================================================
// Channel Buffer Tests
// ============================================================================

// TestChannelBuffer tests channel buffering mechanism
func TestChannelBuffer(t *testing.T) {
	bufferSize := 5
	pool := NewPool(bufferSize)

	// Start 1 Worker, let it process slowly
	err := pool.Start(1)
	require.NoError(t, err)

	// Quickly submit more tasks than buffer size
	taskCount := bufferSize + 3
	submitted := 0
	for i := 0; i < taskCount; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{},
			Timeout: 2 * time.Second,
		}
		err := pool.Submit(task)
		if err == nil {
			submitted++
		}
	}

	// Verify all tasks submitted successfully (some in buffer, some being processed)
	assert.Equal(t, taskCount, submitted)

	// Wait for all tasks to complete
	for i := 0; i < submitted; i++ {
		_, err := pool.ReceiveResult()
		assert.NoError(t, err)
	}

	pool.Stop()
}

// ============================================================================
// Error Handling Tests
// ============================================================================

// TestSubmitBeforeStart tests submitting jobs before starting
func TestSubmitBeforeStart(t *testing.T) {
	pool := NewPool(10)

	// Submitting tasks before starting should return error
	task := Task{
		ID:      types.JobID("task-before-start"),
		Payload: map[string]interface{}{},
		Timeout: 1 * time.Second,
	}
	err := pool.Submit(task)
	assert.Error(t, err)
	assert.Equal(t, ErrPoolNotStarted, err)
}

// TestReceiveResultAfterStop tests receiving results after shutdown
func TestReceiveResultAfterStop(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(2)
	require.NoError(t, err)

	pool.Stop()

	// Receiving results after shutdown should return error
	_, err = pool.ReceiveResult()
	assert.Error(t, err)
	assert.Equal(t, ErrPoolClosed, err)
}

// ============================================================================
// Worker Behavior Tests
// ============================================================================

// TestWorkerExecuteSuccess tests Worker successful execution
func TestWorkerExecuteSuccess(t *testing.T) {
	worker := &Worker{
		id:       1,
		taskCh:   make(chan Task),
		resultCh: make(chan Result, 1),
	}

	ctx := context.Background()
	payload := map[string]interface{}{"test": "data"}

	// Execute multiple times, at least one should succeed (90% success rate)
	successCount := 0
	attempts := 20
	for i := 0; i < attempts; i++ {
		err := worker.execute(ctx, payload)
		if err == nil {
			successCount++
		}
	}

	// Verify at least some succeeded
	assert.Greater(t, successCount, 0)
	t.Logf("Success rate: %d/%d", successCount, attempts)
}

// TestWorkerExecuteTimeout tests Worker execution timeout
func TestWorkerExecuteTimeout(t *testing.T) {
	worker := &Worker{
		id:       1,
		taskCh:   make(chan Task),
		resultCh: make(chan Result, 1),
	}

	// Create already timed-out context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // Ensure timeout

	payload := map[string]interface{}{"test": "data"}
	err := worker.execute(ctx, payload)

	// Verify timeout error
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// ============================================================================
// Benchmark Tests
// ============================================================================

// BenchmarkPoolSubmit tests job submission performance
func BenchmarkPoolSubmit(b *testing.B) {
	pool := NewPool(1000)
	pool.Start(8)
	defer pool.Stop()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 1 * time.Second,
		}
		pool.Submit(task)
	}
}

// BenchmarkPoolThroughput tests throughput
func BenchmarkPoolThroughput(b *testing.B) {
	pool := NewPool(1000)
	pool.Start(8)
	defer pool.Stop()

	// Receive results in background
	go func() {
		for {
			_, err := pool.ReceiveResult()
			if err != nil {
				return
			}
		}
	}()

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 1 * time.Second,
		}
		pool.Submit(task)
	}
}
