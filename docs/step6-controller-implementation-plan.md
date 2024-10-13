# Step 6: Controller 核心實作規劃

## 📋 目錄結構檢視

### 已完成模組
```
✅ pkg/types/types.go               - 統一的領域模型（Job, JobStatus, SnapshotData）
✅ internal/jobmanager/             - 任務狀態管理
   ├── job_manager.go               - 核心邏輯（Enqueue, PopPending, MarkInFlight 等）
   └── job_manager_test.go          - 測試覆蓋
✅ internal/worker/                 - Worker Pool
   ├── worker.go                    - Worker 執行邏輯
   ├── worker_pool.go               - Pool 管理（Start, Submit, Stop）
   ├── types.go                     - Task, Result 定義
   └── worker_test.go               - 測試覆蓋
✅ internal/storage/wal/            - Write-Ahead Log
   ├── wal.go                       - WAL 核心（Append, Replay, Rotate）
   ├── types.go                     - Event 定義
   ├── checksum.go                  - 校驗和計算
   └── wal_test.go                  - 測試覆蓋
✅ internal/snapshot/               - 快照管理
   ├── snapshot_manager.go          - 快照讀寫（Write, Load）
   └── snapshot_manager_test.go     - 測試覆蓋
```

### 待實作模組
```
⏳ internal/controller/             - Controller 核心（本次實作）
   ├── controller.go                - 主要實作檔案
   ├── controller_test.go           - 單元測試
   └── integration_test.go          - 整合測試（崩潰恢復）
```

---

## 🎯 Controller 架構設計

### 核心職責
1. **協調所有模組**：整合 JobManager, WAL, Snapshot, WorkerPool
2. **實現四個核心循環**：
   - `dispatchLoop()` - 調度待處理任務給 Worker Pool
   - `resultLoop()` - 處理 Worker 執行結果
   - `timeoutLoop()` - 檢測並處理超時任務
   - `snapshotLoop()` - 定期生成快照
3. **處理崩潰恢復**：loadSnapshot → replayWAL → 重新調度
4. **確保狀態一致性與冪等性**

### 設計要點

#### 1. 鎖的使用策略
```go
// 原則：最小化鎖的範圍，避免死鎖

// ❌ 錯誤：長時間持有鎖
func dispatchLoop() {
    mu.Lock()
    defer mu.Unlock()  // 整個循環都持有鎖
    for {
        job := jobManager.PopPending()
        pool.Submit(job)  // 可能阻塞
    }
}

// ✅ 正確：鎖範圍最小化
func dispatchLoop() {
    for {
        mu.Lock()
        job := jobManager.PopPending()
        mu.Unlock()
        
        if job != nil {
            wal.Append("DISPATCH", *job, false)
            
            mu.Lock()
            jobManager.MarkInFlight(job.ID, deadline)
            mu.Unlock()
            
            pool.Submit(task)  // 不持有鎖
        }
    }
}
```

#### 2. WAL 寫入時機
```go
// 原則：Write-Ahead，先記錄意圖再執行

// 調度任務：DISPATCH 先寫 WAL，再 MarkInFlight
wal.Append("DISPATCH", job, false)
jobManager.MarkInFlight(jobID, deadline)

// 確認完成：ACK 先寫 WAL，再 MarkCompleted
wal.Append("ACK", job, false)
jobManager.MarkCompleted(jobID)

// 重試任務：RETRY 先寫 WAL，再 Requeue
wal.Append("RETRY", job, false)
jobManager.Requeue(jobID)
```

#### 3. 冪等性設計
```go
// 重放 WAL 時需要檢查冪等性
func replayWAL() error {
    handler := func(event Event) error {
        switch event.Type {
        case "DISPATCH":
            // 已完成或已死亡的任務不再調度
            if jobManager.IsCompleted(event.JobID) || 
               jobManager.IsDead(event.JobID) {
                return nil  // 跳過
            }
            jobManager.MarkInFlight(event.JobID, deadline)
            
        case "ACK":
            // 已完成的任務不再重複標記
            if !jobManager.IsCompleted(event.JobID) {
                jobManager.MarkCompleted(event.JobID)
            }
        }
        return nil
    }
    return wal.Replay(handler)
}
```

