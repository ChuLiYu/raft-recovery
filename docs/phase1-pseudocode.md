# Phase 1 假代碼與實作指引

本文件提供 Phase 1 各模組的假代碼，供您自行實作練習。

---

## 📁 模組一：pkg/types/types.go - 公開型別定義

### 設計意圖

定義系統核心資料結構，供所有模組共用。

### 假代碼

```go
// 任務狀態枚舉
定義 JobStatus 為字串型別
常數:
  - StatusPending = "pending"      // 待處理
  - StatusInFlight = "in_flight"   // 執行中
  - StatusCompleted = "completed"  // 已完成
  - StatusDead = "dead"            // 失敗超過重試次數

// 任務結構
型別 Job:
  欄位:
    - ID: 字串（唯一識別碼）
    - Payload: 映射表（任務資料）
    - Attempt: 整數（重試次數）
    - Status: JobStatus

// 執行中任務資訊
型別 InFlightInfo:
  欄位:
    - WorkerID: 整數（執行此任務的 Worker 編號）
    - DeadlineMs: 整數（截止時間的 Unix 毫秒時間戳）

// 完整狀態（用於快照）
型別 JobManager:
  欄位:
    - Queue: Job 陣列（待處理佇列）
    - InFlight: 映射表[字串 -> InFlightInfo]（執行中任務）
    - Completed: 字串陣列（已完成任務 ID）
    - Dead: 字串陣列（失敗任務 ID）
    - LastSeq: 無符號整數（WAL 最後序號）
    - SchemaVer: 整數（狀態結構版本，用於未來相容性）

// 系統配置
型別 Config:
  欄位:
    - WorkerCount: 整數（Worker 數量）
    - TaskTimeout: 時間間隔（任務超時時間）
    - SnapshotInterval: 時間間隔（快照間隔）
    - MaxRetry: 整數（最大重試次數）
    - WALPath: 字串（WAL 檔案路徑）
    - SnapshotPath: 字串（快照檔案路徑）
    - MetricsPort: 整數（Prometheus 指標埠）
```

### 實作提示

- 使用 JSON tag 支援序列化
- 使用 YAML tag 支援配置檔讀取
- JobStatus 使用 `type JobStatus string` 實現類型安全

---

## 📁 模組二：internal/job/queue.go - 佇列狀態管理

### 設計意圖

維護三個集合（queue, in_flight, completed），確保每個任務只存在於一個集合中。

### 假代碼

