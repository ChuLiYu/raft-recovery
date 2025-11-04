# Phase 1 快速參考手冊

本文件提供實作過程中的常見問題、設計決策理由、以及程式碼範例。

---

## 🤔 常見問題 (FAQ)

### Q1: 為什麼要同時使用 WAL 和 Snapshot？

**答**：兩者各有優勢，結合使用達到最佳效果。

| 機制                 | 優點                                                     | 缺點                                             |
| -------------------- | -------------------------------------------------------- | ------------------------------------------------ |
| **WAL（日誌）**      | • 寫入快速（追加）<br>• 保證持久性<br>• 精確記錄所有操作 | • 恢復慢（需重放所有事件）<br>• 檔案持續增長     |
| **Snapshot（快照）** | • 恢復快速（直接載入）<br>• 檔案大小固定                 | • 寫入慢（全量序列化）<br>• 可能丟失快照後的操作 |

**結合策略**：

1. **正常運作**：寫 WAL（低延遲）
2. **定期快照**：每 Δ 秒快照一次，然後清空 WAL
3. **恢復流程**：載入最新快照 + 重放 WAL 增量

**範例時間軸**：

```text
T0: [快照] 100 個任務完成
T1: [WAL] DISPATCH task-101
T2: [WAL] ACK task-101
T3: [WAL] DISPATCH task-102
T4: [崩潰！]
T5: [恢復] 載入 T0 快照 + 重放 T1~T3 的 WAL
```

---

### Q2: 如何確保 WAL 重放的冪等性？

**問題**：如果 WAL 包含 `ACK task-001`，但快照中 `task-001` 已在 `completed`，重放時會出錯嗎？

**解決方案**：在重放時檢查當前狀態。

```go
// 錯誤做法（非冪等）
case "ACK":
    queue.MarkCompleted(event.JobID)  // 如果已完成會出錯

// 正確做法（冪等）
case "ACK":
    if !queue.IsCompleted(event.JobID) {
        queue.MarkCompleted(event.JobID)
    }
```

**完整範例**：

```go
func (c *Controller) replayWAL() error {
    handler := func(event Event) error {
        switch event.Type {
        case "DISPATCH":
            // 檢查是否已完成或已死亡
            if queue.IsCompleted(event.JobID) || queue.IsDead(event.JobID) {
                return nil  // 跳過，已處理過
            }
            queue.MarkInFlight(event.JobID, time.Now().Add(timeout))

        case "ACK":
            if !queue.IsCompleted(event.JobID) {
                queue.MarkCompleted(event.JobID)
            }

        case "RETRY":
            job := queue.GetJob(event.JobID)
            if job != nil && !queue.IsCompleted(event.JobID) {
                queue.Requeue(*job)
            }
        }
        return nil
    }

    return wal.Replay(handler)
}
```

---

### Q3: 為什麼 Snapshot 需要「原子性寫入」？

**問題場景**：

```text
T0: 開始寫入快照到 snapshot.json（耗時 500ms）
T1: 寫入到一半，系統崩潰
T2: 重啟後載入 snapshot.json → 得到損壞的 JSON！
```

**解決方案**：使用 temp file + rename 模式。

```go
// 1. 寫入臨時檔（可能失敗，但不影響舊快照）
tmpPath := "snapshot.json.tmp"
os.WriteFile(tmpPath, jsonData, 0644)

// 2. 原子重新命名（POSIX 保證原子性）
os.Rename(tmpPath, "snapshot.json")
```

**為什麼 `rename` 是原子的？**

- POSIX 規範保證 `rename()` 系統呼叫是原子操作
- 要嘛成功（新檔案出現），要嘛失敗（舊檔案保留）
- 不會出現「半成品」狀態

