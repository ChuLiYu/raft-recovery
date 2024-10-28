# Beaver-Raft Phase 1: Implementation Order# Beaver-Raft Phase 1 實作順序



**English** | **[中文版](IMPLEMENTATION_ORDER.zh-CN.md)**本文件提供明確的實作步驟，每個步驟都包含目標、檔案、驗證方式。



> Step-by-step module implementation guide (3 weeks, 11 steps)---



## Overview## 📊 整體進度追蹤



This document outlines the implementation order for Phase 1 modules, designed to minimize dependencies and enable incremental testing.```text

第一週：基礎層（資料結構 + 持久化）

## Timeline  ├─ Step 1: 資料結構定義        [1 天]

  ├─ Step 2: 佇列狀態管理        [2 天]

**Total Duration**: 3 weeks    ├─ Step 3: WAL 實作            [2-3 天]

**Approach**: Bottom-up (foundations first)    └─ Step 4: Snapshot 管理       [1-2 天]

**Testing**: Test each step before proceeding

第二週：執行層（Worker + Controller）

## Implementation Steps  ├─ Step 5: Worker Pool         [2-3 天]

  ├─ Step 6: Controller 核心     [3-4 天]

### Step 1: Project Setup & Types (Day 1)  └─ Step 7: 整合測試            [1-2 天]



**Duration**: 1 day  第三週：介面層（CLI + Demo）

**Priority**: ⭐⭐⭐ (Critical)  ├─ Step 8: Metrics 監控        [1 天]

  ├─ Step 9: CLI 介面            [2 天]

**Files**:  ├─ Step 10: Demo & 文件        [2 天]

- `pkg/types/types.go` - Core type definitions  └─ Step 11: 效能調校           [2 天]

- `go.mod` - Dependencies```

- `Makefile` - Build automation

---

**Key Types**:

```go## 🎯 Step 1: 資料結構定義（1 天）

type Job struct {

    ID        string### Step 1 - 目標

    Payload   map[string]interface{}

    Attempt   int建立所有模組共用的基礎資料結構。

    Status    JobStatus

    CreatedAt time.Time### Step 1 - 檔案

}

- `internal/types/types.go`（新建）

type Config struct {

    WorkerCount      int### Step 1 - 實作內容

    TaskTimeout      time.Duration

    SnapshotInterval time.Duration```go

    WALPath          stringpackage types

    SnapshotPath     string

}import "time"

```

// JobStatus 任務狀態

**Tests**: Type serialization, config loadingtype JobStatus string



---const (

    StatusPending   JobStatus = "pending"

### Step 2: JobManager (Days 2-3)    StatusInFlight  JobStatus = "in_flight"

    StatusCompleted JobStatus = "completed"

**Duration**: 2 days      StatusDead      JobStatus = "dead"

**Priority**: ⭐⭐⭐ (Critical))



**File**: `internal/jobmanager/job_manager.go`// Job 任務結構

type Job struct {

**Core Functions**:    ID        string                 `json:"id"`

- `Enqueue(job)` - Add job to pending queue    Payload   map[string]interface{} `json:"payload"`

- `Dequeue()` - Get next pending job    Attempt   int                    `json:"attempt"`

- `MarkInFlight(jobID, workerID)` - Update state    Status    JobStatus              `json:"status"`

- `MarkCompleted(jobID)` - Job succeeded    CreatedAt time.Time              `json:"created_at"`

- `MarkFailed(jobID)` - Job failed}

- `GetTimeouts()` - Find timed-out jobs

// InFlightInfo 執行中任務資訊

**State Machine**:type InFlightInfo struct {

```text    WorkerID   int   `json:"worker_id"`

PENDING → IN_FLIGHT → COMPLETED    DeadlineMs int64 `json:"deadline_ms"`

              ↓}

            FAILED