```go
型別 Queue:
  私有欄位:
    - mu: 互斥鎖（保護所有欄位）
    - queue: Job 陣列
    - inFlight: 映射表[JobID -> InFlightInfo]
    - completed: 映射表[JobID -> 布林值]（用 map 加速查找）
    - dead: 映射表[JobID -> Job]
    - jobIndex: 映射表[JobID -> *Job]（快速查找任何狀態的 Job）

// 建構函式
函式 NewQueue() -> *Queue:
  回傳新的 Queue 實例，初始化所有映射表

// 加入新任務
方法 Enqueue(job Job):
  鎖定 mu
  defer 解鎖

  如果 job.ID 已存在於任何集合:
    回傳錯誤「任務 ID 重複」

  job.Status = StatusPending
  job.Attempt = 0
  加入 job 到 queue 陣列
  jobIndex[job.ID] = &job

// 彈出待處理任務
方法 PopPending() -> *Job:
  鎖定 mu
  defer 解鎖

  如果 queue 為空:
    回傳 nil

  job := queue[0]
  queue = queue[1:]  // 移除第一個元素
  回傳 &job

// 標記為執行中
方法 MarkInFlight(jobID 字串, deadline 時間) -> 錯誤:
  鎖定 mu
  defer 解鎖

  如果 jobID 不存在:
    回傳錯誤

  inFlight[jobID] = InFlightInfo{
    WorkerID: -1,  // 可選：追蹤哪個 Worker
    DeadlineMs: deadline.UnixMilli()
  }

  更新 jobIndex[jobID].Status = StatusInFlight

// 標記為完成
方法 MarkCompleted(jobID 字串) -> 錯誤:
  鎖定 mu
  defer 解鎖

  如果 jobID 不在 inFlight:
    回傳錯誤「任務不在執行中狀態」

  刪除 inFlight[jobID]
  completed[jobID] = true
  更新 jobIndex[jobID].Status = StatusCompleted

// 重新排隊（用於重試）
方法 Requeue(job Job):
  鎖定 mu
  defer 解鎖

  刪除 inFlight[job.ID]
  job.Status = StatusPending
  追加 job 到 queue 陣列末尾
  jobIndex[job.ID] = &job

// 標記為死信
方法 MarkDead(jobID 字串):
  鎖定 mu
  defer 解鎖

  job := jobIndex[jobID]
  刪除 inFlight[jobID]
  dead[jobID] = *job
  job.Status = StatusDead

// 取得超時任務
方法 GetExpiredInFlight(now 時間) -> []字串:
  鎖定 mu
  defer 解鎖

  expired := 空字串陣列
  nowMs := now.UnixMilli()

  對於 inFlight 中每個 (jobID, info):
    如果 info.DeadlineMs < nowMs:
      追加 jobID 到 expired

  回傳 expired

// 取得任務（任何狀態）
方法 GetJob(jobID 字串) -> *Job:
  鎖定 mu
  defer 解鎖

  回傳 jobIndex[jobID]

// 產生快照
方法 Snapshot() -> JobManager:
  鎖定 mu
  defer 解鎖

  completedList := 從 completed 映射表提取所有鍵

  回傳 JobManager{
    Queue: 複製 queue 陣列,
    InFlight: 複製 inFlight 映射表,
    Completed: completedList,
    Dead: 從 dead 提取所有鍵,
    SchemaVer: 1
  }

// 從快照恢復
方法 RestoreFromSnapshot(jobManager JobManager):
  鎖定 mu
  defer 解鎖

  清空所有內部集合

  對於 jobManager.Queue 中每個 job:
    追加到 queue
    jobIndex[job.ID] = &job

  對於 jobManager.InFlight 中每個 (jobID, info):
    inFlight[jobID] = info
    // 重建 job（需從某處載入或重新排隊）

  對於 jobManager.Completed 中每個 jobID:
    completed[jobID] = true

  對於 jobManager.Dead 中每個 jobID:
    dead[jobID] = Job{ID: jobID}

// 驗證不變性（測試用）
方法 Validate() -> 錯誤:
  鎖定 mu
  defer 解鎖

  seen := 空映射表[JobID -> 整數]

  對於 queue 中每個 job:
    seen[job.ID]++

  對於 inFlight 中每個 jobID:
    seen[jobID]++

  對於 completed 中每個 jobID:
    seen[jobID]++

  對於 seen 中每個 (jobID, count):
    如果 count > 1:
      回傳錯誤「任務 {jobID} 出現在多個狀態」

  回傳 nil

// 取得統計資訊
方法 Stats() -> 映射表:
  鎖定 mu
  defer 解鎖

  回傳 映射表{
    "pending": len(queue),
    "in_flight": len(inFlight),
    "completed": len(completed),
    "dead": len(dead)
  }
```

### 實作提示

- 所有公開方法都需要加鎖
- 使用 `defer mu.Unlock()` 確保解鎖
- 考慮使用 `sync.RWMutex` 優化讀取效能（Stats 可用讀鎖）
- Snapshot 時需深拷貝，避免外部修改

---

## 📁 模組三：internal/storage/wal/wal.go - Write-Ahead Log

### 設計意圖

追加事件到日誌檔案，支援重放以恢復狀態，使用校驗和防止損壞。

### 假代碼