#### 4. 優雅關閉流程
```go
func (c *Controller) Stop() {
    // 1. 發送停止訊號給所有循環
    close(c.stopCh)
    
    // 2. 等待 Worker Pool 完成當前任務
    c.pool.Stop()
    
    // 3. 最後一次快照（保存當前狀態）
    c.mu.Lock()
    data := c.jobManager.Snapshot()
    data.LastSeq = c.wal.GetLastSeq()
    c.mu.Unlock()
    
    c.snapshot.Write(data)
    
    // 4. 關閉 WAL
    c.wal.Close()
}
```

---

## 📝 實作清單（分 4 天完成）

### Day 1: 基礎結構與恢復流程

#### 任務 1.1: 定義 Controller 結構 (30 分鐘)
```go
// internal/controller/controller.go

package controller

import (
    "sync"
    "time"
    
    "github.com/ChuLiYu/beaver-raft/internal/jobmanager"
    "github.com/ChuLiYu/beaver-raft/internal/snapshot"
    "github.com/ChuLiYu/beaver-raft/internal/storage/wal"
    "github.com/ChuLiYu/beaver-raft/internal/worker"
    "github.com/ChuLiYu/beaver-raft/pkg/types"
)

// Config Controller 配置
type Config struct {
    WorkerCount      int           // Worker 數量
    TaskTimeout      time.Duration // 任務超時時間
    SnapshotInterval time.Duration // 快照間隔
    MaxRetry         int           // 最大重試次數
    WALPath          string        // WAL 檔案路徑
    SnapshotPath     string        // 快照檔案路徑
    WALBufferSize    int           // WAL 批次緩衝大小
}

// Controller 核心控制器
type Controller struct {
    mu          sync.Mutex               // 保護 jobManager 操作
    jobManager  *jobmanager.JobManager   // 任務狀態管理
    wal         *wal.WAL                 // Write-Ahead Log
    snapshot    *snapshot.Manager        // 快照管理
    pool        *worker.Pool             // Worker Pool
    config      Config                   // 配置
    stopCh      chan struct{}            // 停止訊號
    startTime   time.Time                // 啟動時間（用於統計）
}

// NewController 建立新的 Controller 實例
func NewController(config Config) (*Controller, error) {
    // TODO: 實作
    return nil, nil
}
```

**驗證方式**：
```bash
cd internal/controller
go build .
```

---

#### 任務 1.2: 實作 NewController (1 小時)
```go
func NewController(config Config) (*Controller, error) {
    // 1. 建立 JobManager
    jobManager := jobmanager.NewJobManager()
    
    // 2. 開啟 WAL
    walInstance, err := wal.NewWAL(config.WALPath, false)
    if err != nil {
        return nil, fmt.Errorf("failed to open WAL: %w", err)
    }
    
    // 3. 建立 Snapshot Manager
    snapshotMgr := snapshot.NewManager(config.SnapshotPath)
    
    // 4. 建立 Worker Pool
    pool := worker.NewPool(config.WALBufferSize)
    
    return &Controller{
        jobManager: jobManager,
        wal:        walInstance,
        snapshot:   snapshotMgr,
        pool:       pool,
        config:     config,
        stopCh:     make(chan struct{}),
    }, nil
}
```

**測試**：
```go
// controller_test.go
func TestNewController(t *testing.T) {
    config := Config{
        WorkerCount:      4,
        TaskTimeout:      30 * time.Second,
        SnapshotInterval: 10 * time.Second,
        MaxRetry:         3,
        WALPath:          "/tmp/test-wal.log",
        SnapshotPath:     "/tmp/test-snapshot.json",
        WALBufferSize:    100,
    }
    
    ctrl, err := NewController(config)
    assert.NoError(t, err)
    assert.NotNil(t, ctrl)
}
```

---