```// Config 系統配置

type Config struct {

**Tests**: State transitions, concurrency, invariants    WorkerCount      int           `yaml:"worker_count"`

    TaskTimeout      time.Duration `yaml:"task_timeout"`

---    SnapshotInterval time.Duration `yaml:"snapshot_interval"`

    MaxRetry         int           `yaml:"max_retry"`

### Step 3: WAL Implementation (Days 4-6)    WALPath          string        `yaml:"wal_path"`

    SnapshotPath     string        `yaml:"snapshot_path"`

**Duration**: 2-3 days      MetricsPort      int           `yaml:"metrics_port"`

**Priority**: ⭐⭐⭐⭐ (Critical + Complex)}

```

**Files**:

- `internal/storage/wal/types.go`### Step 1 - 驗證

- `internal/storage/wal/wal.go`

- `internal/storage/wal/checksum.go````bash

go build ./internal/types/

**Core Functions**:```

- `NewWAL(path)` - Initialize WAL

- `Append(event)` - Write operation log**完成標準**：編譯通過，無錯誤。

- `Replay(handler)` - Replay events

- `Rotate()` - Log rotation---

- `Close()` - Clean shutdown

## 🎯 Step 2: 佇列狀態管理（2 天）

**Event Structure**:

```go### Step 2 - 目標

type Event struct {

    Seq       uint64實作 JobManager，管理 queue、in_flight、completed 三個集合。

    Type      string  // DISPATCH, ACK, RETRY

    JobID     string### Step 2 - 檔案

    Timestamp int64

    Checksum  uint32- `internal/jobmanager/job_manager.go`（已存在，需完成實作）

}- `internal/jobmanager/job_manager_test.go`（新建）

```

### Step 2 - 實作內容（按順序）

**Tests**: Append, replay, corruption recovery, rotation

#### 2.1 基礎結構

---

```go

### Step 4: Worker Pool (Days 7-9)package jobmanager



**Duration**: 2-3 days  import (

**Priority**: ⭐⭐⭐ (Critical)    "sync"

    "time"

**Files**:    "github.com/ChuLiYu/beaver-raft/internal/types"

- `internal/worker/worker.go`)

- `internal/worker/worker_pool.go`

type State struct {

**Architecture**:    mu        sync.RWMutex

```text    queue     []types.Job

Pool → Worker1 (goroutine)    inFlight  map[string]types.InFlightInfo

    → Worker2 (goroutine)    completed map[string]bool

    → Worker3 (goroutine)    dead      map[string]types.Job

    ...}

    → WorkerN (goroutine)

```func NewJobManager() *JobManager {

    return &JobManager{

**Core Functions**:        queue:     make([]types.Job, 0),

- `NewPool(size)` - Create worker pool        inFlight:  make(map[string]types.InFlightInfo),

- `Start(workerCount)` - Launch workers        completed: make(map[string]bool),

- `Submit(task)` - Distribute task        dead:      make(map[string]types.Job),

- `GetResult()` - Collect results    }

- `Stop()` - Graceful shutdown}

```

**Tests**: Concurrency, timeout handling, graceful shutdown

#### 2.2 基本操作（先實作這些）

---

1. `Enqueue(job Job) error`

### Step 5: Snapshot Manager (Days 10-11)2. `PopPending() *Job`

3. `MarkInFlight(jobID, deadline)`

**Duration**: 1-2 days  4. `MarkCompleted(jobID)`

**Priority**: ⭐⭐⭐ (Critical)

#### 2.3 進階操作

**File**: `internal/snapshot/snapshot_manager.go`

1. `Requeue(job Job)`

**Core Functions**:2. `MarkDead(jobID)`

- `SaveSnapshot(state)` - Persist full state3. `GetExpiredJobs(now) []string`

- `LoadSnapshot()` - Load latest snapshot4. `GetJob(jobID) *Job`

- `ScheduleSnapshots(interval)` - Periodic snapshots

#### 2.4 持久化支援

**Snapshot Format**:

```json1. `Snapshot() SnapshotData`

{2. `Restore(data SnapshotData)`

  "jobs": {...},3. `Validate() error`（驗證不變性）

  "schema_ver": 1,4. `Stats() map[string]int`

  "last_seq": 12345

}### 測試（寫測試 → 實作 → 通過）

```

```bash

**Tests**: Save/load, recovery, concurrent access# 建立測試檔

touch internal/jobmanager/job_manager_test.go

---```