```go
型別 Event:
  欄位:
    - Seq: 無符號整數（事件序號）
    - Type: 字串（DISPATCH, ACK, RETRY, TIMEOUT, DEAD）
    - JobID: 字串
    - Timestamp: 時間戳
    - Checksum: 無符號32位整數（CRC32）

型別 WAL:
  私有欄位:
    - mu: 互斥鎖
    - file: 檔案指標
    - path: 字串
    - encoder: JSON 編碼器
    - seq: 無符號整數（當前序號）

// 建構函式
函式 NewWAL(path 字串) -> (*WAL, 錯誤):
  開啟或建立檔案於 path（追加模式）

  建立 WAL 實例:
    - file = 開啟的檔案
    - path = path
    - encoder = JSON 編碼器（寫入 file）
    - seq = 0

  如果檔案已存在且有內容:
    讀取最後一個事件以獲取 seq
    設定 wal.seq = 最後事件的 Seq

  回傳 wal, nil

// 追加事件
方法 Append(eventType 字串, jobID 字串) -> 錯誤:
  鎖定 mu
  defer 解鎖

  seq++

  event := Event{
    Seq: seq,
    Type: eventType,
    JobID: jobID,
    Timestamp: 現在時間
  }

  // 計算校驗和（使用 CRC32）
  data := eventType + jobID + 轉字串(seq)
  event.Checksum = CRC32(data)

  // 寫入檔案
  如果 encoder.Encode(event) 失敗:
    回傳錯誤

  // 強制同步到磁碟（可選：批次化以提升效能）
  如果 file.Sync() 失敗:
    回傳錯誤

  回傳 nil

// 重放所有事件
方法 Replay(handler 函式(Event) -> 錯誤) -> 錯誤:
  // 重新開啟檔案用於讀取
  readFile := 開啟檔案(path, 只讀模式)
  defer 關閉 readFile

  decoder := JSON 解碼器(readFile)

  循環:
    var event Event
    錯誤 := decoder.Decode(&event)

    如果錯誤 == EOF:
      跳出循環

    如果錯誤 != nil:
      回傳錯誤「WAL 損壞」

    // 驗證校驗和
    expectedChecksum := CRC32(event.Type + event.JobID + 轉字串(event.Seq))
    如果 event.Checksum != expectedChecksum:
      回傳錯誤「校驗和不符，seq={event.Seq}」

    // 呼叫處理函式應用事件
    如果 handler(event) 失敗:
      回傳錯誤

  回傳 nil

// 旋轉日誌（快照後清空）
方法 Rotate() -> 錯誤:
  鎖定 mu
  defer 解鎖

  關閉當前檔案

  // 重新命名舊檔案為備份（可選）
  備份路徑 := path + ".old"
  重新命名 file 到備份路徑

  // 建立新的空檔案
  newFile := 建立檔案(path)
  如果失敗:
    回傳錯誤

  file = newFile
  encoder = 新 JSON 編碼器(file)
  seq = 0  // 重置序號

  回傳 nil

// 關閉 WAL
方法 Close() -> 錯誤:
  鎖定 mu
  defer 解鎖

  回傳 file.Close()
```

### 實作提示

- 使用 `hash/crc32` 套件計算校驗和
- `file.Sync()` 確保資料寫入磁碟，但可能影響效能
- 考慮批次寫入：累積 N 個事件後才 Sync
- Rotate 時可保留舊檔案用於偵錯

---

## 📁 模組四：internal/storage/snapshot/snapshot.go - 快照管理

### 設計意圖

將完整狀態序列化為 JSON，使用原子性寫入防止損壞。

### 假代碼

```go
型別 Manager:
  私有欄位:
    - path: 字串（快照檔案路徑）
    - mu: 互斥鎖

// 建構函式
函式 NewManager(path 字串) -> *Manager:
  回傳 &Manager{path: path}

// 寫入快照
方法 Write(jobManager JobManager) -> 錯誤:
  鎖定 mu
  defer 解鎖

  // 序列化為 JSON（美化格式）
  data := JSON.MarshalIndent(jobManager, "", "  ")
  如果失敗:
    回傳錯誤

  // 原子性寫入：先寫臨時檔，再重新命名
  tmpPath := path + ".tmp"

  如果 WriteFile(tmpPath, data) 失敗:
    回傳錯誤

  // 重新命名（原子操作）
  如果 Rename(tmpPath, path) 失敗:
    回傳錯誤

  回傳 nil

// 載入快照
方法 Load() -> (JobManager, 錯誤):
  鎖定 mu
  defer 解鎖

  var jobManager JobManager

  // 讀取檔案
  data := ReadFile(path)

  如果檔案不存在:
    // 首次啟動，無快照
    回傳空 JobManager（SchemaVer=1）, nil

  如果讀取失敗:
    回傳 jobManager, 錯誤

  // 反序列化
  如果 JSON.Unmarshal(data, &jobManager) 失敗:
    回傳 jobManager, 錯誤「快照格式錯誤」

  // 驗證版本
  如果 jobManager.SchemaVer != 1:
    回傳 jobManager, 錯誤「不相容的快照版本」

  回傳 jobManager, nil

// 檢查快照是否存在
方法 Exists() -> 布林值:
  _, 錯誤 := 檔案資訊(path)
  回傳錯誤 == nil
```

