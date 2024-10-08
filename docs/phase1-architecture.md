# Phase 1 系統架構文檔

## 📚 系統總覽

### 🎯 核心目標

Phase 1 是一個**單節點的快照感知任務佇列系統**，重點在於：

- 實現並發任務處理
- 支援崩潰後快速恢復（< 3 秒）
- 保證任務不遺失、不重複執行

### 價值主張

- 展示並發處理、崩潰恢復、可重啟快照的能力
- 提供易於演示的故事：終止進程、重啟，佇列自動恢復
- 單節點控制器協調多個 Worker goroutine
- 持久化狀態透過 JSON 快照加上預寫日誌（WAL）

---

## 🏗️ 系統架構圖

```
                    ┌─────────────────────────────────────┐
                    │         Controller (調度中樞)        │
                    │                                     │
                    │  ┌─────────────────────────────┐   │
                    │  │   四個核心循環 (Goroutines)  │   │
                    │  │                             │   │
                    │  │  • dispatchLoop()           │   │
                    │  │  • resultLoop()             │   │
                    │  │  • timeoutLoop()            │   │
                    │  │  • snapshotLoop()           │   │
                    │  └─────────────────────────────┘   │
                    └──────────┬──────────────────────────┘
                               │
              ┌────────────────┼────────────────┐
              │                │                │
       ┌──────▼──────┐  ┌─────▼─────┐  ┌──────▼──────┐
       │    State    │                                               │    WAL    │      │  Snapshot   │
       │  (狀態管理)  │  │  (日誌)   │  │  (快照)     │
       └─────────────┘  └───────────┘  └─────────────┘
              │
       ┌──────▼──────────────────────┐
       │      Worker Pool             │
       │  ┌────┐ ┌────┐ ┌────┐       │
       │  │ W1 │ │ W2 │ │ W3 │ ...   │
       │  └────┘ └────┘ └────┘       │
       └──────────────────────────────┘
```

---

## 📁 模組結構

```
internal/
├── jobmanager/      # 任務狀態管理 (原 state)
│   ├── job_manager.go
│   └── job_manager_test.go
├── storage/wal/     # WAL 持久化
│   ├── wal.go
│   ├── wal_test.go
│   └── README.md
├── snapshot/        # 快照管理
│   └── snapshot_manager.go
├── controller/      # 協調器
│   └── controller.go
└── worker/          # Worker Pool
    └── worker_pool.go
```

---

## 🔧 核心元件詳解

### 1️⃣ JobManager（任務管理器）

**位置：** `internal/jobmanager/job_manager.go`

**職責：**

- 管理任務的四種狀態集合：
  - `queue`：待處理佇列（FIFO）
  - `inFlight`：執行中的任務（記錄 deadline）
  - `completed`：已完成的任務
  - `dead`：失敗超過重試次數的任務

**核心不變性：**

> 每個任務 ID **只能存在於一個集合中**

**關鍵方法：**

```go
// 基本操作
Enqueue(job Job)                    // 加入任務
PopPending() *Job                   // 取出待處理任務

// 狀態轉換
MarkInFlight(jobID, deadline)       // 標記為執行中
MarkCompleted(jobID)                // 標記完成
Requeue(job)                        // 重新排隊（失敗重試）
MarkDead(jobID)                     // 標記為死信

// 超時檢測
GetExpiredJobs(now) []string        // 找出超時任務

// 持久化支援
Snapshot() SnapshotData             // 生成快照
Restore(data)                       // 從快照恢復
Validate() error                    // 驗證不變性
```

**並發安全：**

- 使用 `sync.RWMutex` 保護所有狀態
- 讀操作用 `RLock()`，寫操作用 `Lock()`

**資料結構：**

```go
type State struct {
    mu        sync.RWMutex           // 讀寫鎖
    queue     []Job                  // 待處理佇列
    inFlight  map[JobID]InFlightInfo // 執行中（記錄 deadline）
    completed map[JobID]bool         // 已完成
    dead      map[JobID]Job          // 失敗（超過重試）
}

type Job struct {
    ID        string                 // 唯一識別碼
    Payload   []byte                 // 任務資料（JSON）
    Attempt   int                    // 重試次數
    CreatedAt time.Time              // 創建時間
}

type InFlightInfo struct {
    WorkerID   int                   // 執行的 Worker ID
    DeadlineMs int64                 // 超時時間（毫秒）
}
```

---

### 2️⃣ WAL（Write-Ahead Log，預寫日誌）