**延伸閱讀**：[Linux rename(2) man page](https://man7.org/linux/man-pages/man2/rename.2.html)

---

### Q4: 為什麼需要 `file.Sync()` ？

**問題**：即使呼叫了 `file.Write()`，資料可能還在作業系統緩衝區，未真正寫入磁碟。

**危險場景**：

```text
T0: file.Write(walEvent)  → 寫入 OS 緩衝區
T1: 系統崩潰（斷電）
T2: 重啟後 → WAL 檔案缺少該事件！
```

**解決方案**：

```go
file.Write(data)
file.Sync()  // 強制刷新到磁碟（fsync 系統呼叫）
```

**效能影響**：

- `Sync()` 會等待磁碟 I/O 完成，可能延遲 1-10ms
- 對於每秒數千次寫入，可能成為瓶頸

**優化策略**：

```go
// 策略 1：批次寫入
events := []Event{}
for event := range eventCh {
    events = append(events, event)
    if len(events) >= 100 {
        writeAll(events)
        file.Sync()
        events = events[:0]
    }
}

// 策略 2：定時 Sync
lastSync := time.Now()
for event := range eventCh {
    file.Write(event)
    if time.Since(lastSync) > 10*time.Millisecond {
        file.Sync()
        lastSync = time.Now()
    }
}
```

---

### Q5: 如何處理超時任務？

**流程**：

```text
T0: 任務分派給 Worker，記錄 deadline = T0 + 3s
T1: Worker 執行中...
T3: Worker 仍在執行（可能卡住）
T3+: Controller 偵測到超時 → 重新排隊
T4: Worker 完成（晚了）→ ACK 被忽略（因已不在 in_flight）
```

**實作**：

```go
// Controller 定時檢查超時
func (c *Controller) timeoutLoop() {
    ticker := time.NewTicker(1 * time.Second)
    for range ticker.C {
        c.mu.Lock()
        now := time.Now()

        expiredIDs := c.queue.GetExpiredInFlight(now)
        for _, jobID := range expiredIDs {
            c.wal.Append("TIMEOUT", jobID)
            job := c.queue.GetJob(jobID)
            job.Attempt++

            if job.Attempt >= c.config.MaxRetry {
                c.queue.MarkDead(jobID)
            } else {
                c.queue.Requeue(*job)
            }
        }

        c.mu.Unlock()
    }
}

// 處理遲到的 ACK
func (c *Controller) handleAck(result Result) {
    c.mu.Lock()
    defer c.mu.Unlock()

    // 檢查任務是否還在 in_flight
    if !c.queue.IsInFlight(result.JobID) {
        log.Printf("忽略遲到的 ACK: %s", result.JobID)
        return
    }

    c.queue.MarkCompleted(result.JobID)
    c.wal.Append("ACK", result.JobID)
}
```

---

### Q6: 為什麼要用 Channel 而不是直接呼叫函式？

**比較**：

**方式 1：直接呼叫（耦合）**

```go
// Controller 直接呼叫 Worker
func (c *Controller) dispatch(job Job) {
    c.worker.Execute(job)  // 阻塞！
}
```

**問題**：

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

**優點**：

- Controller 和 Worker 解耦
- 自然支援並發（多個 Worker goroutine）
- 符合 Go 的「通過通訊共享記憶體」哲學

---

### Q7: 如何驗證系統的正確性？

**三層測試策略**：

#### 1. 單元測試（隔離測試）

```go
// 測試 Queue 的不變性
func TestQueueInvariant(t *testing.T) {
    q := NewQueue()

    // 加入 100 個任務
    for i := 0; i < 100; i++ {
        q.Enqueue(Job{ID: fmt.Sprintf("task-%d", i)})
    }

    // 模擬狀態轉換
    for i := 0; i < 50; i++ {
        job := q.PopPending()
        q.MarkInFlight(job.ID, time.Now().Add(1*time.Second))
    }

    // 驗證不變性
    if err := q.Validate(); err != nil {
        t.Fatal(err)
    }
}
```

#### 2. 整合測試（端到端）

```go
func TestCrashRecovery(t *testing.T) {
    // 1. 啟動系統
    ctrl := NewController(config)
    ctrl.Start()

    // 2. 加入任務
    ctrl.EnqueueJobs(make100Jobs())

    // 3. 等待部分完成
    time.Sleep(2 * time.Second)
    beforeCrash := ctrl.GetStatus()

    // 4. 模擬崩潰
    ctrl.Stop()

    // 5. 重啟
    ctrl2 := NewController(config)
    start := time.Now()
    ctrl2.Start()
    recoveryTime := time.Since(start)

    // 6. 驗證
    assert.Less(t, recoveryTime, 3*time.Second)

    // 7. 等待所有任務完成
    waitForCompletion(ctrl2)
    afterRecover := ctrl2.GetStatus()

    // 8. 驗證最終狀態
    total := afterRecover["completed"] + afterRecover["dead"]
    assert.Equal(t, 100, total)
}
```

#### 3. 競爭檢測

```bash
# 自動偵測資料競爭
go test -race ./...

# 常見問題：
# - 未加鎖訪問共享變數
# - Goroutine 洩漏
```

---

## 🎯 設計決策理由

### 決策 1: 為什麼使用 JSON 而不是二進位格式？

**考量**：

| 格式                 | 優點                                             | 缺點                      |
| -------------------- | ------------------------------------------------ | ------------------------- |
| **JSON**             | • 人類可讀（除錯友善）<br>• 跨語言相容<br>• 簡單 | • 體積較大<br>• 解析較慢  |
| **Protocol Buffers** | • 體積小<br>• 解析快                             | • 需要 schema<br>• 不可讀 |
| **自訂二進位**       | • 極致效能                                       | • 複雜度高<br>• 易出錯    |

**結論**：Phase 1 專注於**理解概念**，JSON 的可讀性更重要。Phase 3 可考慮優化。

---

### 決策 2: 為什麼 Controller 使用單一 `sync.Mutex`？

**考量**：

**方案 A：單一全域鎖**

```go
type Controller struct {
    mu    sync.Mutex  // 保護所有狀態
    queue *Queue
    ...
}

func (c *Controller) dispatch() {
    c.mu.Lock()
    defer c.mu.Unlock()
    // 修改 queue
}
```

**優點**：簡單，不會死鎖  
**缺點**：可能限制並發

**方案 B：細粒度鎖**

```go
type Controller struct {
    queueMu    sync.Mutex
    walMu      sync.Mutex
    metricsMu  sync.Mutex
    ...
}
```

**優點**：更高並發  
**缺點**：容易死鎖，複雜度高

**結論**：Phase 1 使用方案 A，除非效能測試顯示鎖競爭嚴重。

---

### 決策 3: 為什麼 Worker 使用 `context.WithTimeout` 而不是 `time.After`？

**比較**：

```go
// 方式 1: time.After（有問題）
select {
case <-time.After(timeout):
    return errors.New("timeout")
default:
    doWork()  // 無法中斷！
}

// 方式 2: context.WithTimeout（正確）
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()

doWorkWithContext(ctx)  // 可以監聽 ctx.Done() 並中斷
```

**Context 的優勢**：

- 可以主動取消
- 可以傳遞到深層函式
- 是 Go 的標準模式

---

## 📋 測試資料範例

### 1. 任務 JSON 檔案（`test-jobs.json`）

```json
[
  {
    "id": "task-001",
    "payload": {
      "type": "compute",
      "operation": "fibonacci",
      "input": 30
    },
    "attempt": 0,
    "status": "pending"
  },
  {
    "id": "task-002",
    "payload": {
      "type": "io",
      "operation": "write_file",
      "path": "/tmp/test.txt",
      "content": "Hello World"
    },
    "attempt": 0,
    "status": "pending"
  },
  {
    "id": "task-003",
    "payload": {
      "type": "network",
      "operation": "http_get",
      "url": "https://api.github.com"
    },
    "attempt": 0,
    "status": "pending"
  }
]
```

**產生大量測試資料**：

```go
func generateTestJobs(count int) []Job {
    jobs := make([]Job, count)
    for i := 0; i < count; i++ {
        jobs[i] = Job{
            ID: fmt.Sprintf("task-%04d", i),
            Payload: map[string]interface{}{
                "index": i,
                "value": rand.Intn(1000),
            },
            Attempt: 0,
            Status:  StatusPending,
        }
    }
    return jobs
}
```

---

### 2. 配置檔範例（`configs/default.yaml`）

```yaml
# Worker 配置
worker_count: 8
task_timeout: 3s

# 快照配置
snapshot_interval: 2s

# 重試配置
max_retry: 3

# 儲存路徑
wal_path: ./data/wal.log
snapshot_path: ./data/snapshot.json

# 監控
metrics_port: 9090
```

**測試用配置**（`configs/test.yaml`）：

```yaml
worker_count: 2
task_timeout: 1s
snapshot_interval: 500ms
max_retry: 2
wal_path: ./test-data/wal.log
snapshot_path: ./test-data/snapshot.json
metrics_port: 9091
```

---

### 3. WAL 檔案範例

```json
{"seq":1,"type":"ENQUEUE","job_id":"task-001","timestamp":"2024-01-01T10:00:00Z","checksum":123456}
{"seq":2,"type":"DISPATCH","job_id":"task-001","timestamp":"2024-01-01T10:00:01Z","checksum":234567}
{"seq":3,"type":"ACK","job_id":"task-001","timestamp":"2024-01-01T10:00:02Z","checksum":345678}
{"seq":4,"type":"DISPATCH","job_id":"task-002","timestamp":"2024-01-01T10:00:03Z","checksum":456789}
{"seq":5,"type":"TIMEOUT","job_id":"task-002","timestamp":"2024-01-01T10:00:06Z","checksum":567890}
{"seq":6,"type":"RETRY","job_id":"task-002","timestamp":"2024-01-01T10:00:06Z","checksum":678901}
```

---

### 4. Snapshot 檔案範例

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
  "schema_version": 1
}
```

---

## 🔧 除錯技巧

### 1. 印出 WAL 事件

```bash
# 使用 jq 格式化顯示
cat data/wal.log | jq '.'