### 實作提示

- 使用 `os.WriteFile` 和 `os.Rename` 實現原子寫入
- 考慮在 JSON 中加入時間戳記錄快照時間
- 未來擴展：壓縮大型快照（gzip）

---

## 📁 模組五：internal/worker/worker.go - Worker 執行器

### 設計意圖

接收任務，執行工作（帶超時），回報結果。

### 假代碼

```go
型別 Task:
  欄位:
    - ID: 字串
    - Payload: 映射表
    - Timeout: 時間間隔

型別 Result:
  欄位:
    - JobID: 字串
    - Success: 布林值
    - Error: 錯誤
    - Duration: 時間間隔

型別 Worker:
  私有欄位:
    - id: 整數
    - taskCh: 任務通道（只讀）
    - resultCh: 結果通道（只寫）

// 建構函式
函式 NewWorker(id 整數, taskCh 通道, resultCh 通道) -> *Worker:
  回傳 &Worker{
    id: id,
    taskCh: taskCh,
    resultCh: resultCh
  }

// 主循環
方法 Run():
  循環從 taskCh 接收任務:
    task := <-taskCh

    startTime := 現在時間

    // 建立帶超時的 Context
    ctx := Context.WithTimeout(背景 Context, task.Timeout)
    defer 取消 ctx

    // 執行工作
    錯誤 := execute(ctx, task.Payload)

    // 回報結果
    result := Result{
      JobID: task.ID,
      Success: (錯誤 == nil),
      Error: 錯誤,
      Duration: 現在時間 - startTime
    }

    resultCh <- result

// 執行具體工作（模擬）
私有方法 execute(ctx Context, payload 映射表) -> 錯誤:
  // 模擬 CPU 密集型工作
  workDuration := 隨機(100ms, 500ms)

  選擇:
    情況 <-ctx.Done():
      回傳 ctx.Err()  // 超時或取消

    情況 <-時間.After(workDuration):
      // 模擬 10% 失敗率
      如果 隨機數(0, 100) < 10:
        回傳錯誤「模擬執行失敗」

      回傳 nil
```

### 實作提示

- 使用 `context.WithTimeout` 處理超時
- 真實場景可執行實際業務邏輯（處理圖片、計算等）
- 考慮加入 Worker ID 到結果中，方便追蹤

---

## 📁 模組六：internal/worker/pool.go - Worker Pool

### 設計意圖

管理多個 Worker goroutine，分發任務，收集結果。

### 假代碼

```go
型別 Pool:
  私有欄位:
    - workers: Worker 陣列
    - taskCh: 任務通道（緩衝）
    - resultCh: 結果通道（緩衝）
    - stopCh: 停止訊號通道
    - wg: WaitGroup（等待所有 Worker 結束）

// 建構函式
函式 NewPool() -> *Pool:
  回傳 &Pool{
    taskCh: make(通道, 容量=100),
    resultCh: make(通道, 容量=100),
    stopCh: make(通道)
  }

// 啟動 Worker Pool
方法 Start(workerCount 整數):
  對於 i := 0 到 workerCount:
    worker := NewWorker(i, taskCh, resultCh)
    追加 worker 到 workers 陣列

    wg.Add(1)
    啟動 goroutine:
      worker.Run()
      wg.Done()

// 提交任務（非阻塞）
方法 Submit(task Task):
  taskCh <- task

// 接收結果（阻塞）
方法 ReceiveResult() -> Result:
  回傳 <-resultCh

// 停止所有 Worker
方法 Stop():
  關閉 taskCh  // Worker 會在 taskCh 關閉後退出
  關閉 stopCh
  wg.Wait()    // 等待所有 Worker 完成
  關閉 resultCh
```

### 實作提示

- 使用緩衝通道避免阻塞
- 關閉 `taskCh` 會讓所有 Worker 的 range 循環結束
- 考慮實作動態調整 Worker 數量（進階）

---

## 📁 模組七：internal/controller/controller.go - 控制器核心

### 設計意圖

協調所有模組，實現任務調度、狀態轉換、崩潰恢復。

### 假代碼