**位置：** `internal/storage/wal/`

**職責：**

- 記錄所有狀態變更事件（在實際變更前）
- 崩潰後可重放事件恢復狀態
- 使用 CRC32 校驗和防止資料損壞

**事件類型：**

```json
{
  "seq": 1,
  "type": "DISPATCH", // 類型：ENQUEUE, DISPATCH, ACK, RETRY, TIMEOUT, DEAD
  "job_id": "task-001",
  "timestamp": 1730790000,
  "checksum": 123456
}
```

**關鍵特性：**

- **追加模式（Append-Only）**：只在文件末尾追加，永不修改已寫入內容
- **`fsync` 保證持久性**：每次寫入後強制刷新到磁碟
- **日誌輪轉（Rotate）**：快照後可清空 WAL

**核心方法：**

```go
Append(eventType, jobID)            // 追加事件
Replay(handler func(Event) error)   // 重放所有事件
Rotate()                            // 清空日誌（快照後）
Close()                             // 關閉文件
```

**寫入流程：**

```go
// 1. 構建事件
event := Event{
    Seq:       nextSeq,
    Type:      eventType,
    JobID:     jobID,
    Timestamp: time.Now().Unix(),
}
event.Checksum = CalculateChecksum(event)

// 2. 寫入文件
encoder.Encode(event)

// 3. 強制刷新到磁碟
file.Sync()  // fsync 系統呼叫
```

---

### 3️⃣ Snapshot Manager（快照管理器）

**位置：** `internal/snapshot/snapshot_manager.go`

**職責：**

- 定期保存完整狀態到磁碟（JSON 格式）
- 使用**原子寫入**防止快照損壞

**快照格式：**

```json
{
  "queue": [
    {
      "id": "task-003",
      "payload": { "value": 100 },
      "attempt": 0,
      "status": "pending"
    }
  ],
  "in_flight": {
    "task-002": {
      "worker_id": 3,
      "deadline_ms": 1704105606000
    }
  },
  "completed": ["task-001"],
  "dead": [],
  "last_seq": 6,
  "schema_version": 1,
  "timestamp": 1730790000
}
```

**原子寫入技術：**

```go
// 1. 寫入臨時文件
tmpPath := "snapshot.json.tmp"
os.WriteFile(tmpPath, jsonData, 0644)

// 2. 原子重命名（POSIX 保證原子性）
os.Rename(tmpPath, "snapshot.json")
```

> 💡 **為什麼原子性重要？** 即使寫入過程中崩潰，舊快照仍然完好！POSIX 規範保證 `rename()` 系統呼叫是原子操作，要嘛成功（新檔案出現），要嘛失敗（舊檔案保留），不會出現「半成品」狀態。

**核心方法：**

```go
Write(data SnapshotData) error      // 原子寫入快照
Load() (SnapshotData, error)        // 載入快照
Exists() bool                       // 檢查快照存在
```

---

### 4️⃣ Worker Pool（工作池）

**位置：** `internal/worker/worker_pool.go`

**職責：**

- 管理 N 個 Worker goroutine
- 接收 Controller 分派的任務
- 並發執行任務，回報結果

**通訊方式：**

```go
type Pool struct {
    workers  []*Worker
    taskCh   chan Task      // Controller → Worker（緩衝）
    resultCh chan Result    // Worker → Controller（緩衝）
    stopCh   chan struct{}  // 停止訊號
    wg       sync.WaitGroup // 等待所有 Worker
}

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

**Worker 執行流程：**

```
1. 從 taskCh 接收任務
2. 創建帶超時的 Context
3. 執行任務（模擬工作 100-500ms）
4. 將結果發送到 resultCh
```

**超時控制：**

```go
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()

select {
case <-ctx.Done():
    return ctx.Err()  // 超時！
case <-time.After(workDuration):
    return nil        // 完成
}
```

**優雅關閉：**

```go
func (p *Pool) Stop() {
    close(p.stopCh)    // 訊號所有 Worker 停止
    close(p.taskCh)    // Worker 的 range 循環會結束
    p.wg.Wait()        // 等待所有 Worker 完成當前任務
    close(p.resultCh)  // 關閉結果通道
}
```

---

### 5️⃣ Controller（控制器 - 系統中樞）

**位置：** `internal/controller/controller.go`

**職責：**

- 協調所有模組（JobManager, WAL, Snapshot, WorkerPool）
- 實現**四個核心循環**
- 處理崩潰恢復流程
- 確保狀態一致性與冪等性

**結構：**

```go
type Controller struct {
    mu       sync.Mutex
    state    *State
    wal      *WAL
    snapshot *SnapshotManager
    pool     *WorkerPool
    config   Config
    stopCh   chan struct{}
}