# 過濾特定類型
cat data/wal.log | jq 'select(.type == "TIMEOUT")'

# 統計事件類型
cat data/wal.log | jq -r '.type' | sort | uniq -c
```

### 2. 驗證 Snapshot

```bash
# 檢查 JSON 格式
cat data/snapshot.json | jq '.'

# 檢查佇列深度
cat data/snapshot.json | jq '.queue | length'

# 檢查完成任務數
cat data/snapshot.json | jq '.completed | length'
```

### 3. 監控 Goroutine 洩漏

```go
import "runtime"

func (c *Controller) Stop() {
    close(c.stopCh)

    // 等待所有 goroutine 結束
    time.Sleep(100 * time.Millisecond)

    // 檢查 goroutine 數量
    before := runtime.NumGoroutine()
    time.Sleep(1 * time.Second)
    after := runtime.NumGoroutine()

    if after > before {
        log.Printf("警告：可能有 goroutine 洩漏！before=%d, after=%d", before, after)
    }
}
```

### 4. 使用 pprof 效能分析

```go
import _ "net/http/pprof"

func main() {
    // 啟動 pprof
    go func() {
        http.ListenAndServe(":6060", nil)
    }()

    // ... 正常邏輯
}
```

```bash
# CPU profiling
go tool pprof http://localhost:6060/debug/pprof/profile?seconds=30

