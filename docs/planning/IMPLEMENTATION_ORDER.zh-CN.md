# Beaver-Raft Phase 1 實作順序

本文件提供明確的實作步驟，每個步驟都包含目標、檔案、驗證方式。

---

## 📊 整體進度追蹤

```text
第一週：基礎層（資料結構 + 持久化）
  ├─ Step 1: 資料結構定義        [1 天]
  ├─ Step 2: 佇列狀態管理        [2 天]
  ├─ Step 3: WAL 實作            [2-3 天]
  └─ Step 4: Snapshot 管理       [1-2 天]

第二週：執行層（Worker + Controller）
  ├─ Step 5: Worker Pool         [2-3 天]
  ├─ Step 6: Controller 核心     [3-4 天]
  └─ Step 7: 整合測試            [1-2 天]

第三週：介面層（CLI + Demo）
  ├─ Step 8: Metrics 監控        [1 天]
  ├─ Step 9: CLI 介面            [2 天]
  ├─ Step 10: Demo & 文件        [2 天]
  └─ Step 11: 效能調校           [2 天]
```

---

## 🎯 Step 1: 資料結構定義（1 天）

### Step 1 - 目標

建立所有模組共用的基礎資料結構。

### Step 1 - 檔案

- `internal/types/types.go`（新建）

### Step 1 - 實作內容

```go
package types

import "time"

// JobStatus 任務狀態
type JobStatus string

const (
    StatusPending   JobStatus = "pending"
    StatusInFlight  JobStatus = "in_flight"
    StatusCompleted JobStatus = "completed"
    StatusDead      JobStatus = "dead"
)

// Job 任務結構
type Job struct {
    ID        string                 `json:"id"`
    Payload   map[string]interface{} `json:"payload"`
    Attempt   int                    `json:"attempt"`
    Status    JobStatus              `json:"status"`
    CreatedAt time.Time              `json:"created_at"`
}

// InFlightInfo 執行中任務資訊
type InFlightInfo struct {
    WorkerID   int   `json:"worker_id"`
    DeadlineMs int64 `json:"deadline_ms"`
}

// Config 系統配置
type Config struct {
    WorkerCount      int           `yaml:"worker_count"`
    TaskTimeout      time.Duration `yaml:"task_timeout"`
    SnapshotInterval time.Duration `yaml:"snapshot_interval"`
    MaxRetry         int           `yaml:"max_retry"`
    WALPath          string        `yaml:"wal_path"`
    SnapshotPath     string        `yaml:"snapshot_path"`
    MetricsPort      int           `yaml:"metrics_port"`
}
```

### Step 1 - 驗證

```bash
go build ./internal/types/
```

**完成標準**：編譯通過，無錯誤。

---

## 🎯 Step 2: 佇列狀態管理（2 天）

### Step 2 - 目標

實作 JobManager，管理 queue、in_flight、completed 三個集合。

### Step 2 - 檔案

- `internal/jobmanager/job_manager.go`（已存在，需完成實作）
- `internal/jobmanager/job_manager_test.go`（新建）

### Step 2 - 實作內容（按順序）

#### 2.1 基礎結構

```go
package jobmanager

import (
    "sync"
    "time"
    "github.com/ChuLiYu/beaver-raft/internal/types"
)

type State struct {
    mu        sync.RWMutex
    queue     []types.Job
    inFlight  map[string]types.InFlightInfo
    completed map[string]bool
    dead      map[string]types.Job
}

func NewJobManager() *JobManager {
    return &JobManager{
        queue:     make([]types.Job, 0),
        inFlight:  make(map[string]types.InFlightInfo),
        completed: make(map[string]bool),
        dead:      make(map[string]types.Job),
    }
}
```

#### 2.2 基本操作（先實作這些）

1. `Enqueue(job Job) error`
2. `PopPending() *Job`
3. `MarkInFlight(jobID, deadline)`
4. `MarkCompleted(jobID)`

#### 2.3 進階操作

1. `Requeue(job Job)`
2. `MarkDead(jobID)`
3. `GetExpiredJobs(now) []string`
4. `GetJob(jobID) *Job`

#### 2.4 持久化支援

1. `Snapshot() SnapshotData`
2. `Restore(data SnapshotData)`
3. `Validate() error`（驗證不變性）
4. `Stats() map[string]int`

### 測試（寫測試 → 實作 → 通過）

```bash
# 建立測試檔
touch internal/jobmanager/job_manager_test.go
```