type Config struct {
    WorkerCount      int
    TaskTimeout      time.Duration
    SnapshotInterval time.Duration
    MaxRetry         int
    WALPath          string
    SnapshotPath     string
    MetricsPort      int
}
```

---

## ⚙️ 系統運作流程

### 🚀 啟動流程（含崩潰恢復）

```
Start()
  │
  ├─ 1. loadSnapshot()
  │    ↓
  │    載入最新快照 → State.Restore()
  │    測量恢復時間（目標 < 3s）
  │
  ├─ 2. replayWAL()
  │    ↓
  │    重放 WAL 增量事件 → 應用到 State
  │    （冪等性檢查：已完成的任務跳過）
  │
  ├─ 3. 啟動 Worker Pool
  │    ↓
  │    pool.Start(workerCount)
  │    啟動 N 個 Worker goroutine
  │
  └─ 4. 啟動四個核心循環
       ↓
       go dispatchLoop()   // 調度任務
       go resultLoop()     // 處理結果
       go timeoutLoop()    // 檢查超時
       go snapshotLoop()   // 定期快照
```

**恢復時間目標：< 3 秒**

---

### 🔄 四個核心循環

#### **Loop 1: dispatchLoop（調度循環）**

**職責：** 從佇列取出任務，分派給 Worker

```go
func dispatchLoop() {
    for {
        select {
        case <-stopCh:
            return

        default:
            // 1. 從 State.queue 彈出任務
            mu.Lock()
            job := jobManager.PopPending()
            mu.Unlock()

            if job == nil {
                time.Sleep(100 * time.Millisecond)
                continue
            }

            // 2. 寫入 WAL（先記錄意圖！）
            wal.Append("DISPATCH", job.ID)

            // 3. 標記為執行中
            mu.Lock()
            deadline := time.Now().Add(config.TaskTimeout)
            state.MarkInFlight(job.ID, deadline)
            mu.Unlock()

            // 4. 提交給 Worker Pool
            pool.Submit(Task{
                ID:      job.ID,
                Payload: job.Payload,
                Timeout: config.TaskTimeout,
            })

            metrics.IncrementDispatched()
        }
    }
}
```

**關鍵：** WAL 必須在狀態變更前寫入（Write-Ahead）

---

#### **Loop 2: resultLoop（結果處理循環）**

**職責：** 接收 Worker 執行結果，更新狀態

```go
func resultLoop() {
    for {
        select {
        case <-stopCh:
            return

        case result := <-pool.ReceiveResult():
            handleResult(result)
        }
    }
}