# Memory profiling
go tool pprof http://localhost:6060/debug/pprof/heap

# Goroutine profiling
go tool pprof http://localhost:6060/debug/pprof/goroutine
```

---

## 🚨 常見錯誤與解決

### 錯誤 1: 死鎖

**症狀**：程式卡住不動

**原因**：

```go
// 錯誤：在鎖內呼叫可能需要鎖的函式
func (c *Controller) dispatch() {
    c.mu.Lock()
    defer c.mu.Unlock()

    c.handleResult(result)  // 這個函式也需要鎖！→ 死鎖
}
```

**解決**：

```go
func (c *Controller) dispatch() {
    c.mu.Lock()
    // ... 只做必要操作
    c.mu.Unlock()

    // 在鎖外呼叫
    c.handleResult(result)
}
```

---

### 錯誤 2: Channel 阻塞

**症狀**：goroutine 永久等待

**原因**：

```go
ch := make(chan int)  // 無緩衝 channel
ch <- 1  // 阻塞！沒有接收者
```

**解決**：

```go
// 方案 1: 使用緩衝
ch := make(chan int, 100)

// 方案 2: 在 goroutine 中發送
go func() {
    ch <- 1
}()
```

---

### 錯誤 3: Race Condition

**症狀**：`go test -race` 報錯

**範例錯誤**：

```go
type Counter struct {
    count int  // 沒有保護！
}

func (c *Counter) Inc() {
    c.count++  // 非原子操作
}