### Step 6: Controller (Days 12-14)```go

// job_manager_test.go

**Duration**: 3 days  func TestEnqueueDequeue(t *testing.T) {

**Priority**: ⭐⭐⭐⭐ (Most Complex)    jobManager := jobmanager.NewJobManager()



**File**: `internal/controller/controller.go`    // 加入 10 個任務

    for i := 0; i < 10; i++ {

**Four Main Loops**:        job := types.Job{ID: fmt.Sprintf("task-%d", i)}

1. **Dispatch Loop**: Dequeue → WAL log → Send to workers        jobManager.Enqueue(job)

2. **Result Loop**: Collect results → WAL log → Update state    }

3. **Timeout Loop**: Check timeouts → Retry or fail

4. **Snapshot Loop**: Periodic state snapshots    // 彈出驗證 FIFO

    for i := 0; i < 10; i++ {

**Tests**: Integration tests, recovery scenarios        job := jobManager.PopPending()

        assert.Equal(t, fmt.Sprintf("task-%d", i), job.ID)

---    }



### Step 7: Metrics (Day 15)    // 空佇列

    assert.Nil(t, jobManager.PopPending())

**Duration**: 1 day  }

**Priority**: ⭐⭐ (Important)

func TestJobManagerTransitions(t *testing.T) { /* ... */ }

**File**: `internal/metrics/metrics.go`func TestInvariant(t *testing.T) { /* ... */ }

func TestConcurrency(t *testing.T) { /* ... */ }

**Metrics**:```

- `jobs_enqueued_total`

- `jobs_completed_total`### Step 2 - 驗證

- `jobs_failed_total`

- `jobs_in_flight````bash

- `recovery_time_seconds`go test -v ./internal/jobmanager/

go test -race ./internal/jobmanager/

**Tests**: Metric collection, Prometheus endpoint```



---**完成標準**：



### Step 8: CLI Interface (Days 16-17)- 所有測試通過

- `go test -race` 無警告

**Duration**: 1-2 days  - Validate() 能檢測出不變性違反

**Priority**: ⭐⭐ (Important)

---

**File**: `internal/cli/cli.go`

## 🎯 Step 3: WAL 實作（2-3 天）

**Commands**:

- `run` - Start server### Step 3 - 目標

- `enqueue` - Submit jobs

- `status` - Check status實作 Write-Ahead Log，支援追加、重放、校驗。



**Tests**: Command parsing, integration### Step 3 - 檔案（看起來您已開始）



---- `internal/storage/wal/types.go` ✅（已存在）

- `internal/storage/wal/checksum.go` ✅（已存在）

### Step 9: Integration Tests (Day 18)- `internal/storage/wal/wal.go` ✅（已存在，需完善）

- `internal/storage/wal/wal_test.go`（新建）

**Duration**: 1 day  

**Priority**: ⭐⭐⭐ (Critical)### Step 3 - 實作順序



**File**: `test/integration/`#### 3.1 Event 結構（types.go）



**Test Scenarios**:```go

- End-to-end job processingtype Event struct {

- Crash recovery    Seq       uint64    `json:"seq"`

- High load    Type      string    `json:"type"` // DISPATCH, ACK, RETRY, etc.

- Race conditions    JobID     jobmanager.JobID    `json:"job_id"`

    Timestamp int64     `json:"timestamp"`

---    Checksum  uint32    `json:"checksum"`

}

### Step 10: Documentation (Day 19)```



**Duration**: 1 day  #### 3.2 WAL 主體（wal.go）

**Priority**: ⭐⭐ (Important)

1. `NewWAL(path) (*WAL, error)`

**Files**:2. `Append(eventType, jobID) error`

- README.md3. `Replay(handler func(Event) error) error`

- USAGE_GUIDE.md4. `Rotate() error`

- Architecture docs5. `Close() error`



---#### 3.3 校驗和（checksum.go）



### Step 11: Demo & Polish (Days 20-21)```go

func CalculateChecksum(event Event) uint32 {

**Duration**: 1-2 days      data := event.Type + event.JobID + strconv.FormatUint(event.Seq, 10)

**Priority**: ⭐ (Nice-to-have)    return crc32.ChecksumIEEE([]byte(data))

}