func handleResult(result Result) {
    mu.Lock()
    defer mu.Unlock()

    job := state.GetJob(result.JobID)
    if job == nil {
        log.Warn("未知任務", result.JobID)
        return
    }

    if result.Success {
        // 成功：標記完成
        wal.Append("ACK", result.JobID)
        state.MarkCompleted(result.JobID)
        metrics.RecordCompletion(result.Duration)
    } else {
        // 失敗：重試或死信
        job.Attempt++

        if job.Attempt >= config.MaxRetry {
            wal.Append("DEAD", result.JobID)
            state.MarkDead(result.JobID)
            metrics.IncrementDead()
        } else {
            wal.Append("RETRY", result.JobID)
            state.Requeue(job)
            metrics.IncrementRetry()
        }
    }
}
```

---

#### **Loop 3: timeoutLoop（超時檢查循環）**

**職責：** 定期檢查執行中任務是否超時

```go
func timeoutLoop() {
    ticker := time.NewTicker(1 * time.Second)
    defer ticker.Stop()

    for {
        select {
        case <-stopCh:
            return

        case <-ticker.C:
            mu.Lock()

            expired := state.GetExpiredJobs(time.Now())

            for _, jobID := range expired {
                wal.Append("TIMEOUT", jobID)

                job := state.GetJob(jobID)
                job.Attempt++

                if job.Attempt >= config.MaxRetry {
                    state.MarkDead(jobID)
                } else {
                    state.Requeue(job)
                }

                metrics.IncrementTimeout()
            }

            mu.Unlock()
        }
    }
}
```

**超時處理流程：**

```
T0: 任務分派給 Worker，記錄 deadline = T0 + 3s
T1: Worker 執行中...
T3: Worker 仍在執行（可能卡住）
T3+: Controller 偵測到超時 → 重新排隊
T4: Worker 完成（晚了）→ ACK 被忽略（因已不在 in_flight）
```

---

#### **Loop 4: snapshotLoop（快照循環）**

**職責：** 定期保存完整狀態，清空 WAL

```go
func snapshotLoop() {
    ticker := time.NewTicker(config.SnapshotInterval)
    defer ticker.Stop()

    for {
        select {
        case <-stopCh:
            return

        case <-ticker.C:
            mu.Lock()

            // 取得當前狀態
            data := state.Snapshot()
            data.LastSeq = wal.CurrentSeq()

            mu.Unlock()

            // 寫入快照（不需要鎖，已深拷貝）
            err := snapshot.Write(data)
            if err != nil {
                log.Error("快照失敗", err)
                continue
            }

            // 旋轉 WAL
            err = wal.Rotate()
            if err != nil {
                log.Error("WAL 旋轉失敗", err)
            }
        }
    }
}
```

**重要：** Snapshot 後 WAL 可清空（因狀態已持久化）

---

## 📊 任務生命週期

### 狀態轉換圖

```
                          ┌─────────┐
                          │ Enqueue │
                          └────┬────┘
                               │
                          ┌────▼────┐
                          │  Queue  │ (待處理)
                          └────┬────┘
                               │ dispatchLoop
                          ┌────▼─────┐
                          │ InFlight │ (執行中)
                          └─┬──┬──┬──┘
                            │  │  │
            ┌───────────────┘  │  └──────────┐
            │                  │             │
      ┌─────▼──────┐    ┌─────▼─────┐  ┌────▼────┐
      │ Completed  │    │  Timeout  │  │  Retry  │
      └────────────┘    └─────┬─────┘  └────┬────┘
                              │             │
                         ┌────▼─────────────▼────┐
                         │  Attempt >= MaxRetry? │
                         └──┬────────────────┬───┘
                            │ Yes            │ No
                        ┌───▼───┐       ┌────▼────┐
                        │  Dead │       │ Requeue │
                        └───────┘       └─────────┘
```

### 狀態說明

| 狀態      | 描述                       | 資料結構位置        |
| --------- | -------------------------- | ------------------- |
| Queue     | 等待分派                   | `state.queue[]`     |
| InFlight  | Worker 正在執行            | `state.inFlight{}`  |
| Completed | 成功完成                   | `state.completed{}` |
| Dead      | 超過重試次數，進入死信佇列 | `state.dead{}`      |

---

## 🔐 關鍵設計決策

### 1. 為什麼需要 WAL + Snapshot？

| 機制         | 優點                                               | 缺點                                         |
| ------------ | -------------------------------------------------- | -------------------------------------------- |
| **WAL**      | 寫入快速（追加）<br>保證持久性<br>精確記錄所有操作 | 恢復慢（需重放所有事件）<br>檔案持續增長     |
| **Snapshot** | 恢復快速（直接載入）<br>檔案大小固定               | 寫入慢（全量序列化）<br>可能丟失快照後的操作 |

**結合策略：**

- **正常運作** → 寫 WAL（低延遲）
- **定期快照** → 每 Δ 秒快照一次，然後清空 WAL
- **恢復流程** → 載入最新快照 + 重放 WAL 增量

**時間軸範例：**

```
T0: [快照] 100 個任務完成
T1: [WAL] DISPATCH task-101
T2: [WAL] ACK task-101
T3: [WAL] DISPATCH task-102
T4: [崩潰！]
T5: [恢復] 載入 T0 快照 + 重放 T1~T3 的 WAL
```

---

### 2. 冪等性重放（Idempotent Replay）

**問題：** 如果 WAL 包含 `ACK task-001`，但快照中 `task-001` 已在 `completed`，重放時會出錯嗎？

**解決方案：** 在重放時檢查當前狀態

```go
func replayWAL() error {
    handler := func(event Event) error {
        mu.Lock()
        defer mu.Unlock()

        switch event.Type {
        case "DISPATCH":
            // 檢查冪等性
            if state.IsCompleted(event.JobID) || state.IsDead(event.JobID) {
                return nil  // 跳過已處理的
            }
            deadline := time.Now().Add(config.TaskTimeout)
            state.MarkInFlight(event.JobID, deadline)

        case "ACK":
            if !state.IsCompleted(event.JobID) {  // 冪等性檢查
                state.MarkCompleted(event.JobID)
            }

        case "RETRY":
            if !state.IsCompleted(event.JobID) {
                state.Requeue(event.JobID)
            }

        case "TIMEOUT":
            if !state.IsCompleted(event.JobID) {
                state.Requeue(event.JobID)
            }

        case "DEAD":
            state.MarkDead(event.JobID)
        }

        return nil
    }

    return wal.Replay(handler)
}
```

**重要：** 冪等性檢查確保重複重放不會出錯

---

### 3. 並發控制策略

**選擇：** Controller 使用**單一全域鎖** (`sync.Mutex`)

**方案 A：單一全域鎖**

```go
type Controller struct {
    mu    sync.Mutex  // 保護所有狀態
    state *State
    ...
}