#### 任務 1.3: 實作 loadSnapshot (1 小時)
```go
// loadSnapshot 從快照恢復狀態
func (c *Controller) loadSnapshot() error {
    start := time.Now()
    
    // 載入快照
    data, err := c.snapshot.Load()
    if err != nil {
        return fmt.Errorf("failed to load snapshot: %w", err)
    }
    
    // 恢復 JobManager 狀態
    c.mu.Lock()
    if err := c.jobManager.Restore(data); err != nil {
        c.mu.Unlock()
        return fmt.Errorf("failed to restore state: %w", err)
    }
    c.mu.Unlock()
    
    recoveryTime := time.Since(start)
    
    // 記錄恢復時間（目標 < 3s）
    if recoveryTime > 3*time.Second {
        log.Warn("Recovery time exceeds 3s", 
            "duration", recoveryTime)
    }
    
    log.Info("Snapshot loaded", 
        "duration", recoveryTime,
        "jobs", len(data.Jobs))
    
    return nil
}
```

**注意**：需要在 JobManager 中實作 `Restore` 方法：
```go
// internal/jobmanager/job_manager.go

// Restore 從快照恢復狀態
func (jm *JobManager) Restore(data types.SnapshotData) error {
    jm.mu.Lock()
    defer jm.mu.Unlock()
    
    // 清空現有狀態
    jm.jobs = make(map[types.JobID]*types.Job)
    jm.queue = make([]types.JobID, 0)
    jm.inFlight = make(map[types.JobID]*types.Job)
    jm.completed = make(map[types.JobID]*types.Job)
    jm.dead = make(map[types.JobID]*types.Job)
    
    // 恢復所有任務
    for jobID, job := range data.Jobs {
        jm.jobs[jobID] = job
        
        // 根據狀態分類
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

// Snapshot 生成快照資料
func (jm *JobManager) Snapshot() types.SnapshotData {
    jm.mu.RLock()
    defer jm.mu.RUnlock()
    
    // 深拷貝所有任務
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
```

---

#### 任務 1.4: 實作 replayWAL (2 小時)
```go
// replayWAL 重放 WAL 事件
func (c *Controller) replayWAL() error {
    handler := func(event wal.Event) error {
        c.mu.Lock()
        defer c.mu.Unlock()
        
        switch event.Type {
        case wal.EventEnqueue:
            // 通常快照已包含，可跳過
            
        case wal.EventDispatch:
            // 檢查冪等性
            if c.jobManager.IsCompleted(event.JobID) || 
               c.jobManager.IsDead(event.JobID) {
                return nil
            }
            
            // 標記為執行中
            deadline := time.Now().Add(c.config.TaskTimeout)
            return c.jobManager.MarkInFlight(event.JobID, deadline)
            
        case wal.EventAck:
            // 已完成則跳過
            if c.jobManager.IsCompleted(event.JobID) {
                return nil
            }
            return c.jobManager.MarkCompleted(event.JobID)
            
        case wal.EventRetry:
            return c.jobManager.Requeue(event.JobID)
            
        case wal.EventTimeout:
            return c.jobManager.Requeue(event.JobID)
            
        case wal.EventDead:
            return c.jobManager.MarkDead(event.JobID)
        }
        
        return nil
    }
    
    return c.wal.Replay(handler)
}
```

**需要在 JobManager 中添加查詢方法**：
```go
// IsCompleted 檢查任務是否已完成
func (jm *JobManager) IsCompleted(jobID types.JobID) bool {
    jm.mu.RLock()
    defer jm.mu.RUnlock()
    _, exists := jm.completed[jobID]
    return exists
}

// IsDead 檢查任務是否已死亡
func (jm *JobManager) IsDead(jobID types.JobID) bool {
    jm.mu.RLock()
    defer jm.mu.RUnlock()
    _, exists := jm.dead[jobID]
    return exists
}

// GetJob 取得任務
func (jm *JobManager) GetJob(jobID types.JobID) *types.Job {
    jm.mu.RLock()
    defer jm.mu.RUnlock()
    return jm.jobs[jobID]
}
```

---

#### 任務 1.5: 實作 Start 方法 (1 小時)
```go
// Start 啟動 Controller
func (c *Controller) Start() error {
    c.startTime = time.Now()
    
    // 1. 恢復階段
    log.Info("Starting recovery...")
    
    if err := c.loadSnapshot(); err != nil {
        return fmt.Errorf("loadSnapshot failed: %w", err)
    }
    
    if err := c.replayWAL(); err != nil {
        return fmt.Errorf("replayWAL failed: %w", err)
    }
    
    log.Info("Recovery completed", 
        "duration", time.Since(c.startTime))
    
    // 2. 啟動 Worker Pool
    if err := c.pool.Start(c.config.WorkerCount); err != nil {
        return fmt.Errorf("failed to start worker pool: %w", err)
    }
    
    // 3. 啟動四個核心循環
    go c.dispatchLoop()
    go c.resultLoop()
    go c.timeoutLoop()
    go c.snapshotLoop()
    
    log.Info("Controller started", "workers", c.config.WorkerCount)
    return nil
}
```