```go
型別 Controller:
  私有欄位:
    - mu: 互斥鎖
    - queue: *Queue
    - wal: *WAL
    - snapshot: *SnapshotManager
    - pool: *WorkerPool
    - metrics: *MetricsCollector
    - config: Config
    - stopCh: 停止訊號通道

// 建構函式
函式 NewController(config Config) -> (*Controller, 錯誤):
  queue := NewQueue()
  wal := NewWAL(config.WALPath)
  snapshot := NewSnapshotManager(config.SnapshotPath)
  pool := NewPool()
  metrics := NewMetricsCollector()

  回傳 &Controller{
    queue: queue,
    wal: wal,
    snapshot: snapshot,
    pool: pool,
    metrics: metrics,
    config: config,
    stopCh: make(通道)
  }, nil

// 啟動 Controller
方法 Start() -> 錯誤:
  // 1. 載入快照
  如果 loadSnapshot() 失敗:
    回傳錯誤

  // 2. 重放 WAL
  如果 replayWAL() 失敗:
    回傳錯誤

  // 3. 啟動 Worker Pool
  pool.Start(config.WorkerCount)

  // 4. 啟動後台循環
  啟動 goroutine: dispatchLoop()
  啟動 goroutine: resultLoop()
  啟動 goroutine: timeoutLoop()
  啟動 goroutine: snapshotLoop()

  回傳 nil

// 載入快照
私有方法 loadSnapshot() -> 錯誤:
  開始計時

  state, 錯誤 := snapshot.Load()
  如果錯誤:
    回傳錯誤

  如果 state 非空:
    queue.RestoreFromSnapshot(state)

  恢復時間 := 停止計時
  metrics.RecordRecoveryTime(恢復時間)

  回傳 nil

// 重放 WAL
私有方法 replayWAL() -> 錯誤:
  handler := 函式(event Event) -> 錯誤:
    根據 event.Type:
      情況 "DISPATCH":
        // 檢查任務是否已完成（冪等性）
        如果 queue 的 completed 包含 event.JobID:
          跳過此事件
        否則:
          queue.MarkInFlight(event.JobID, ...)

      情況 "ACK":
        queue.MarkCompleted(event.JobID)

      情況 "RETRY":
        job := queue.GetJob(event.JobID)
        job.Attempt++
        queue.Requeue(job)

      情況 "TIMEOUT":
        job := queue.GetJob(event.JobID)
        queue.Requeue(job)

      情況 "DEAD":
        queue.MarkDead(event.JobID)

    回傳 nil

  回傳 wal.Replay(handler)

// 調度循環
私有方法 dispatchLoop():
  循環:
    選擇:
      情況 <-stopCh:
        回傳

      預設:
        mu.Lock()
        job := queue.PopPending()
        mu.Unlock()

        如果 job == nil:
          睡眠 100ms
          繼續

        // 寫 WAL
        wal.Append("DISPATCH", job.ID)

        // 標記為 in_flight
        mu.Lock()
        deadline := 現在時間 + config.TaskTimeout
        queue.MarkInFlight(job.ID, deadline)
        mu.Unlock()

        // 發送給 Worker Pool
        pool.Submit(Task{
          ID: job.ID,
          Payload: job.Payload,
          Timeout: config.TaskTimeout
        })

        metrics.IncrementDispatched()

// 結果處理循環
私有方法 resultLoop():
  循環:
    選擇:
      情況 <-stopCh:
        回傳

      情況 result := <-pool.ReceiveResult():
        handleResult(result)

// 處理單一結果
私有方法 handleResult(result Result):
  mu.Lock()
  defer mu.Unlock()

  job := queue.GetJob(result.JobID)

  如果 result.Success:
    // 成功
    wal.Append("ACK", result.JobID)
    queue.MarkCompleted(result.JobID)
    metrics.RecordCompletion(result.JobID, result.Duration)

  否則:
    // 失敗 - 檢查重試次數
    job.Attempt++

    如果 job.Attempt >= config.MaxRetry:
      wal.Append("DEAD", result.JobID)
      queue.MarkDead(result.JobID)
      metrics.IncrementDead()

    否則:
      wal.Append("RETRY", result.JobID)
      queue.Requeue(*job)
      metrics.IncrementRetry()

// 超時檢查循環
私有方法 timeoutLoop():
  ticker := 每秒一次的計時器
  defer ticker.Stop()

  循環:
    選擇:
      情況 <-stopCh:
        回傳

      情況 <-ticker.C:
        mu.Lock()
        expiredIDs := queue.GetExpiredInFlight(現在時間)

        對於每個 jobID 在 expiredIDs:
          wal.Append("TIMEOUT", jobID)
          job := queue.GetJob(jobID)
          job.Attempt++
          queue.Requeue(*job)
          metrics.IncrementTimeout()

        mu.Unlock()

// 快照循環
私有方法 snapshotLoop():
  ticker := 計時器(config.SnapshotInterval)
  defer ticker.Stop()

  循環:
    選擇:
      情況 <-stopCh:
        回傳

      情況 <-ticker.C:
        mu.Lock()

        state := queue.Snapshot()
        state.LastSeq = wal.CurrentSeq()

        如果 snapshot.Write(state) 成功:
          wal.Rotate()  // 清空 WAL

        mu.Unlock()

// 加入任務
方法 EnqueueJobs(jobs []Job) -> 錯誤:
  mu.Lock()
  defer mu.Unlock()

  對於每個 job 在 jobs:
    wal.Append("ENQUEUE", job.ID)
    queue.Enqueue(job)

  回傳 nil

// 取得狀態
方法 GetStatus() -> 映射表:
  mu.Lock()
  defer mu.Unlock()

  回傳 queue.Stats()

// 停止 Controller
方法 Stop():
  關閉 stopCh
  pool.Stop()
  wal.Close()
```