**Tasks**:

- `make demo` scriptfunc VerifyChecksum(event Event) bool {

- Performance tuning    expected := CalculateChecksum(event)

- Bug fixes    return event.Checksum == expected

- Final testing}

```

---

### Step 3 - 測試重點

## Dependency Graph

```go

```textfunc TestAppendAndReplay(t *testing.T) { /* ... */ }

Step 1 (Types)func TestChecksum(t *testing.T) {

    ↓    // 手動破壞 WAL 檔案，驗證能偵測

Step 2 (JobManager)}

    ↓func TestRotate(t *testing.T) { /* ... */ }

Step 3 (WAL) ←──┐func TestConcurrentAppend(t *testing.T) { /* ... */ }

    ↓           │```

Step 4 (Workers)│

    ↓           │### Step 3 - 驗證

Step 5 (Snapshot)

    ↓           │```bash

Step 6 (Controller) ─→ All componentsgo test -v ./internal/storage/wal/

    ↓go test -race ./internal/storage/wal/

Step 7-11 (Polish)

```# 手動驗證

cat /tmp/test-wal.log | jq .

## Testing Strategy```



| Step | Unit Tests | Integration Tests | Coverage Target |**完成標準**：

|------|-----------|-------------------|-----------------|

| 1-5  | ✅ Each module | ❌ | 80%+ |- 所有測試通過

| 6    | ✅ | ✅ | 85%+ |- 校驗和驗證有效

| 7-8  | ✅ | ❌ | 75%+ |- Replay 正確重放所有事件

| 9    | ❌ | ✅ Full system | N/A |

---

## Success Criteria

## 🎯 Step 4: Snapshot 管理（1-2 天）

Each step must pass before proceeding:

### Step 4 - 目標

- ✅ All unit tests pass

- ✅ No race conditions (`go test -race`)實作快照序列化，使用原子性寫入。

- ✅ Code review completed

- ✅ Documentation updated### Step 4 - 檔案



## Common Pitfalls- `internal/snapshot/snapshot.go`（已有偽代碼）

- `internal/snapshot/snapshot_test.go`（新建）

1. **Step 3 (WAL)**: Ensure fsync for durability

2. **Step 4 (Workers)**: Avoid goroutine leaks### Step 4 - 實作內容

3. **Step 6 (Controller)**: Race conditions in state updates

4. **All Steps**: Proper error handling#### 4.1 SnapshotData 結構



## Daily Checklist```go

type SnapshotData struct {

- [ ] Write tests first (TDD)    Queue       []types.Job                   `json:"queue"`

- [ ] Run `go test -race`    InFlight    map[string]types.InFlightInfo `json:"in_flight"`

- [ ] Update documentation    Completed   []string                      `json:"completed"`

- [ ] Commit with clear messages    Dead        []string                      `json:"dead"`

- [ ] Review before proceeding    LastSeq     uint64                        `json:"last_seq"`

    SchemaVer   int                           `json:"schema_version"`

## Tools & Commands    Timestamp   int64                         `json:"timestamp"`

}

```bash```

# Run tests

go test ./...#### 4.2 Manager 實作



# Race detection1. `NewManager(path) *Manager`

go test -race ./...2. `Write(data SnapshotData) error` - 使用 temp + rename

3. `Load() (SnapshotData, error)`

# Coverage4. `Exists() bool`

go test -cover ./...

### 關鍵：原子性寫入

# Benchmarks

go test -bench=. ./...```go

func (m *Manager) Write(data SnapshotData) error {

# Build    m.mu.Lock()

make build    defer m.mu.Unlock()



# Clean    data.SchemaVer = 1

make clean    data.Timestamp = time.Now().Unix()

```

    jsonData, _ := json.MarshalIndent(data, "", "  ")

## References

    tmpPath := m.path + ".tmp"

- [QUICKSTART.md](QUICKSTART.md) - Development guide    os.WriteFile(tmpPath, jsonData, 0644)