**Day 1 驗證**：
```bash
go test -v ./internal/controller/ -run TestStart
```

---

### Day 2: 實作四個核心循環

#### 任務 2.1: 實作 dispatchLoop (2 小時)
```go
// dispatchLoop 調度待處理任務
func (c *Controller) dispatchLoop() {
    ticker := time.NewTicker(100 * time.Millisecond)
    defer ticker.Stop()
    
    for {
        select {
        case <-c.stopCh:
            log.Info("Dispatch loop stopped")
            return
            
        case <-ticker.C:
            // 取出待處理任務
            c.mu.Lock()
            job := c.jobManager.PopPending()
            c.mu.Unlock()
            
            if job == nil {
                continue
            }
            
            // 先寫 WAL（Write-Ahead）
            if err := c.wal.Append(wal.EventDispatch, *job, false); err != nil {
                log.Error("Failed to append DISPATCH event", "error", err)
                continue
            }
            
            // 標記為執行中
            deadline := time.Now().Add(c.config.TaskTimeout)
            c.mu.Lock()
            if err := c.jobManager.MarkInFlight(job.ID, deadline); err != nil {
                log.Error("Failed to mark in-flight", "error", err)
                c.mu.Unlock()
                continue
            }
            c.mu.Unlock()
            
            // 提交給 Worker Pool
            task := worker.Task{
                ID:      job.ID,
                Payload: job.Payload,
                Timeout: c.config.TaskTimeout,
            }
            
            if err := c.pool.Submit(task); err != nil {
                log.Error("Failed to submit task", "error", err)
            }
        }
    }
}
```

---

#### 任務 2.2: 實作 resultLoop 與 handleResult (2 小時)
```go
// resultLoop 處理 Worker 執行結果
func (c *Controller) resultLoop() {
    for {
        select {
        case <-c.stopCh:
            log.Info("Result loop stopped")
            return
            
        default:
            result, err := c.pool.ReceiveResult()
            if err != nil {
                if err == worker.ErrPoolClosed {
                    return
                }
                log.Error("Failed to receive result", "error", err)
                time.Sleep(100 * time.Millisecond)
                continue
            }
            
            c.handleResult(result)
        }
    }
}

// handleResult 處理單個任務結果
func (c *Controller) handleResult(result worker.Result) {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    job := c.jobManager.GetJob(result.JobID)
    if job == nil {
        log.Warn("Unknown job", "jobID", result.JobID)
        return
    }
    
    if result.Success {
        // 成功：寫 WAL 並標記完成
        if err := c.wal.Append(wal.EventAck, *job, false); err != nil {
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
        // 失敗：增加重試次數
        job.Attempt++
        
        if job.Attempt >= c.config.MaxRetry {
            // 超過重試次數，進入死信
            if err := c.wal.Append(wal.EventDead, *job, false); err != nil {
                log.Error("Failed to append DEAD event", "error", err)
                return
            }
            
            if err := c.jobManager.MarkDead(result.JobID); err != nil {
                log.Error("Failed to mark dead", "error", err)
            }
            
            log.Warn("Job marked as dead", 
                "jobID", result.JobID, 
                "attempts", job.Attempt)
        } else {
            // 重新排隊
            if err := c.wal.Append(wal.EventRetry, *job, false); err != nil {
                log.Error("Failed to append RETRY event", "error", err)
                return
            }
            
            if err := c.jobManager.Requeue(result.JobID); err != nil {
                log.Error("Failed to requeue", "error", err)
            }
            
            log.Debug("Job requeued", 
                "jobID", result.JobID, 
                "attempt", job.Attempt)
        }
    }
}
```

---