### 實作提示

- 使用 `select` 語句處理多通道
- 所有修改狀態的操作都需加鎖
- 考慮使用 `context.Context` 優雅關閉
- 重放 WAL 時檢查 `completed` 實現冪等性

---

## 📁 模組八：internal/metrics/metrics.go - 監控指標

### 設計意圖

暴露 Prometheus 指標供監控系統收集。

### 假代碼

```go
型別 Collector:
  私有欄位:
    - jobsDispatched: Prometheus Counter
    - jobsCompleted: Prometheus Counter
    - jobsRetried: Prometheus Counter
    - jobsDead: Prometheus Counter
    - jobsTimeout: Prometheus Counter
    - jobLatency: Prometheus Histogram
    - recoveryTime: Prometheus Gauge
    - queueDepth: Prometheus Gauge

// 建構函式
函式 NewCollector() -> *Collector:
  collector := &Collector{
    jobsDispatched: prometheus.NewCounter({
      Name: "queue_jobs_dispatched_total",
      Help: "任務分派總數"
    }),
    jobsCompleted: prometheus.NewCounter({...}),
    jobLatency: prometheus.NewHistogram({
      Name: "queue_job_duration_seconds",
      Help: "任務執行時間",
      Buckets: [0.1, 0.5, 1, 2, 5]
    }),
    ...
  }

  // 註冊到 Prometheus
  prometheus.MustRegister(collector.jobsDispatched)
  prometheus.MustRegister(collector.jobsCompleted)
  // ... 註冊所有指標

  回傳 collector

// 記錄任務分派
方法 IncrementDispatched():
  jobsDispatched.Inc()

// 記錄任務完成
方法 RecordCompletion(jobID 字串, duration 時間間隔):
  jobsCompleted.Inc()
  jobLatency.Observe(duration.Seconds())

// 記錄重試
方法 IncrementRetry():
  jobsRetried.Inc()

// 記錄死信
方法 IncrementDead():
  jobsDead.Inc()

// 記錄超時
方法 IncrementTimeout():
  jobsTimeout.Inc()

// 記錄恢復時間
方法 RecordRecoveryTime(duration 時間間隔):
  recoveryTime.Set(duration.Seconds())

// 更新佇列深度
方法 UpdateQueueDepth(depth 整數):
  queueDepth.Set(浮點數(depth))

// 啟動 HTTP 伺服器
函式 StartMetricsServer(port 整數):
  http.Handle("/metrics", promhttp.Handler())
  http.ListenAndServe(":"+port, nil)
```

### 實作提示

- 使用 `github.com/prometheus/client_golang` 套件
- Counter 只能增加，Gauge 可增減
- Histogram 自動計算分位數（P50, P95, P99）

---

## 📁 模組九：cmd/queue/main.go - CLI 入口

### 設計意圖

提供命令列介面操作佇列系統。