// 多個 goroutine 同時呼叫會出錯
```

**解決**：

```go
type Counter struct {
    mu    sync.Mutex
    count int
}

func (c *Counter) Inc() {
    c.mu.Lock()
    defer c.mu.Unlock()
    c.count++
}
```

---

## 📊 效能調校

### 瓶頸 1: WAL 寫入慢

**問題**：每次 `Append()` 都 `Sync()`，延遲高

**解決**：批次 Sync

```go
type WAL struct {
    events     []Event
    lastSync   time.Time
    syncTicker *time.Ticker
}

func (w *WAL) Append(event Event) {
    w.mu.Lock()
    w.events = append(w.events, event)

    // 每 10ms 或累積 100 個事件才 Sync
    if len(w.events) >= 100 || time.Since(w.lastSync) > 10*time.Millisecond {
        w.flush()
    }
    w.mu.Unlock()
}

func (w *WAL) flush() {
    for _, e := range w.events {
        w.encoder.Encode(e)
    }
    w.file.Sync()
    w.events = w.events[:0]
    w.lastSync = time.Now()
}
```

---

### 瓶頸 2: 鎖競爭

**診斷**：

```bash
go test -bench=. -cpuprofile=cpu.prof
go tool pprof cpu.prof
(pprof) top
```

**優化**：使用 `sync.RWMutex`

```go
type Queue struct {
    mu sync.RWMutex  // 讀寫鎖
    ...
}

func (q *Queue) Stats() map[string]int {
    q.mu.RLock()  // 只需讀鎖
    defer q.mu.RUnlock()

    return map[string]int{
        "pending": len(q.queue),
        ...
    }
}
```

---

## 📈 監控指標解讀

### Prometheus 查詢範例

```promql
# 吞吐量（每秒完成任務數）
rate(queue_jobs_completed_total[1m])

# P95 延遲
histogram_quantile(0.95, rate(queue_job_duration_seconds_bucket[5m]))

# 佇列積壓
queue_depth_gauge

# 重試率
rate(queue_jobs_retried_total[1m]) / rate(queue_jobs_dispatched_total[1m])
```

### Grafana Dashboard 面板建議

1. **吞吐量面板**：折線圖顯示 `rate(queue_jobs_completed_total[1m])`
2. **延遲面板**：熱力圖顯示延遲分布
3. **佇列深度**：區域圖顯示 pending/in_flight/completed
4. **錯誤率**：長條圖顯示 retry/timeout/dead

---

## 🎓 進階挑戰

完成基本實作後，可嘗試：

### 挑戰 1: 優先級佇列

```go
type Job struct {
    ID       string
    Priority int  // 0-10，數字越大越優先
    ...
}

// 使用 heap 實現優先級佇列
type PriorityQueue []*Job

func (pq PriorityQueue) Less(i, j int) bool {
    return pq[i].Priority > pq[j].Priority
}
```

### 挑戰 2: 延遲任務

```go
type Job struct {
    ID          string
    ScheduledAt time.Time  // 何時執行
    ...
}

// 只分派已到期的任務
func (c *Controller) dispatchLoop() {
    for {
        job := c.queue.PopReadyJob(time.Now())
        if job == nil {
            time.Sleep(100 * time.Millisecond)
            continue
        }
        // ...
    }
}
```

### 挑戰 3: Job 依賴

```go
type Job struct {
    ID          string
    DependsOn   []string  // 依賴的任務 ID
    ...
}

// 只分派依賴已完成的任務
func (q *Queue) PopReadyJob() *Job {
    for _, job := range q.queue {
        if q.allDependenciesCompleted(job.DependsOn) {
            return job
        }
    }
    return nil
}
```

---

好了！您現在有：

1. ✅ **詳細假代碼**（`phase1-pseudocode.md`）
2. ✅ **實作指南**（`phase1-implementation-guide.md`）
3. ✅ **快速參考**（本文件）

開始動手實作吧！💪

有問題隨時參考這些文件，或查閱：

- [Go 官方文件](https://go.dev/doc/)
- [Effective Go](https://go.dev/doc/effective_go)
- 相關文件中的「延伸閱讀」章節

祝您學習順利！🚀