#### 任務 2.3: 實作 timeoutLoop (1 小時)
```go
// timeoutLoop 檢測並處理超時任務
func (c *Controller) timeoutLoop() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-c.stopCh:
            log.Info("Timeout loop stopped")
            return
            
        case <-ticker.C:
            c.mu.Lock()
            
            // 取得所有過期任務
            expiredJobIDs := c.jobManager.GetExpiredJobs(time.Now())
            
            for _, jobID := range expiredJobIDs {
                job := c.jobManager.GetJob(jobID)
                if job == nil {
                    continue
                }
                
                // 寫 WAL
                if err := c.wal.Append(wal.EventTimeout, *job, false); err != nil {
                    log.Error("Failed to append TIMEOUT event", "error", err)
                    continue
                }
                
                // 增加重試次數
                job.Attempt++
                
                if job.Attempt >= c.config.MaxRetry {
                    // 超過重試次數，進入死信
                    if err := c.jobManager.MarkDead(jobID); err != nil {
                        log.Error("Failed to mark dead", "error", err)
                    }
                    log.Warn("Job timeout and marked as dead", 
                        "jobID", jobID)
                } else {
                    // 重新排隊
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
```

---

#### 任務 2.4: 實作 snapshotLoop (1 小時)
```go
// snapshotLoop 定期生成快照
func (c *Controller) snapshotLoop() {
    ticker := time.NewTicker(c.config.SnapshotInterval)
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

// takeSnapshot 執行快照操作
func (c *Controller) takeSnapshot() error {
    start := time.Now()
    
    // 取得當前狀態（不需要長時間持有鎖）
    c.mu.Lock()
    data := c.jobManager.Snapshot()
    data.LastSeq = c.wal.GetLastSeq()
    c.mu.Unlock()
    
    // 寫入快照
    if err := c.snapshot.Write(data); err != nil {
        return fmt.Errorf("failed to write snapshot: %w", err)
    }
    
    // 旋轉 WAL
    if err := c.wal.Rotate(); err != nil {
        return fmt.Errorf("failed to rotate WAL: %w", err)
    }
    
    log.Info("Snapshot taken", 
        "duration", time.Since(start),
        "jobs", len(data.Jobs))
    
    return nil
}
```

**Day 2 驗證**：
```bash
go test -v ./internal/controller/ -run TestLoops
```

---

### Day 3: 公開方法與整合測試

#### 任務 3.1: 實作 EnqueueJobs (30 分鐘)
```go
// EnqueueJobs 批次加入任務
func (c *Controller) EnqueueJobs(jobs []types.Job) error {
    c.mu.Lock()
    defer c.mu.Unlock()
    
    for _, job := range jobs {
        // 先寫 WAL
        if err := c.wal.Append(wal.EventEnqueue, job, false); err != nil {
            return fmt.Errorf("failed to append ENQUEUE event: %w", err)
        }
        
        // 加入 JobManager
        if err := c.jobManager.Enqueue(job); err != nil {
            return fmt.Errorf("failed to enqueue job: %w", err)
        }
    }
    
    return nil
}
```

---

#### 任務 3.2: 實作 GetStatus (30 分鐘)
```go
// GetStatus 取得系統狀態
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
```

---

#### 任務 3.3: 實作 Stop (30 分鐘)
```go
// Stop 優雅關閉 Controller
func (c *Controller) Stop() {
    log.Info("Stopping controller...")
    
    // 1. 發送停止訊號
    close(c.stopCh)
    
    // 2. 停止 Worker Pool（等待當前任務完成）
    c.pool.Stop()
    
    // 3. 最後一次快照
    if err := c.takeSnapshot(); err != nil {
        log.Error("Failed to take final snapshot", "error", err)
    }
    
    // 4. 關閉 WAL
    if err := c.wal.Close(); err != nil {
        log.Error("Failed to close WAL", "error", err)
    }
    
    log.Info("Controller stopped")
}
```

---