- [PHASE1_SUMMARY.md](PHASE1_SUMMARY.md) - Feature summary    os.Rename(tmpPath, m.path)  // 原子操作

- [docs/phase1-architecture.md](docs/phase1-architecture.md) - Detailed design

    return nil

---}

```

**Note**: For detailed Chinese explanations of each step, see [IMPLEMENTATION_ORDER.zh-CN.md](IMPLEMENTATION_ORDER.zh-CN.md)

### Step 4 - 測試重點

**Status**: ✅ All 11 steps completed successfully

```go
func TestWriteAndLoad(t *testing.T) { /* ... */ }
func TestAtomicWrite(t *testing.T) {
    // 模擬寫入中斷，驗證舊快照不損壞
}
func TestVersionMismatch(t *testing.T) { /* ... */ }
```

### Step 4 - 驗證

```bash
go test -v ./internal/snapshot/
cat /tmp/test-snapshot.json | jq .
```

**完成標準**：

- 原子性測試通過
- 版本驗證有效

---

## 🎯 Step 5: Worker Pool（2-3 天）

### Step 5 - 目標

實作 Worker 並發執行任務。

### Step 5 - 檔案

- `internal/worker/worker.go`（新建）
- `internal/worker/pool.go`（已有偽代碼，需完成）
- `internal/worker/worker_test.go`（新建）

### Step 5 - 實作內容

#### 5.1 Task & Result 結構

```go
type Task struct {
    ID      string
    Payload map[string]interface{}
    Timeout time.Duration
}

type Result struct {
    JobID    jobmanager.JobID
    Success  bool
    Error    error
    Duration time.Duration
}
```

#### 5.2 Worker（worker.go）

```go
type Worker struct {
    id       int
    taskCh   <-chan Task
    resultCh chan<- Result
}

func (w *Worker) Run() {
    for task := range w.taskCh {
        start := time.Now()

        ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)
        err := w.execute(ctx, task.Payload)
        cancel()

        w.resultCh <- Result{
            JobID:    task.ID,
            Success:  err == nil,
            Error:    err,
            Duration: time.Since(start),
        }
    }
}

func (w *Worker) execute(ctx context.Context, payload map[string]interface{}) error {
    // 模擬工作
    workDuration := time.Duration(rand.Intn(500)) * time.Millisecond

    select {
    case <-ctx.Done():
        return ctx.Err()
    case <-time.After(workDuration):
        if rand.Intn(100) < 10 {
            return errors.New("模擬失敗")
        }
        return nil
    }
}
```

#### 5.3 Pool（pool.go）

1. `NewPool(bufferSize) *Pool`
2. `Start(workerCount)`
3. `Submit(task Task)`
4. `ReceiveResult() Result`
5. `Stop()`

### Step 5 - 測試重點

```go
func TestWorkerExecution(t *testing.T) { /* ... */ }
func TestTimeout(t *testing.T) { /* ... */ }
func TestGracefulShutdown(t *testing.T) { /* ... */ }
```

### Step 5 - 驗證

```bash
go test -v ./internal/worker/
go test -race ./internal/worker/
```

**完成標準**：

- 超時機制正常
- 優雅關閉無 goroutine 洩漏

---

## 🎯 Step 6: Controller 核心（3-4 天）

### Step 6 - 目標

整合所有模組，實作四個循環。

### Step 6 - 檔案

- `internal/controller/controller.go`（已有偽代碼）
- `internal/controller/controller_test.go`（新建）

### Step 6 - 實作順序

#### 6.1 結構與建構（Day 1）

```go
type Controller struct {
    mu       sync.Mutex
    state    *state.State
    wal      *wal.WAL
    snapshot *snapshot.Manager
    pool     *worker.Pool
    config   types.Config
    stopCh   chan struct{}
}

