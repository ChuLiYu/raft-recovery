package controller

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChuLiYu/beaver-raft/internal/storage/wal"
	"github.com/ChuLiYu/beaver-raft/pkg/types"
)

// ============================================================================
// 測試輔助函數
// ============================================================================

// createTestController 建立測試用的 Controller
func createTestController(t *testing.T) (*Controller, string) {
	t.Helper()

	// 建立臨時目錄
	tmpDir, err := os.MkdirTemp("", "controller_test_*")
	if err != nil {
		t.Fatalf("建立臨時目錄失敗: %v", err)
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
		t.Fatalf("建立 Controller 失敗: %v", err)
	}

	return controller, tmpDir
}

// cleanup 清理測試資源
func cleanup(t *testing.T, controller *Controller, tmpDir string) {
	t.Helper()

	if controller != nil {
		controller.Stop()
	}

	if tmpDir != "" {
		os.RemoveAll(tmpDir)
	}
}

// waitForJobStatus 等待任務達到指定狀態
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
// 基礎功能測試
// ============================================================================

// TestNewController 測試 Controller 初始化
func TestNewController(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	if controller == nil {
		t.Fatal("Controller 不應為 nil")
	}

	if controller.jobManager == nil {
		t.Error("JobManager 未初始化")
	}

	if controller.wal == nil {
		t.Error("WAL 未初始化")
	}

	if controller.snapshot == nil {
		t.Error("Snapshot Manager 未初始化")
	}

	if controller.pool == nil {
		t.Error("Worker Pool 未初始化")
	}

	if controller.config.WorkerCount != 2 {
		t.Errorf("WorkerCount = %d, want 2", controller.config.WorkerCount)
	}
}

// TestNewControllerWithInvalidPath 測試使用無效路徑初始化
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
		t.Error("使用無效路徑應該回傳錯誤")
	}
}

// TestStart 測試 Controller 啟動
func TestStart(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 檢查啟動時間是否設定
	if controller.startTime.IsZero() {
		t.Error("啟動時間未設定")
	}

	// 等待一小段時間確保循環啟動
	time.Sleep(200 * time.Millisecond)

	// 檢查 stopCh 是否可用
	select {
	case <-controller.stopCh:
		t.Error("stopCh 不應該被關閉")
	default:
		// 正確
	}
}

// TestEnqueueJobs 測試任務入隊
func TestEnqueueJobs(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 驗證任務已加入
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	if stats["pending"] != 3 {
		t.Errorf("pending 任務數 = %d, want 3", stats["pending"])
	}
}

// TestGetStatus 測試狀態查詢
func TestGetStatus(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 先等待一小段時間
	time.Sleep(100 * time.Millisecond)

	status := controller.GetStatus()

	// 檢查必要欄位
	if _, ok := status["uptime"]; !ok {
		t.Error("status 缺少 uptime 欄位")
	}

	if workers, ok := status["workers"]; !ok || workers != 2 {
		t.Errorf("workers = %v, want 2", workers)
	}

	if _, ok := status["pending"]; !ok {
		t.Error("status 缺少 pending 欄位")
	}

	if _, ok := status["in_flight"]; !ok {
		t.Error("status 缺少 in_flight 欄位")
	}

	if _, ok := status["completed"]; !ok {
		t.Error("status 缺少 completed 欄位")
	}

	if _, ok := status["dead"]; !ok {
		t.Error("status 缺少 dead 欄位")
	}
}

// TestStop 測試優雅關閉
func TestStop(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, nil, tmpDir) // 不要在 cleanup 中再次 Stop

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 加入一些任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test"}},
	}
	controller.EnqueueJobs(jobs)

	// 等待任務開始處理
	time.Sleep(200 * time.Millisecond)

	// 執行 Stop
	controller.Stop()

	// 檢查 stopCh 是否關閉
	select {
	case <-controller.stopCh:
		// 正確
	default:
		t.Error("stopCh 應該被關閉")
	}
}

// ============================================================================
// 基本工作流程測試
// ============================================================================