#### 任務 3.4: 整合測試 - 基本流程 (2 小時)
```go
// controller_test.go

func TestControllerBasicFlow(t *testing.T) {
    // 清理測試環境
    os.RemoveAll("/tmp/test-controller")
    os.MkdirAll("/tmp/test-controller", 0755)
    
    config := Config{
        WorkerCount:      4,
        TaskTimeout:      5 * time.Second,
        SnapshotInterval: 10 * time.Second,
        MaxRetry:         3,
        WALPath:          "/tmp/test-controller/wal.log",
        SnapshotPath:     "/tmp/test-controller/snapshot.json",
        WALBufferSize:    100,
    }
    
    // 建立並啟動 Controller
    ctrl, err := NewController(config)
    require.NoError(t, err)
    
    err = ctrl.Start()
    require.NoError(t, err)
    
    // 加入 10 個任務
    jobs := make([]types.Job, 10)
    for i := 0; i < 10; i++ {
        jobs[i] = types.Job{
            ID:      types.JobID(fmt.Sprintf("task-%03d", i)),
            Payload: map[string]interface{}{"index": i},
            Timeout: 5 * time.Second,
        }
    }
    
    err = ctrl.EnqueueJobs(jobs)
    require.NoError(t, err)
    
    // 等待所有任務完成
    time.Sleep(10 * time.Second)
    
    // 檢查狀態
    status := ctrl.GetStatus()
    completed := status["completed"].(int)
    assert.GreaterOrEqual(t, completed, 8) // 至少 80% 完成
    
    // 停止
    ctrl.Stop()
}
```

---

#### 任務 3.5: 整合測試 - 崩潰恢復 (3 小時)
```go
// integration_test.go

func TestCrashRecovery(t *testing.T) {
    // 清理測試環境
    testDir := "/tmp/test-crash-recovery"
    os.RemoveAll(testDir)
    os.MkdirAll(testDir, 0755)
    
    config := Config{
        WorkerCount:      4,
        TaskTimeout:      30 * time.Second,
        SnapshotInterval: 5 * time.Second,
        MaxRetry:         3,
        WALPath:          filepath.Join(testDir, "wal.log"),
        SnapshotPath:     filepath.Join(testDir, "snapshot.json"),
        WALBufferSize:    100,
    }
    
    // ========== 第一階段：啟動並處理部分任務 ==========
    ctrl1, err := NewController(config)
    require.NoError(t, err)
    
    err = ctrl1.Start()
    require.NoError(t, err)
    
    // 加入 100 個任務
    jobs := make([]types.Job, 100)
    for i := 0; i < 100; i++ {
        jobs[i] = types.Job{
            ID:      types.JobID(fmt.Sprintf("task-%03d", i)),
            Payload: map[string]interface{}{"index": i},
            Timeout: 30 * time.Second,
        }
    }
    
    err = ctrl1.EnqueueJobs(jobs)
    require.NoError(t, err)
    
    // 等待 50 個完成（約 3-5 秒）
    time.Sleep(5 * time.Second)
    
    status1 := ctrl1.GetStatus()
    completed1 := status1["completed"].(int)
    t.Logf("Phase 1: Completed %d jobs", completed1)
    
    // 模擬崩潰（強制停止，不呼叫 Stop）
    ctrl1 = nil
    
    // ========== 第二階段：恢復並繼續處理 ==========
    recoveryStart := time.Now()
    
    ctrl2, err := NewController(config)
    require.NoError(t, err)
    
    err = ctrl2.Start()
    require.NoError(t, err)
    
    recoveryDuration := time.Since(recoveryStart)
    t.Logf("Recovery took: %v", recoveryDuration)
    
    // 驗證恢復時間 < 3s
    assert.Less(t, recoveryDuration, 3*time.Second, 
        "Recovery should complete within 3 seconds")
    
    // 等待剩餘任務完成
    timeout := time.After(30 * time.Second)
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()
    
    for {
        select {
        case <-timeout:
            t.Fatal("Timeout waiting for jobs to complete")
            
        case <-ticker.C:
            status := ctrl2.GetStatus()
            completed := status["completed"].(int)
            pending := status["pending"].(int)
            inFlight := status["in_flight"].(int)
            
            t.Logf("Status: completed=%d, pending=%d, in_flight=%d", 
                completed, pending, inFlight)
            
            // 所有任務完成
            if completed >= 90 && pending == 0 && inFlight == 0 {
                t.Logf("All jobs completed: %d", completed)
                goto done
            }
        }
    }
    
done:
    // 優雅關閉
    ctrl2.Stop()
    
    // 最終驗證
    status2 := ctrl2.GetStatus()
    completed2 := status2["completed"].(int)
    assert.GreaterOrEqual(t, completed2, 90, 
        "At least 90% of jobs should be completed")
}
```