func NewController(config types.Config) (*Controller, error) {
    // 初始化所有模組
}
```

#### 6.2 恢復流程（Day 1-2）

1. `Start() error`
2. `loadSnapshot() error`
3. `replayWAL() error`（重點：冪等性）

```go
func (c *Controller) replayWAL() error {
    handler := func(event wal.Event) error {
        c.mu.Lock()
        defer c.mu.Unlock()

        switch event.Type {
        case "DISPATCH":
            if c.state.IsCompleted(event.JobID) {
                return nil  // 冪等性檢查
            }
            c.state.MarkInFlight(event.JobID, ...)
        case "ACK":
            if !c.state.IsCompleted(event.JobID) {
                c.state.MarkCompleted(event.JobID)
            }
        // ... 其他事件
        }
        return nil
    }

    return c.wal.Replay(handler)
}
```

#### 6.3 四個循環（Day 2-3）

1. `dispatchLoop()` - 調度任務
2. `resultLoop()` + `handleResult()` - 處理結果
3. `timeoutLoop()` - 超時檢查
4. `snapshotLoop()` - 定時快照

#### 6.4 公開方法（Day 3）

1. `EnqueueJobs(jobs []Job) error`
2. `GetStatus() map[string]interface{}`
3. `Stop()`

### Step 6 - 測試重點（關鍵！）

```go
func TestCrashRecovery(t *testing.T) {
    // 1. 啟動，加入 100 個任務
    // 2. 等待 50 個完成
    // 3. Stop()
    // 4. 重新 Start()
    // 5. 驗證恢復時間 < 3s
    // 6. 驗證剩餘任務完成
}

func TestIdempotency(t *testing.T) {
    // 重放 WAL 兩次，驗證結果相同
}
```

### Step 6 - 驗證

```bash
go test -v ./internal/controller/
go test -race ./internal/controller/
```

**完成標準**：

- 崩潰恢復測試通過
- 恢復時間 < 3s
- 無競爭條件

---

## 🎯 Step 7: 整合測試（1-2 天）

### Step 7 - 目標

端到端測試整個系統。

### Step 7 - 檔案

- `test/integration/recovery_test.go`（新建）
- `test/integration/throughput_test.go`（新建）

### Step 7 - 測試場景

#### 7.1 崩潰恢復測試

```go
func TestEndToEndRecovery(t *testing.T) {
    // 完整流程測試
}
```

#### 7.2 吞吐量測試

```go
func BenchmarkThroughput(b *testing.B) {
    // 目標：≥ 200 jobs/s
}
```

### Step 7 - 驗證

```bash
go test -v ./test/integration/
go test -bench=. ./test/integration/
```

**完成標準**：

- 恢復時間 < 3s
- 吞吐量 ≥ 200 jobs/s

---

## 🎯 Step 8: Metrics 監控（1 天）

### Step 8 - 目標

暴露 Prometheus 指標。

### Step 8 - 檔案

- `internal/metrics/metrics.go`（新建）

### Step 8 - 實作內容

```go
type Collector struct {
    jobsDispatched prometheus.Counter
    jobsCompleted  prometheus.Counter
    jobLatency     prometheus.Histogram
    recoveryTime   prometheus.Gauge
}

func NewCollector() *Collector {
    // 建立並註冊所有指標
}

func StartServer(port int) {
    http.Handle("/metrics", promhttp.Handler())
    http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
}
```

### Step 8 - 驗證

```bash
curl http://localhost:9090/metrics | grep queue_
```

---

## 🎯 Step 9: CLI 介面（2 天）

### Step 9 - 目標

實作命令列介面。

### Step 9 - 檔案

- `internal/cli/cli.go`（已有偽代碼）
- `cmd/queue/main.go`（已有偽代碼）

### Step 9 - 實作順序

#### 9.1 CLI 框架（Day 1）

1. `buildEnqueueCmd()` - 加入任務
2. `buildRunCmd()` - 啟動系統
3. `buildStatusCmd()` - 查看狀態

#### 9.2 配置管理（Day 1）

1. `loadConfig()` - YAML + 環境變數 + 旗標

#### 9.3 Main 入口（Day 2）

1. `cmd/queue/main.go` - 呼叫 CLI

### Step 9 - 驗證

```bash
go build -o bin/queue cmd/queue/main.go