func (c *Controller) dispatch() {
    c.mu.Lock()
    defer c.mu.Unlock()
    // 修改 state
}
```

**優點：** 簡單，不會死鎖  
**缺點：** 可能限制並發

**方案 B：細粒度鎖**

```go
type Controller struct {
    queueMu    sync.Mutex
    walMu      sync.Mutex
    metricsMu  sync.Mutex
    ...
}
```

**優點：** 更高並發  
**缺點：** 容易死鎖，複雜度高

**結論：** Phase 1 使用方案 A，除非效能測試顯示鎖競爭嚴重。

---

### 4. 為什麼使用 Channel 而不是直接呼叫函式？

**方式 1：直接呼叫（耦合）**

```go
// Controller 直接呼叫 Worker
func (c *Controller) dispatch(job Job) {
    c.worker.Execute(job)  // 阻塞！
}
```

**問題：**

- Controller 會阻塞等待 Worker 完成
- 無法並發處理多個任務

**方式 2：Channel（解耦）**

```go
// Controller 發送到 Channel
func (c *Controller) dispatch(job Job) {
    c.taskCh <- Task{...}  // 非阻塞（如果 channel 有緩衝）
}

// Worker 獨立運作
func (w *Worker) Run() {
    for task := range w.taskCh {
        w.execute(task)
    }
}
```

**優點：**

- Controller 和 Worker 解耦
- 自然支援並發（多個 Worker goroutine）
- 符合 Go 的「通過通訊共享記憶體」哲學

---

## 📈 效能指標（KPI）

### 目標與驗證

| 指標         | 目標         | 驗證方式                        | 備註                |
| ------------ | ------------ | ------------------------------- | ------------------- |
| 崩潰恢復時間 | < 3 秒       | 測量 `loadSnapshot + replayWAL` | 接受 ±1 秒開銷      |
| 吞吐量       | ≥ 200 jobs/s | 處理 1000 個任務的時間          | 模擬 CPU 密集型工作 |
| 資料競爭     | 0 錯誤       | `go test -race`                 | 每次提交前必須通過  |

### 測量方法

**KPI 1: 崩潰恢復時間**

```go
start := time.Now()
controller.Start()  // 包含載入快照 + 重放 WAL
elapsed := time.Since(start)

if elapsed > 3*time.Second {
    t.Errorf("恢復時間過長: %v", elapsed)
}
```

**KPI 2: 吞吐量**

```go
start := time.Now()
controller.EnqueueJobs(make1000Jobs())
waitUntilComplete()
elapsed := time.Since(start)

throughput := 1000.0 / elapsed.Seconds()
if throughput < 200 {
    t.Errorf("吞吐量不足: %.2f jobs/s", throughput)
}
```

**KPI 3: Race Detector**

```bash
go test -race ./...
# 應無任何警告
```

---

## 🛠️ CLI 介面

### 命令範例

```bash
# 啟動系統
queue run --workers 8 --timeout 3s --snapshot 2s

# 加入任務
queue enqueue --file jobs.json