```go
// job_manager_test.go
func TestEnqueueDequeue(t *testing.T) {
    jobManager := jobmanager.NewJobManager()

    // 加入 10 個任務
    for i := 0; i < 10; i++ {
        job := types.Job{ID: fmt.Sprintf("task-%d", i)}
        jobManager.Enqueue(job)
    }

    // 彈出驗證 FIFO
    for i := 0; i < 10; i++ {
        job := jobManager.PopPending()
        assert.Equal(t, fmt.Sprintf("task-%d", i), job.ID)
    }

    // 空佇列
    assert.Nil(t, jobManager.PopPending())
}

func TestJobManagerTransitions(t *testing.T) { /* ... */ }
func TestInvariant(t *testing.T) { /* ... */ }
func TestConcurrency(t *testing.T) { /* ... */ }
```

### Step 2 - 驗證

```bash
go test -v ./internal/jobmanager/
go test -race ./internal/jobmanager/
```

**完成標準**：

- 所有測試通過
- `go test -race` 無警告
- Validate() 能檢測出不變性違反

---

## 🎯 Step 3: WAL 實作（2-3 天）

### Step 3 - 目標

實作 Write-Ahead Log，支援追加、重放、校驗。

### Step 3 - 檔案（看起來您已開始）

- `internal/storage/wal/types.go` ✅（已存在）
- `internal/storage/wal/checksum.go` ✅（已存在）
- `internal/storage/wal/wal.go` ✅（已存在，需完善）
- `internal/storage/wal/wal_test.go`（新建）

### Step 3 - 實作順序

#### 3.1 Event 結構（types.go）

```go
type Event struct {
    Seq       uint64    `json:"seq"`
    Type      string    `json:"type"` // DISPATCH, ACK, RETRY, etc.
    JobID     jobmanager.JobID    `json:"job_id"`
    Timestamp int64     `json:"timestamp"`
    Checksum  uint32    `json:"checksum"`
}
```

#### 3.2 WAL 主體（wal.go）

1. `NewWAL(path) (*WAL, error)`
2. `Append(eventType, jobID) error`
3. `Replay(handler func(Event) error) error`
4. `Rotate() error`
5. `Close() error`

#### 3.3 校驗和（checksum.go）

```go
func CalculateChecksum(event Event) uint32 {
    data := event.Type + event.JobID + strconv.FormatUint(event.Seq, 10)
    return crc32.ChecksumIEEE([]byte(data))
}

func VerifyChecksum(event Event) bool {
    expected := CalculateChecksum(event)
    return event.Checksum == expected
}
```

### Step 3 - 測試重點

```go
func TestAppendAndReplay(t *testing.T) { /* ... */ }
func TestChecksum(t *testing.T) {
    // 手動破壞 WAL 檔案，驗證能偵測
}
func TestRotate(t *testing.T) { /* ... */ }
func TestConcurrentAppend(t *testing.T) { /* ... */ }
```

### Step 3 - 驗證

```bash
go test -v ./internal/storage/wal/
go test -race ./internal/storage/wal/

# 手動驗證
cat /tmp/test-wal.log | jq .
```

**完成標準**：

- 所有測試通過
- 校驗和驗證有效
- Replay 正確重放所有事件

---

## 🎯 Step 4: Snapshot 管理（1-2 天）

### Step 4 - 目標

實作快照序列化，使用原子性寫入。

### Step 4 - 檔案

- `internal/snapshot/snapshot.go`（已有偽代碼）
- `internal/snapshot/snapshot_test.go`（新建）

### Step 4 - 實作內容

#### 4.1 SnapshotData 結構

```go
type SnapshotData struct {
    Queue       []types.Job                   `json:"queue"`
    InFlight    map[string]types.InFlightInfo `json:"in_flight"`
    Completed   []string                      `json:"completed"`
    Dead        []string                      `json:"dead"`
    LastSeq     uint64                        `json:"last_seq"`
    SchemaVer   int                           `json:"schema_version"`
    Timestamp   int64                         `json:"timestamp"`
}
```

#### 4.2 Manager 實作

1. `NewManager(path) *Manager`
2. `Write(data SnapshotData) error` - 使用 temp + rename
3. `Load() (SnapshotData, error)`
4. `Exists() bool`

### 關鍵：原子性寫入

```go
func (m *Manager) Write(data SnapshotData) error {
    m.mu.Lock()
    defer m.mu.Unlock()

    data.SchemaVer = 1
    data.Timestamp = time.Now().Unix()

    jsonData, _ := json.MarshalIndent(data, "", "  ")

    tmpPath := m.path + ".tmp"
    os.WriteFile(tmpPath, jsonData, 0644)
    os.Rename(tmpPath, m.path)  // 原子操作

    return nil
}
```

### Step 4 - 測試重點

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