./bin/queue --help
./bin/queue run --workers 8
./bin/queue status
```

**完成標準**：

- 所有命令正常運作
- Ctrl+C 優雅關閉

---

## 🎯 Step 10: Demo & 文件（2 天）

### Step 10 - 目標

建立示範腳本與更新文件。

### Step 10 - 檔案

- `scripts/demo.sh`（新建）
- `Makefile`（新建）
- `README.md`（更新）
- `configs/default.yaml`（新建）

### 10.1 Demo 腳本

```bash
#!/bin/bash
echo "=== Phase 1 Demo ==="

# 1. 清理
rm -rf data/
mkdir -p data/

# 2. 產生測試任務
cat > /tmp/jobs.json <<EOF
[
  {"id": "task-001", "payload": {"value": 42}},
  ...
]
EOF

# 3. 啟動
./bin/queue run --workers 8 &
PID=$!

# 4. 加入任務
./bin/queue enqueue --file /tmp/jobs.json

# 5. 模擬崩潰
sleep 3
kill -9 $PID

# 6. 恢復
./bin/queue run &
sleep 2

# 7. 查看狀態
./bin/queue status
```

### 10.2 Makefile

```makefile
build:
    go build -o bin/queue cmd/queue/main.go

test:
    go test ./...
    go test -race ./...

demo:
    ./scripts/demo.sh

clean:
    rm -rf bin/ data/
```

### 10.3 README 更新

- 加入架構圖（Mermaid）
- 快速開始指南
- 效能指標

### Step 10 - 驗證

```bash
make demo
```

---

## 🎯 Step 11: 效能調校（2 天）

### Step 11 - 目標

優化至 KPI 目標。

### Step 11 - 調校重點

#### 11.1 恢復時間優化

- 測量 loadSnapshot 時間
- 測量 replayWAL 時間
- 目標：< 3s

#### 11.2 吞吐量優化

- WAL 批次寫入
- 使用 RWMutex
- 目標：≥ 200 jobs/s

#### 11.3 最終驗證

```bash
go test -bench=. ./test/integration/
go test -race ./...
```

**完成標準**：

- 恢復時間 < 3s
- 吞吐量 ≥ 200 jobs/s
- 通過所有測試

---

## ✅ 完成檢查清單

### 核心功能

- [ ] 任務可以 Enqueue
- [ ] Worker 並發執行
- [ ] 失敗任務重試
- [ ] 超時任務重新排隊
- [ ] 超過重試次數進入死信

### 持久化

- [ ] WAL 記錄所有事件
- [ ] 校驗和驗證有效
- [ ] Snapshot 原子性寫入
- [ ] 恢復流程正確

### 效能

- [ ] 恢復時間 < 3s
- [ ] 吞吐量 ≥ 200 jobs/s
- [ ] 通過 race detector

### 使用性

- [ ] CLI 命令正常
- [ ] Demo 腳本可執行
- [ ] 文件完整

---

## 📅 時間規劃建議

**全職開發**（每天 8 小時）：

- Week 1: Step 1-4（基礎層）
- Week 2: Step 5-7（執行層）
- Week 3: Step 8-11（完善）

**兼職開發**（每天 2-3 小時）：

- Week 1-2: Step 1-4
- Week 3-4: Step 5-7
- Week 5-6: Step 8-11

---

## 🚀 立即開始

### 您目前的進度

看起來您已經：

- ✅ 建立了 WAL 相關檔案（types.go, checksum.go, wal.go）
- ⏳ 正在修改 job_manager.go

### 建議下一步

1. **完成 Step 1**（types.go）- 30 分鐘
2. **完成 Step 2**（job_manager.go）- 今天內
3. **驗證 Step 3**（WAL）- 明天

### 今天的具體任務

```bash
# 1. 建立 types.go
touch internal/types/types.go
# → 複製上面 Step 1 的程式碼

# 2. 修正 job_manager.go 的語法錯誤
# synce.RWMutex → sync.RWMutex
# queue [] → queue []types.Job

# 3. 實作 job_manager.go 的基本方法
# → Enqueue, PopPending

# 4. 寫第一個測試
touch internal/jobmanager/job_manager_test.go
# → TestEnqueueDequeue

# 5. 跑測試
go test -v ./internal/jobmanager/
```

開始吧！🎯