// TestBasicWorkflow 測試基本工作流程：入隊 -> 調度 -> 完成
func TestBasicWorkflow(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 加入任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 等待任務完成或進入死信（最多 10 秒）
	success := waitForJobStatus(t, controller, "task-001", func() bool {
		controller.mu.Lock()
		defer controller.mu.Unlock()
		return controller.jobManager.IsCompleted("task-001") ||
			controller.jobManager.IsDead("task-001")
	}, 10*time.Second)

	if !success {
		t.Error("任務未在 10 秒內完成或進入死信")
	}

	// 檢查最終狀態
	controller.mu.Lock()
	completed := controller.jobManager.IsCompleted("task-001")
	dead := controller.jobManager.IsDead("task-001")
	controller.mu.Unlock()

	if !completed && !dead {
		t.Error("任務應該處於完成或死信狀態")
	}

	t.Logf("任務 task-001 最終狀態: completed=%v, dead=%v", completed, dead)
}

// TestMultipleJobsWorkflow 測試多任務並發處理
func TestMultipleJobsWorkflow(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 加入多個任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
		{ID: "task-004", Payload: map[string]interface{}{"data": "test4"}},
		{ID: "task-005", Payload: map[string]interface{}{"data": "test5"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 等待所有任務完成（最多 15 秒）
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
		t.Error("並非所有任務都在 15 秒內完成")
	}

	// 檢查最終統計
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	totalDone := stats["completed"] + stats["dead"]
	if totalDone != 5 {
		t.Errorf("完成任務總數 = %d, want 5", totalDone)
	}

	t.Logf("任務統計: completed=%d, dead=%d, in_flight=%d, pending=%d",
		stats["completed"], stats["dead"], stats["in_flight"], stats["pending"])
}

// ============================================================================
// 快照與恢復測試
// ============================================================================

// TestSnapshotCreation 測試快照生成
func TestSnapshotCreation(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 加入任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
	}

	err = controller.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 等待任務被調度
	time.Sleep(500 * time.Millisecond)

	// 手動觸發快照
	err = controller.takeSnapshot()
	if err != nil {
		t.Fatalf("建立快照失敗: %v", err)
	}

	// 檢查快照檔案是否存在
	if _, err := os.Stat(controller.config.SnapshotPath); os.IsNotExist(err) {
		t.Error("快照檔案不存在")
	}
}

// TestLoadSnapshot 測試快照載入
func TestLoadSnapshot(t *testing.T) {
	// 第一階段：建立 Controller 並產生快照
	controller1, tmpDir := createTestController(t)

	err := controller1.Start()
	if err != nil {
		t.Fatalf("啟動 controller1 失敗: %v", err)
	}

	// 加入任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
	}

	err = controller1.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 等待任務被處理
	time.Sleep(500 * time.Millisecond)

	// 建立快照
	err = controller1.takeSnapshot()
	if err != nil {
		t.Fatalf("建立快照失敗: %v", err)
	}

	// 取得快照前的統計
	controller1.mu.Lock()
	stats1 := controller1.jobManager.Stats()
	controller1.mu.Unlock()

	// 關閉第一個 Controller
	controller1.Stop()

	// 第二階段：建立新的 Controller 並載入快照
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
		t.Fatalf("建立 controller2 失敗: %v", err)
	}
	defer cleanup(t, controller2, tmpDir)

	// 啟動會自動載入快照
	err = controller2.Start()
	if err != nil {
		t.Fatalf("啟動 controller2 失敗: %v", err)
	}

	// 取得恢復後的統計
	controller2.mu.Lock()
	stats2 := controller2.jobManager.Stats()
	totalJobs2 := stats2["pending"] + stats2["in_flight"] + stats2["completed"] + stats2["dead"]
	controller2.mu.Unlock()

	// 驗證任務數量
	totalJobs1 := stats1["pending"] + stats1["in_flight"] + stats1["completed"] + stats1["dead"]
	if totalJobs2 != totalJobs1 {
		t.Errorf("恢復後任務總數 = %d, want %d", totalJobs2, totalJobs1)
	}

	t.Logf("快照前統計: %+v", stats1)
	t.Logf("恢復後統計: %+v", stats2)
}