### 假代碼

```go
主函式 main():
  rootCmd := cobra.Command{
    Use: "queue",
    Short: "Beaver-Raft Phase 1 Job Queue"
  }

  // enqueue 命令
  enqueueCmd := cobra.Command{
    Use: "enqueue --file jobs.json",
    Short: "加入任務到佇列",
    Run: 函式(cmd, args):
      filePath := cmd.Flags().GetString("file")

      // 讀取 JSON 檔案
      data := ReadFile(filePath)
      var jobs []Job
      JSON.Unmarshal(data, &jobs)

      // 載入配置
      config := loadConfig()

      // 建立 Controller
      controller := NewController(config)
      controller.Start()

      // 加入任務
      controller.EnqueueJobs(jobs)

      輸出 "已加入 {len(jobs)} 個任務"
  }
  enqueueCmd.Flags().StringP("file", "f", "", "任務 JSON 檔案")

  // run 命令
  runCmd := cobra.Command{
    Use: "run",
    Short: "啟動佇列處理器",
    Run: 函式(cmd, args):
      config := loadConfig()
      覆蓋配置從命令列旗標:
        - workers
        - timeout
        - snapshot-interval

      // 啟動 Controller
      controller := NewController(config)
      controller.Start()

      // 啟動 Metrics 伺服器
      啟動 goroutine: metrics.StartMetricsServer(config.MetricsPort)

      輸出 "Controller 已啟動"
      輸出 "Workers: {config.WorkerCount}"
      輸出 "Metrics: http://localhost:{config.MetricsPort}/metrics"

      // 等待終止訊號
      等待 SIGINT 或 SIGTERM

      輸出 "正在停止..."
      controller.Stop()
  }
  runCmd.Flags().IntP("workers", "w", 8, "Worker 數量")
  runCmd.Flags().DurationP("timeout", "t", 3*秒, "任務超時時間")
  runCmd.Flags().DurationP("snapshot", "s", 2*秒, "快照間隔")

  // status 命令
  statusCmd := cobra.Command{
    Use: "status",
    Short: "顯示佇列狀態",
    Run: 函式(cmd, args):
      // 讀取快照檔案
      snapshot := NewSnapshotManager(預設快照路徑)
      state, _ := snapshot.Load()

      輸出 "佇列狀態："
      輸出 "  待處理: {len(state.Queue)}"
      輸出 "  執行中: {len(state.InFlight)}"
      輸出 "  已完成: {len(state.Completed)}"
      輸出 "  失敗: {len(state.Dead)}"
  }

  rootCmd.AddCommand(enqueueCmd, runCmd, statusCmd)
  rootCmd.Execute()

// 載入配置檔
函式 loadConfig() -> Config:
  // 讀取 YAML 配置檔
  data := ReadFile("configs/default.yaml")
  var config Config
  YAML.Unmarshal(data, &config)
  回傳 config
```

### 實作提示

- 使用 `github.com/spf13/cobra` 建立 CLI
- 使用 `os/signal` 捕捉 SIGINT/SIGTERM
- 考慮支援環境變數覆蓋配置

---

## 📁 配置檔：configs/default.yaml

```yaml
worker_count: 8
task_timeout: 3s
snapshot_interval: 2s
max_retry: 3
wal_path: ./data/wal.log
snapshot_path: ./data/snapshot.json
metrics_port: 9090
```

---

## 📁 測試：test/integration/recovery_test.go

### 假代碼

```go
測試函式 TestCrashRecovery(t *testing.T):
  // 1. 啟動 Controller
  config := Config{
    WorkerCount: 4,
    TaskTimeout: 1 * 秒,
    ...
  }
  controller := NewController(config)
  controller.Start()

  // 2. 加入 100 個任務
  jobs := 產生 100 個測試任務
  controller.EnqueueJobs(jobs)

  // 3. 等待部分任務完成
  等待 500ms

  // 4. 模擬崩潰（停止 Controller）
  controller.Stop()

  // 5. 記錄當前狀態
  快照前狀態 := 讀取快照

  // 6. 重啟 Controller
  開始計時
  newController := NewController(config)
  newController.Start()
  恢復時間 := 停止計時

  // 7. 驗證恢復時間 < 3s
  斷言 恢復時間 < 3*秒

  // 8. 等待所有任務完成
  等待直到 所有任務完成

  // 9. 驗證所有任務都已處理
  最終狀態 := newController.GetStatus()
  斷言 最終狀態["completed"] + 最終狀態["dead"] == 100

  // 10. 驗證無重複執行（檢查 completed 集合）
  斷言 無重複 JobID

測試函式 TestWALReplay(t *testing.T):
  // 測試 WAL 重放的冪等性
  ...

測試函式 TestTimeoutHandling(t *testing.T):
  // 測試超時任務重新排隊
  ...
```

