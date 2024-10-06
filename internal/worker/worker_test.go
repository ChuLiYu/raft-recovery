package worker

// ============================================================================
// Worker Pool 測試檔案
// 職責：驗證並發執行、超時機制、優雅關閉
// ============================================================================

import (
	"context"
	"fmt"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/ChuLiYu/beaver-raft/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 基礎功能測試
// ============================================================================

// TestNewPool 測試建立 Worker Pool
func TestNewPool(t *testing.T) {
	pool := NewPool(10)
	assert.NotNil(t, pool)
	assert.Equal(t, 0, pool.GetWorkerCount())
	assert.False(t, pool.IsStarted())
}

// TestPoolStart 測試啟動 Worker Pool
func TestPoolStart(t *testing.T) {
	pool := NewPool(10)

	// 啟動 8 個 Worker
	err := pool.Start(8)
	require.NoError(t, err)
	assert.Equal(t, 8, pool.GetWorkerCount())
	assert.True(t, pool.IsStarted())

	// 嘗試重複啟動
	err = pool.Start(4)
	assert.Error(t, err)

	pool.Stop()
}

// TestWorkerExecution 測試 Worker 執行任務
func TestWorkerExecution(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(1) // 單一 Worker
	require.NoError(t, err)

	// 提交 10 個任務
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

	// 收集結果
	results := make(map[types.JobID]Result)
	for i := 0; i < taskCount; i++ {
		result, err := pool.ReceiveResult()
		require.NoError(t, err)
		results[result.JobID] = result
	}

	// 驗證所有任務都收到結果
	assert.Equal(t, taskCount, len(results))

	pool.Stop()
}

// TestTimeout 測試任務超時機制
func TestTimeout(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(1)
	require.NoError(t, err)

	// 提交超時任務（超時時間設得很短）
	task := Task{
		ID:      types.JobID("timeout-task"),
		Payload: map[string]interface{}{},
		Timeout: 1 * time.Millisecond, // 極短的超時時間
	}
	err = pool.Submit(task)
	require.NoError(t, err)

	// 接收結果
	result, err := pool.ReceiveResult()
	require.NoError(t, err)

	// 驗證任務因超時而失敗
	assert.False(t, result.Success)
	assert.Error(t, result.Error)
	assert.Contains(t, result.Error.Error(), "deadline exceeded")

	pool.Stop()
}

// ============================================================================
// 並發測試
// ============================================================================

// TestConcurrency 測試並發執行
func TestConcurrency(t *testing.T) {
	pool := NewPool(100)
	workerCount := 8
	taskCount := 100

	err := pool.Start(workerCount)
	require.NoError(t, err)

	start := time.Now()

	// 快速提交 100 個任務
	for i := 0; i < taskCount; i++ {
		task := Task{
			ID:      types.JobID(fmt.Sprintf("task-%d", i)),
			Payload: map[string]interface{}{"index": i},
			Timeout: 2 * time.Second,
		}
		err := pool.Submit(task)
		require.NoError(t, err)
	}

	// 收集所有結果
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

	// 驗證結果
	assert.Equal(t, taskCount, successCount+failCount)
	t.Logf("Processed %d tasks in %v with %d workers", taskCount, duration, workerCount)
	t.Logf("Success: %d, Failed: %d", successCount, failCount)

	// 並發執行應該比串行快很多
	// 假設每個任務平均 250ms，串行需要 25s，並發應該 < 10s
	assert.Less(t, duration, 10*time.Second)

	pool.Stop()
}

// TestConcurrentSubmit 測試並發提交任務
func TestConcurrentSubmit(t *testing.T) {
	pool := NewPool(100)
	err := pool.Start(4)
	require.NoError(t, err)

	taskCount := 50
	var wg sync.WaitGroup
	wg.Add(taskCount)

	// 並發提交任務
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

	// 收集所有結果
	for i := 0; i < taskCount; i++ {
		_, err := pool.ReceiveResult()
		require.NoError(t, err)
	}

	pool.Stop()
}

// ============================================================================
// 優雅關閉測試
// ============================================================================

// TestGracefulShutdown 測試優雅關閉
func TestGracefulShutdown(t *testing.T) {
	pool := NewPool(50)
	err := pool.Start(4)
	require.NoError(t, err)

	// 提交 50 個任務
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

	// 等待部分任務完成
	completedCount := 10
	for i := 0; i < completedCount; i++ {
		_, err := pool.ReceiveResult()
		require.NoError(t, err)
	}

	// 記錄關閉前的 goroutine 數量
	goroutinesBefore := runtime.NumGoroutine()

	// 優雅關閉
	pool.Stop()

	// 驗證所有 Worker goroutine 都已退出
	// 給一點時間讓 goroutine 清理
	time.Sleep(100 * time.Millisecond)
	goroutinesAfter := runtime.NumGoroutine()

	// Worker goroutine 應該減少
	assert.LessOrEqual(t, goroutinesAfter, goroutinesBefore)

	t.Logf("Goroutines before: %d, after: %d", goroutinesBefore, goroutinesAfter)
}

// TestStopBeforeStart 測試在啟動前關閉
func TestStopBeforeStart(t *testing.T) {
	pool := NewPool(10)

	// 未啟動時關閉不應該 panic
	assert.NotPanics(t, func() {
		pool.Stop()
	})
}

// TestSubmitAfterStop 測試關閉後提交任務
func TestSubmitAfterStop(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(2)
	require.NoError(t, err)

	pool.Stop()

	// 關閉後提交任務應該返回錯誤
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
// 通道緩衝測試
// ============================================================================

// TestChannelBuffer 測試通道緩衝機制
func TestChannelBuffer(t *testing.T) {
	bufferSize := 5
	pool := NewPool(bufferSize)

	// 啟動 1 個 Worker，但讓它慢慢處理
	err := pool.Start(1)
	require.NoError(t, err)

	// 快速提交超過緩衝大小的任務
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

	// 驗證任務都成功提交（可能有些在 buffer，有些在 Worker 處理）
	assert.Equal(t, taskCount, submitted)

	// 等待所有任務完成
	for i := 0; i < submitted; i++ {
		_, err := pool.ReceiveResult()
		assert.NoError(t, err)
	}

	pool.Stop()
}

// ============================================================================
// 錯誤處理測試
// ============================================================================

// TestSubmitBeforeStart 測試啟動前提交任務
func TestSubmitBeforeStart(t *testing.T) {
	pool := NewPool(10)

	// 未啟動時提交任務應該返回錯誤
	task := Task{
		ID:      types.JobID("task-before-start"),
		Payload: map[string]interface{}{},
		Timeout: 1 * time.Second,
	}
	err := pool.Submit(task)
	assert.Error(t, err)
	assert.Equal(t, ErrPoolNotStart, err)
}

// TestReceiveResultAfterStop 測試關閉後接收結果
func TestReceiveResultAfterStop(t *testing.T) {
	pool := NewPool(10)
	err := pool.Start(2)
	require.NoError(t, err)

	pool.Stop()

	// 關閉後接收結果應該返回錯誤
	_, err = pool.ReceiveResult()
	assert.Error(t, err)
	assert.Equal(t, ErrPoolClosed, err)
}

// ============================================================================
// Worker 行為測試
// ============================================================================

// TestWorkerExecuteSuccess 測試 Worker 執行成功
func TestWorkerExecuteSuccess(t *testing.T) {
	worker := &Worker{
		id:       1,
		taskCh:   make(chan Task),
		resultCh: make(chan Result, 1),
	}

	ctx := context.Background()
	payload := map[string]interface{}{"test": "data"}

	// 執行多次，至少有一次會成功（90% 成功率）
	successCount := 0
	attempts := 20
	for i := 0; i < attempts; i++ {
		err := worker.execute(ctx, payload)
		if err == nil {
			successCount++
		}
	}

	// 驗證至少有一些成功
	assert.Greater(t, successCount, 0)
	t.Logf("Success rate: %d/%d", successCount, attempts)
}

// TestWorkerExecuteTimeout 測試 Worker 執行超時
func TestWorkerExecuteTimeout(t *testing.T) {
	worker := &Worker{
		id:       1,
		taskCh:   make(chan Task),
		resultCh: make(chan Result, 1),
	}

	// 建立已超時的 context
	ctx, cancel := context.WithTimeout(context.Background(), 1*time.Nanosecond)
	defer cancel()

	time.Sleep(10 * time.Millisecond) // 確保超時

	payload := map[string]interface{}{"test": "data"}
	err := worker.execute(ctx, payload)

	// 驗證超時錯誤
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

// ============================================================================
// Benchmark 測試
// ============================================================================

// BenchmarkPoolSubmit 測試提交任務的效能
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

// BenchmarkPoolThroughput 測試吞吐量
func BenchmarkPoolThroughput(b *testing.B) {
	pool := NewPool(1000)
	pool.Start(8)
	defer pool.Stop()

	// 在背景接收結果
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