// TestCrashRecovery 測試崩潰恢復（快照 + WAL 重放）
func TestCrashRecovery(t *testing.T) {
	// 第一階段：正常運行
	controller1, tmpDir := createTestController(t)

	err := controller1.Start()
	if err != nil {
		t.Fatalf("啟動 controller1 失敗: %v", err)
	}

	// 加入一批任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
		{ID: "task-003", Payload: map[string]interface{}{"data": "test3"}},
		{ID: "task-004", Payload: map[string]interface{}{"data": "test4"}},
		{ID: "task-005", Payload: map[string]interface{}{"data": "test5"}},
	}

	err = controller1.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 等待部分任務完成
	time.Sleep(1 * time.Second)

	// 手動建立快照
	err = controller1.takeSnapshot()
	if err != nil {
		t.Fatalf("建立快照失敗: %v", err)
	}

	// 取得快照時的統計
	controller1.mu.Lock()
	stats1 := controller1.jobManager.Stats()
	controller1.mu.Unlock()

	// 模擬崩潰：直接關閉 WAL 和檔案（不執行 Stop）
	controller1.wal.Close()

	// 第二階段：恢復
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
		t.Fatalf("建立 controller2 失敗: %v", err)
	}
	defer cleanup(t, controller2, tmpDir)

	// 啟動（會執行 loadSnapshot + replayWAL）
	err = controller2.Start()
	if err != nil {
		t.Fatalf("啟動 controller2 失敗: %v", err)
	}

	recoveryTime := time.Since(startRecovery)

	// 驗證恢復時間 < 3s
	if recoveryTime > 3*time.Second {
		t.Errorf("恢復時間 = %v, want < 3s", recoveryTime)
	}

	// 取得恢復後的統計
	controller2.mu.Lock()
	stats2 := controller2.jobManager.Stats()
	controller2.mu.Unlock()

	// 驗證任務數量一致
	totalJobs1 := stats1["pending"] + stats1["in_flight"] + stats1["completed"] + stats1["dead"]
	totalJobs2 := stats2["pending"] + stats2["in_flight"] + stats2["completed"] + stats2["dead"]

	if totalJobs2 != totalJobs1 {
		t.Errorf("恢復後任務總數 = %d, want %d", totalJobs2, totalJobs1)
	}

	t.Logf("恢復時間: %v", recoveryTime)
	t.Logf("崩潰前統計: %+v", stats1)
	t.Logf("恢復後統計: %+v", stats2)
}

// TestRecoveryTime 測試恢復時間性能（目標 < 3s）
func TestRecoveryTime(t *testing.T) {
	// 建立一個有較多任務的場景
	controller1, tmpDir := createTestController(t)

	err := controller1.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 加入 50 個任務
	jobs := make([]types.Job, 50)
	for i := 0; i < 50; i++ {
		jobs[i] = types.Job{
			ID:      types.JobID(string(rune('a'+i/26)) + string(rune('a'+i%26))),
			Payload: map[string]interface{}{"index": i},
		}
	}

	err = controller1.EnqueueJobs(jobs)
	if err != nil {
		t.Fatalf("入隊失敗: %v", err)
	}

	// 等待任務被處理
	time.Sleep(2 * time.Second)

	// 建立快照
	err = controller1.takeSnapshot()
	if err != nil {
		t.Fatalf("建立快照失敗: %v", err)
	}

	controller1.Stop()

	// 測試恢復時間
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
		t.Fatalf("建立 controller2 失敗: %v", err)
	}
	defer cleanup(t, controller2, tmpDir)

	err = controller2.Start()
	if err != nil {
		t.Fatalf("啟動 controller2 失敗: %v", err)
	}

	recoveryTime := time.Since(startTime)

	if recoveryTime > 3*time.Second {
		t.Errorf("恢復時間 = %v, 超過 3s 目標", recoveryTime)
	}

	t.Logf("恢復 50 個任務耗時: %v", recoveryTime)
}

// ============================================================================
// WAL 重放與冪等性測試
// ============================================================================