**Day 3 驗證**：
```bash
go test -v ./internal/controller/ -run TestCrashRecovery -timeout 60s
```

---

### Day 4: 優化與文檔

#### 任務 4.1: 添加日誌 (1 小時)
使用結構化日誌（推薦 `log/slog`）：
```go
import "log/slog"

var log = slog.Default()

// 在關鍵點添加日誌
log.Info("Job dispatched", "jobID", job.ID)
log.Warn("Job timeout", "jobID", jobID, "attempt", job.Attempt)
log.Error("Failed to write snapshot", "error", err)
```

---

#### 任務 4.2: 添加 Metrics (1 小時)
如果有 metrics 模組，添加統計：
```go
// 在 handleResult 中
if result.Success {
    metrics.JobCompleted.Inc()
    metrics.JobDuration.Observe(result.Duration.Seconds())
} else {
    metrics.JobFailed.Inc()
}
```

---

#### 任務 4.3: 性能優化 (2 小時)
1. **批次處理優化**：
```go
// 在 dispatchLoop 中批次取出多個任務
jobs := c.jobManager.PopPendingBatch(10)
```

2. **減少鎖競爭**：
```go
// 使用 RWMutex 讀寫分離
type Controller struct {
    mu sync.RWMutex  // 改用 RWMutex
    // ...
}

// 查詢操作使用讀鎖
func (c *Controller) GetStatus() map[string]interface{} {
    c.mu.RLock()
    defer c.mu.RUnlock()
    // ...
}
```

3. **WAL 批次寫入**：
已在 WAL 中實現，確保正確使用：
```go
// 非關鍵事件使用批次寫入
c.wal.Append(wal.EventDispatch, *job, false)

// 關鍵事件強制刷新
c.wal.Append(wal.EventAck, *job, true)
```

---

#### 任務 4.4: 文檔完善 (1 小時)
更新 `internal/controller/README.md`：
```markdown
# Controller 模組

## 職責
- 協調所有模組運作
- 實現任務調度與執行
- 處理崩潰恢復

## 架構
[架構圖]

## 使用範例
[程式碼範例]

## 效能指標
- 恢復時間：< 3s
- 吞吐量：≥ 200 jobs/s
```

---

## 🧪 測試策略

### 單元測試
```bash
# 測試單個方法
go test -v ./internal/controller/ -run TestLoadSnapshot
go test -v ./internal/controller/ -run TestReplayWAL
```

### 整合測試
```bash
# 測試完整流程
go test -v ./internal/controller/ -run TestControllerBasicFlow
go test -v ./internal/controller/ -run TestCrashRecovery
```

### 競態檢測
```bash
go test -race ./internal/controller/
```

### 基準測試
```bash
go test -bench=. ./internal/controller/
```

---

## 📊 驗收標準

### 功能完整性
- [x] 任務可以 Enqueue
- [x] 任務被正確調度
- [x] 失敗任務重試
- [x] 超時任務處理
- [x] 超過重試次數進入死信
- [x] 定期生成快照
- [x] 崩潰後正確恢復

### 效能指標
- [x] 恢復時間 < 3s
- [x] 吞吐量 ≥ 200 jobs/s

### 程式碼品質
- [x] 所有測試通過
- [x] 無競態條件
- [x] 日誌完善
- [x] 文檔清晰

---

## 🔧 常見問題

### Q1: 如何處理死鎖？
**A**: 使用以下原則：
- 最小化鎖的範圍
- 避免嵌套鎖
- 使用 `defer` 確保解鎖
- 不在持有鎖時呼叫可能阻塞的操作

### Q2: 如何確保冪等性？
**A**: 在重放 WAL 時檢查任務狀態：
```go
if jobManager.IsCompleted(jobID) {
    return nil  // 跳過已完成的任務
}
```

### Q3: 如何優化恢復時間？
**A**:
- 減少快照大小（只儲存必要資料）
- 使用壓縮（gzip）
- 並行化恢復流程

---

## 📝 下一步

完成 Controller 後，進入 **Step 7: 整合測試**：
- 端到端測試
- 壓力測試
- 長時間運行測試

---

**預估完成時間**：3-4 天
**目前狀態**：⏳ 待開始