# 查看狀態
queue status
```

### 輸出範例

```json
{
  "stats": {
    "pending": 10,
    "in_flight": 5,
    "completed": 85,
    "dead": 0
  },
  "uptime": "5m30s"
}
```

### 任務檔案格式

```json
[
  {
    "id": "task-001",
    "payload": {
      "type": "compute",
      "operation": "fibonacci",
      "input": 30
    }
  },
  {
    "id": "task-002",
    "payload": {
      "type": "io",
      "operation": "write_file",
      "path": "/tmp/test.txt",
      "content": "Hello World"
    }
  }
]
```

---

## 🧪 測試策略

### 1. 單元測試

**JobManager 測試：**

```go
func TestEnqueueDequeue(t *testing.T) {
    jobManager := jobmanager.NewJobManager()

    // 加入 10 個任務
    for i := 0; i < 10; i++ {
        job := Job{ID: fmt.Sprintf("task-%d", i)}
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
```

**WAL 測試：**

```go
func TestAppendAndReplay(t *testing.T) { /* ... */ }
func TestChecksum(t *testing.T) {
    // 手動破壞 WAL 檔案，驗證能偵測
}
func TestRotate(t *testing.T) { /* ... */ }
```

**Snapshot 測試：**

```go
func TestWriteAndLoad(t *testing.T) { /* ... */ }
func TestAtomicWrite(t *testing.T) {
    // 模擬寫入中斷，驗證舊快照不損壞
}
```

---

### 2. 整合測試

**崩潰恢復測試：**

```go
func TestCrashRecovery(t *testing.T) {
    // 1. 啟動 Controller，加入 100 個任務
    ctrl := NewController(config)
    ctrl.Start()
    ctrl.EnqueueJobs(make100Jobs())

    // 2. 等待部分完成
    time.Sleep(2 * time.Second)
    beforeCrash := ctrl.GetStatus()

    // 3. 模擬崩潰
    ctrl.Stop()

    // 4. 重啟
    ctrl2 := NewController(config)
    start := time.Now()
    ctrl2.Start()
    recoveryTime := time.Since(start)

    // 5. 驗證
    assert.Less(t, recoveryTime, 3*time.Second)

    // 6. 等待所有任務完成
    waitForCompletion(ctrl2)
    afterRecover := ctrl2.GetStatus()

    // 7. 驗證最終狀態
    total := afterRecover["completed"] + afterRecover["dead"]
    assert.Equal(t, 100, total)
}
```

**冪等性測試：**

```go
func TestIdempotentReplay(t *testing.T) {
    // 測試 WAL 重放多次結果相同
}
```

---

### 3. 競爭檢測

```bash
# 自動偵測資料競爭
go test -race ./...

# 常見問題：
# - 未加鎖訪問共享變數
# - Goroutine 洩漏
```

---

### 4. 效能測試

```go
func BenchmarkThroughput(b *testing.B) {
    ctrl := NewController(config)
    ctrl.Start()
    defer ctrl.Stop()

    b.ResetTimer()
    for i := 0; i < b.N; i++ {
        ctrl.EnqueueJobs(make100Jobs())
        waitForCompletion(ctrl)
    }
}
```

---

## 🎯 總結

### 學習目標

完成 Phase 1 後，您已掌握：

- ✅ **Go 並發程式設計**

  - Goroutine 管理與生命週期
  - Channel 通訊模式
  - Mutex 並發控制
  - Context 與超時處理

- ✅ **持久化機制**

  - WAL（Write-Ahead Log）設計
  - Snapshot 快照策略
  - 原子性寫入技術
  - `fsync` 與資料持久性

- ✅ **崩潰恢復原理**

  - 事件重放（Replay）
  - 冪等性（Idempotency）
  - 狀態一致性保證

- ✅ **系統設計能力**
  - 模組化設計
  - 狀態機設計
  - 錯誤處理策略
  - 監控與測試

### Demo 效果

```bash
# 1. 啟動系統
./queue run --workers 8 &

# 2. 加入 100 個任務
./queue enqueue --file jobs.json

# 3. 查看狀態
./queue status
# Output: {"pending": 50, "in_flight": 8, "completed": 42}

# 4. 模擬崩潰
kill -9 $PID

# 5. 自動恢復（< 3 秒）
./queue run --workers 8 &

# 6. 任務繼續完成！
./queue status
# Output: {"pending": 0, "in_flight": 0, "completed": 100}
```

### 為 Phase 2/3 打基礎

- **Phase 2: FalconQueue** - 多節點部署，HTTP RPC，服務發現
- **Phase 3: Beaver-Raft** - Raft 共識協議，分散式一致性

---

## 📚 相關文件

- [Phase 1 實作指南](./phase1-implementation-guide.md)
- [Phase 1 快速參考](./phase1-quick-reference.md)
- [Phase 1 假代碼](./phase1-pseudocode.md)
- [實作順序](../IMPLEMENTATION_ORDER.md)

---

**版本：** 1.0  
**最後更新：** 2025-10-13