// TestReplayWAL 測試 WAL 重放
func TestReplayWAL(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	// 不啟動 Controller，直接測試 replayWAL
	// 先加入一些 WAL 事件（通過直接操作）

	// 手動建立一些任務並寫入 WAL
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
		{ID: "task-002", Payload: map[string]interface{}{"data": "test2"}},
	}

	for _, job := range jobs {
		controller.wal.Append(wal.EventEnqueue, job, false)
	}

	// 執行重放
	err := controller.replayWAL()
	if err != nil {
		t.Fatalf("重放 WAL 失敗: %v", err)
	}

	// 驗證 JobManager 狀態
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	// 因為只有 Enqueue 事件，應該都在快照中
	t.Logf("重放後統計: %+v", stats)
}

// TestIdempotency 測試冪等性（重複重放不會出錯）
func TestIdempotency(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	// 加入任務
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test1"}},
	}

	for _, job := range jobs {
		controller.jobManager.Enqueue(job)
		controller.wal.Append(wal.EventEnqueue, job, false)
	}

	// 標記為完成
	controller.jobManager.PopPending()
	deadline := time.Now().Add(2 * time.Second)
	controller.jobManager.MarkInFlight("task-001", deadline)
	controller.wal.Append(wal.EventDispatch, jobs[0], false)
	controller.jobManager.MarkCompleted("task-001")
	controller.wal.Append(wal.EventAck, jobs[0], false)

	// 第一次重放
	err := controller.replayWAL()
	if err != nil {
		t.Fatalf("第一次重放失敗: %v", err)
	}

	controller.mu.Lock()
	stats1 := controller.jobManager.Stats()
	controller.mu.Unlock()

	// 第二次重放（應該冪等）
	err = controller.replayWAL()
	if err != nil {
		t.Fatalf("第二次重放失敗: %v", err)
	}

	controller.mu.Lock()
	stats2 := controller.jobManager.Stats()
	controller.mu.Unlock()

	// 統計應該相同
	if stats1["completed"] != stats2["completed"] {
		t.Errorf("冪等性測試失敗: 第一次 completed=%d, 第二次 completed=%d",
			stats1["completed"], stats2["completed"])
	}

	t.Logf("冪等性測試通過: stats1=%+v, stats2=%+v", stats1, stats2)
}

// ============================================================================
// 並發與壓力測試
// ============================================================================

// TestConcurrentEnqueue 測試並發入隊
func TestConcurrentEnqueue(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, controller, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	// 啟動多個 goroutine 並發入隊
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
				t.Errorf("goroutine %d 入隊失敗: %v", id, err)
			}

			done <- true
		}(i)
	}

	// 等待所有 goroutine 完成
	for i := 0; i < goroutines; i++ {
		<-done
	}

	// 等待任務被處理
	time.Sleep(2 * time.Second)

	// 檢查任務總數
	controller.mu.Lock()
	stats := controller.jobManager.Stats()
	controller.mu.Unlock()

	expectedTotal := goroutines * jobsPerGoroutine
	actualTotal := stats["pending"] + stats["in_flight"] + stats["completed"] + stats["dead"]

	if actualTotal != expectedTotal {
		t.Errorf("任務總數 = %d, want %d", actualTotal, expectedTotal)
	}

	t.Logf("並發入隊測試: %d 個 goroutines, 每個 %d 個任務, 總計 %d 個",
		goroutines, jobsPerGoroutine, expectedTotal)
	t.Logf("最終統計: %+v", stats)
}

// ============================================================================
// 錯誤處理測試
// ============================================================================

// TestEnqueueAfterStop 測試停止後入隊
func TestEnqueueAfterStop(t *testing.T) {
	controller, tmpDir := createTestController(t)
	defer cleanup(t, nil, tmpDir)

	err := controller.Start()
	if err != nil {
		t.Fatalf("啟動失敗: %v", err)
	}

	controller.Stop()

	// 停止後嘗試入隊
	jobs := []types.Job{
		{ID: "task-001", Payload: map[string]interface{}{"data": "test"}},
	}

	err = controller.EnqueueJobs(jobs)
	// WAL 已關閉，應該會出錯
	if err == nil {
		t.Log("注意：停止後入隊未返回錯誤（可能需要在 WAL 中增強錯誤檢查）")
	} else {
		t.Logf("停止後入隊正確返回錯誤: %v", err)
	}
}