---

## 📁 示範腳本：scripts/demo.sh

```bash
#!/bin/bash

echo "=== Beaver-Raft Phase 1 Demo ==="

# 清理舊資料
rm -rf ./data
mkdir -p ./data

# 建立測試任務
cat > /tmp/jobs.json <<EOF
[
  {"id": "task-001", "payload": {"type": "compute", "value": 42}},
  {"id": "task-002", "payload": {"type": "compute", "value": 100}},
  ...
  {"id": "task-100", "payload": {"type": "compute", "value": 999}}
]
EOF

echo "1. 啟動 Controller..."
./queue run --workers 8 --timeout 3s &
PID=$!
sleep 2

echo "2. 加入 100 個任務..."
./queue enqueue --file /tmp/jobs.json

echo "3. 等待部分任務完成..."
sleep 3

echo "4. 模擬崩潰（kill -9）..."
kill -9 $PID
sleep 1

echo "5. 重啟 Controller（測量恢復時間）..."
START=$(date +%s%N)
./queue run --workers 8 &
PID=$!
sleep 2
END=$(date +%s%N)

RECOVERY_MS=$(( (END - START) / 1000000 ))
echo "   恢復時間: ${RECOVERY_MS}ms"

echo "6. 查看狀態..."
./queue status

echo "7. 等待所有任務完成..."
sleep 10

echo "8. 最終狀態..."
./queue status

kill $PID
echo "=== Demo 完成 ==="
```

---

## 📁 Makefile

```makefile
.PHONY: build test demo clean

build:
	@echo "編譯二進位檔..."
	go build -o bin/queue cmd/queue/main.go

test:
	@echo "執行單元測試..."
	go test ./... -v
	@echo "執行競爭檢測..."
	go test ./... -race

demo: build
	@echo "執行示範..."
	./scripts/demo.sh

clean:
	rm -rf bin/ data/

deps:
	go mod download
	go mod tidy

metrics:
	@echo "Metrics 可於 http://localhost:9090/metrics 查看"
	curl http://localhost:9090/metrics
```

---

## 🎯 實作步驟建議

### 第一週：基礎架構

1. 建立專案結構與 `go.mod`
2. 實作 `types.go` 資料結構
3. 實作 `queue.go` 狀態管理
4. 撰寫 `queue_test.go` 驗證不變性

### 第二週：儲存與執行

5. 實作 `wal.go` 與測試
6. 實作 `snapshot.go` 與測試
7. 實作 `worker.go` 與 `pool.go`
8. 實作 `controller.go` 基本邏輯

### 第三週：整合與示範

9. 實作 `metrics.go` 與 Prometheus 整合
10. 實作 `main.go` CLI
11. 撰寫整合測試與示範腳本
12. 效能調校與文件撰寫

---

## 🔍 學習檢查點

完成實作後，您應該能回答：

1. **並發控制**：為什麼 Controller 需要 `sync.Mutex`？哪些操作必須加鎖？
2. **WAL 機制**：為什麼需要校驗和？如果不做 `fsync` 會有什麼風險？
3. **快照原子性**：為什麼使用 temp file + rename？直接覆蓋原檔案有什麼問題？
4. **超時處理**：超時任務如何重新排隊？如何避免無限重試？
5. **冪等性**：WAL 重放時如何避免重複執行已完成任務？
6. **恢復保證**：為什麼 WAL + Snapshot 能保證狀態一致性？

---

## 📚 延伸閱讀

- [Raft 論文](https://raft.github.io/raft.pdf) - 第 5.3 節討論日誌壓縮
- [Write-Ahead Logging](https://en.wikipedia.org/wiki/Write-ahead_logging)
- [etcd 的 WAL 實作](https://github.com/etcd-io/etcd/tree/main/server/storage/wal)

祝您實作順利！🚀
