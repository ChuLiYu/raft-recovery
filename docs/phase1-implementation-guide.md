# Phase 1 實作練習指南

本指南提供逐步實作建議，幫助您循序漸進地完成 Beaver-Raft Phase 1。

---

## 📋 檔案結構清單

實作前先建立以下目錄結構：

```
beaver-raft/
├── cmd/
│   └── queue/
│       └── main.go                 # ⭐ CLI 入口
│
├── internal/
│   ├── controller/
│   │   ├── controller.go          # ⭐ 核心調度器
│   │   └── controller_test.go
│   │
│   ├── worker/
│   │   ├── worker.go              # ⭐ 任務執行器
│   │   ├── pool.go                # ⭐ Worker Pool
│   │   └── worker_test.go
│   │
│   ├── storage/
│   │   ├── wal/
│   │   │   ├── wal.go             # ⭐ Write-Ahead Log
│   │   │   └── wal_test.go
│   │   └── snapshot/
│   │       ├── snapshot.go        # ⭐ 快照管理
│   │       └── snapshot_test.go
│   │
│   ├── job/
│   │   ├── queue.go               # ⭐ 佇列狀態管理
│   │   └── queue_test.go
│   │
│   └── metrics/
│       └── metrics.go             # ⭐ Prometheus 指標
│
├── pkg/
│   └── types/
│       └── types.go               # ⭐ 公開型別定義
│
├── test/
│   ├── integration/
│   │   └── recovery_test.go       # 崩潰恢復測試
│   └── chaos/
│       └── fault_injection_test.go # 故障注入測試
│
├── scripts/
│   └── demo.sh                    # 示範腳本
│
├── configs/
│   └── default.yaml               # 預設配置
│
├── data/                          # 執行時資料目錄（.gitignore）
│   ├── wal.log
│   └── snapshot.json
│
├── go.mod
├── go.sum
├── Makefile
└── README.md
```

**建立指令**：

```bash
mkdir -p cmd/queue \
         internal/{controller,worker,storage/{wal,snapshot},job,metrics} \
         pkg/types \
         test/{integration,chaos} \
         scripts \
         configs \
         data

# 加入到 .gitignore
echo "data/" >> .gitignore
echo "bin/" >> .gitignore
```

---

## 🎯 實作順序（按依賴關係）

### 階段一：基礎資料結構（第 1-2 天）

#### 1️⃣ `pkg/types/types.go` - 型別定義

**難度**：⭐  
**學習重點**：Go 結構體、JSON/YAML 標籤

```go
// 需實作的型別：
- JobStatus (enum)
- Job struct
- InFlightInfo struct
- State struct (快照結構)
- Config struct
```

**驗證方式**：

```bash
go run -c pkg/types/types.go  # 確保編譯通過
```

---

#### 2️⃣ `internal/job/queue.go` - 佇列管理

**難度**：⭐⭐  
**學習重點**：`sync.Mutex`、map 操作、slice 操作

**實作清單**：

- [ ] `type Queue struct` - 定義私有欄位
- [ ] `NewQueue()` - 建構函式
- [ ] `Enqueue(job Job)` - 加入任務
- [ ] `PopPending() *Job` - 彈出待處理任務
- [ ] `MarkInFlight(jobID, deadline)` - 標記執行中
- [ ] `MarkCompleted(jobID)` - 標記完成
- [ ] `Requeue(job)` - 重新排隊
- [ ] `MarkDead(jobID)` - 標記失敗
- [ ] `GetExpiredInFlight(now)` - 取得超時任務
- [ ] `Snapshot()` - 產生快照
- [ ] `RestoreFromSnapshot(state)` - 恢復狀態
- [ ] `Validate()` - 驗證不變性
- [ ] `Stats()` - 取得統計

**測試要點**（`queue_test.go`）：

```go
func TestQueueInvariant(t *testing.T)  // 測試不變性
func TestEnqueueDequeue(t *testing.T)  // 測試基本操作
func TestTimeout(t *testing.T)         // 測試超時偵測
func TestSnapshot(t *testing.T)        // 測試快照與恢復
```

**執行測試**：

```bash
go test -v internal/job/queue_test.go
go test -race internal/job/  # 檢查競爭條件
```

---

### 階段二：持久化層（第 3-4 天）

#### 3️⃣ `internal/storage/wal/wal.go` - Write-Ahead Log

**難度**：⭐⭐⭐  
**學習重點**：檔案 I/O、`fsync`、CRC32 校驗

**實作清單**：

- [ ] `type Event struct` - 事件結構（含校驗和）
- [ ] `type WAL struct` - WAL 結構
- [ ] `NewWAL(path)` - 開啟/建立 WAL 檔案
- [ ] `Append(eventType, jobID)` - 追加事件
- [ ] `Replay(handler)` - 重放所有事件
- [ ] `Rotate()` - 旋轉清空日誌
- [ ] `Close()` - 關閉檔案

**關鍵技術點**：

```go
// CRC32 校驗
import "hash/crc32"
checksum := crc32.ChecksumIEEE([]byte(data))

// 強制寫入磁碟
file.Sync()

// JSON 編碼/解碼
encoder := json.NewEncoder(file)
encoder.Encode(event)
```

**測試要點**（`wal_test.go`）：

```go
func TestAppendAndReplay(t *testing.T)  // 測試追加與重放
func TestChecksum(t *testing.T)         // 測試校驗和驗證
func TestRotate(t *testing.T)           // 測試日誌旋轉
```

---

#### 4️⃣ `internal/storage/snapshot/snapshot.go` - 快照管理

**難度**：⭐⭐  
**學習重點**：原子性寫入、JSON 序列化

**實作清單**：

- [ ] `type Manager struct`
- [ ] `NewManager(path)`
- [ ] `Write(state State)` - 原子寫入快照
- [ ] `Load() (State, error)` - 載入快照
- [ ] `Exists()` - 檢查快照存在

**原子寫入模式**：

```go
// 1. 寫入臨時檔
tmpPath := path + ".tmp"
os.WriteFile(tmpPath, data, 0644)

// 2. 原子重新命名
os.Rename(tmpPath, path)
```

**測試要點**（`snapshot_test.go`）：

```go
func TestWriteAndLoad(t *testing.T)     // 測試寫入與載入
func TestAtomicWrite(t *testing.T)      // 測試原子性
func TestSchemaVersion(t *testing.T)    // 測試版本驗證
```

---

### 階段三：Worker 層（第 5-6 天）

#### 5️⃣ `internal/worker/worker.go` - Worker 執行器

**難度**：⭐⭐  
**學習重點**：`context.WithTimeout`、channel 通訊

**實作清單**：

- [ ] `type Task struct`
- [ ] `type Result struct`
- [ ] `type Worker struct`
- [ ] `NewWorker(id, taskCh, resultCh)`
- [ ] `Run()` - 主循環
- [ ] `execute(ctx, payload)` - 執行任務（模擬）

**超時控制**：

```go
ctx, cancel := context.WithTimeout(context.Background(), timeout)
defer cancel()

select {
case <-ctx.Done():
    return ctx.Err()
case <-time.After(workDuration):
    return nil
}
```

---

#### 6️⃣ `internal/worker/pool.go` - Worker Pool

**難度**：⭐⭐  
**學習重點**：goroutine 管理、`sync.WaitGroup`

**實作清單**：

- [ ] `type Pool struct`
- [ ] `NewPool()`
- [ ] `Start(workerCount)` - 啟動 N 個 Worker
- [ ] `Submit(task)` - 提交任務
- [ ] `ReceiveResult()` - 接收結果
- [ ] `Stop()` - 停止所有 Worker

**Worker 管理模式**：

```go
for i := 0; i < count; i++ {
    worker := NewWorker(i, taskCh, resultCh)
    wg.Add(1)
    go func() {
        defer wg.Done()
        worker.Run()
    }()
}
```

**測試要點**（`worker_test.go`）：

```go
func TestWorkerExecution(t *testing.T)  // 測試任務執行
func TestTimeout(t *testing.T)          // 測試超時處理
func TestPool(t *testing.T)             // 測試 Pool 管理
```

---

### 階段四：Controller 核心（第 7-9 天）

#### 7️⃣ `internal/controller/controller.go` - 控制器

**難度**：⭐⭐⭐⭐  
**學習重點**：並發控制、狀態機、事件驅動

**實作清單**：

- [ ] `type Controller struct`
- [ ] `NewController(config)`
- [ ] `Start()` - 啟動流程
- [ ] `loadSnapshot()` - 載入快照
- [ ] `replayWAL()` - 重放日誌
- [ ] `dispatchLoop()` - 調度循環
- [ ] `resultLoop()` - 結果處理循環
- [ ] `handleResult(result)` - 處理單一結果
- [ ] `timeoutLoop()` - 超時檢查循環
- [ ] `snapshotLoop()` - 快照循環
- [ ] `EnqueueJobs(jobs)` - 加入任務
- [ ] `GetStatus()` - 取得狀態
- [ ] `Stop()` - 停止

**重放 WAL 關鍵邏輯**：

```go
handler := func(event Event) error {
    switch event.Type {
    case "DISPATCH":
        if !queue.IsCompleted(event.JobID) {  // 冪等性檢查
            queue.MarkInFlight(event.JobID, ...)
        }
    case "ACK":
        queue.MarkCompleted(event.JobID)
    // ... 其他事件
    }
    return nil
}
wal.Replay(handler)
```

**測試要點**（`controller_test.go`）：

```go
func TestStartup(t *testing.T)          // 測試啟動流程
func TestDispatch(t *testing.T)         // 測試任務分派
func TestRetry(t *testing.T)            // 測試重試邏輯
func TestWALReplay(t *testing.T)        // 測試 WAL 重放
```

---

### 階段五：監控與 CLI（第 10-11 天）

#### 8️⃣ `internal/metrics/metrics.go` - Prometheus 指標

**難度**：⭐⭐  
**學習重點**：Prometheus client

**實作清單**：

- [ ] `type Collector struct`
- [ ] `NewCollector()` - 建立並註冊指標
- [ ] `IncrementDispatched()`
- [ ] `RecordCompletion(jobID, duration)`
- [ ] `IncrementRetry()`
- [ ] `IncrementDead()`
- [ ] `IncrementTimeout()`
- [ ] `RecordRecoveryTime(duration)`
- [ ] `UpdateQueueDepth(depth)`
- [ ] `StartMetricsServer(port)` - 啟動 HTTP 伺服器

**Prometheus 範例**：

```go
import (
    "github.com/prometheus/client_golang/prometheus"
    "github.com/prometheus/client_golang/prometheus/promhttp"
)

counter := prometheus.NewCounter(prometheus.CounterOpts{
    Name: "queue_jobs_total",
    Help: "Total number of jobs",
})
prometheus.MustRegister(counter)

http.Handle("/metrics", promhttp.Handler())
http.ListenAndServe(":9090", nil)
```

---

#### 9️⃣ `cmd/queue/main.go` - CLI

**難度**：⭐⭐  
**學習重點**：Cobra 框架、訊號處理

**實作清單**：

- [ ] `main()` - 主函式
- [ ] `enqueueCmd` - enqueue 命令
- [ ] `runCmd` - run 命令
- [ ] `statusCmd` - status 命令
- [ ] `loadConfig()` - 載入配置

**Cobra 範例**：

```go
import "github.com/spf13/cobra"

var rootCmd = &cobra.Command{Use: "queue"}

var runCmd = &cobra.Command{
    Use:   "run",
    Short: "啟動佇列處理器",
    Run: func(cmd *cobra.Command, args []string) {
        // 實作邏輯
    },
}

func init() {
    runCmd.Flags().IntP("workers", "w", 8, "Worker 數量")
    rootCmd.AddCommand(runCmd)
}

func main() {
    rootCmd.Execute()
}
```

**訊號處理**：

```go
sigCh := make(chan os.Signal, 1)
signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
<-sigCh
fmt.Println("正在關閉...")
controller.Stop()
```

---

### 階段六：測試與示範（第 12-14 天）

#### 🔟 `test/integration/recovery_test.go` - 整合測試

**難度**：⭐⭐⭐  
**學習重點**：整合測試、進程管理

**測試場景**：

1. **崩潰恢復測試**：

   ```go
   func TestCrashRecovery(t *testing.T) {
       // 1. 啟動 Controller，加入 100 個任務
       // 2. 等待部分完成
       // 3. 停止 Controller（模擬崩潰）
       // 4. 重啟並測量恢復時間 < 3s
       // 5. 驗證所有任務最終完成
       // 6. 驗證無重複執行
   }
   ```

2. **冪等性測試**：

   ```go
   func TestIdempotentReplay(t *testing.T) {
       // 測試 WAL 重放多次結果相同
   }
   ```

3. **並發測試**：
   ```bash
   go test -race ./test/integration/
   ```

---

#### 1️⃣1️⃣ `test/chaos/fault_injection_test.go` - 混沌測試

**難度**：⭐⭐⭐⭐

**測試場景**：

```go
func TestRandomKill(t *testing.T) {
    // 隨機時間點終止進程，驗證恢復
}

func TestIOError(t *testing.T) {
    // 模擬磁碟 I/O 錯誤
}
```

---

#### 1️⃣2️⃣ `scripts/demo.sh` - 示範腳本

```bash
#!/bin/bash
set -e

echo "=== Beaver-Raft Phase 1 Demo ==="

# 1. 清理
rm -rf data/
mkdir -p data/

# 2. 產生測試任務
cat > /tmp/jobs.json <<'EOF'
[
  {"id": "task-001", "payload": {"value": 42}},
  {"id": "task-002", "payload": {"value": 100}}
]
EOF

# 3. 啟動
./bin/queue run --workers 4 &
PID=$!
sleep 2

# 4. 加入任務
./bin/queue enqueue --file /tmp/jobs.json

# 5. 模擬崩潰
sleep 3
kill -9 $PID

# 6. 恢復
./bin/queue run --workers 4 &
PID=$!
sleep 2

# 7. 查看狀態
./bin/queue status

# 清理
kill $PID
```

---

#### 1️⃣3️⃣ `Makefile`

```makefile
.PHONY: all build test demo clean

all: build

build:
	@echo "編譯..."
	go build -o bin/queue cmd/queue/main.go

test:
	@echo "單元測試..."
	go test ./internal/... -v
	@echo "競爭檢測..."
	go test ./internal/... -race
	@echo "整合測試..."
	go test ./test/... -v

demo: build
	@echo "執行示範..."
	./scripts/demo.sh

clean:
	rm -rf bin/ data/

deps:
	go mod download
	go mod tidy

lint:
	golangci-lint run

coverage:
	go test ./... -coverprofile=coverage.out
	go tool cover -html=coverage.out
```

---

## 📊 效能目標驗證

### KPI 1: 崩潰恢復時間 < 3s

**測量方法**：

```go
start := time.Now()
controller.Start()  // 包含載入快照 + 重放 WAL
elapsed := time.Since(start)

if elapsed > 3*time.Second {
    t.Errorf("恢復時間過長: %v", elapsed)
}
```

### KPI 2: 吞吐量 ≥ 200 jobs/s

**測量方法**：

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

### KPI 3: Race Detector 通過

```bash
go test -race ./...
# 應無任何警告
```

---

## 🐛 除錯技巧

### 1. 使用 Delve 除錯器

```bash
go install github.com/go-delve/delve/cmd/dlv@latest
dlv debug cmd/queue/main.go -- run --workers 4
```

### 2. 加入 Debug Log

```go
import "log"

log.Printf("[DEBUG] Dispatching job %s", job.ID)
```

### 3. 檢視 WAL 內容

```bash
cat data/wal.log | jq .
```

### 4. 檢視快照

```bash
cat data/snapshot.json | jq .
```

---

## 🎓 學習檢查清單

完成實作後，確認您理解：

### 並發控制

- [ ] 為什麼 `Controller.queue` 需要 `mu` 保護？
- [ ] 哪些操作必須在鎖內執行？
- [ ] `defer mu.Unlock()` 的作用？
- [ ] `sync.RWMutex` 何時比 `sync.Mutex` 更好？

### WAL 機制

- [ ] 為什麼需要 CRC32 校驗和？
- [ ] `file.Sync()` 的作用與效能影響？
- [ ] 如何設計批次寫入優化效能？
- [ ] WAL 與 Snapshot 如何配合？

### 快照機制

- [ ] 為什麼使用 temp file + rename？
- [ ] 直接覆蓋原檔案的風險？
- [ ] 如何處理快照過大的問題？

### 狀態機

- [ ] Job 的狀態轉換路徑？
- [ ] 如何保證「每個 Job 只在一個集合」的不變性？
- [ ] 超時任務如何重新排隊？

### 崩潰恢復

- [ ] 恢復流程的順序為何？
- [ ] 如何實現冪等性重放？
- [ ] 為什麼 WAL + Snapshot 能保證狀態一致性？

### 分散式概念

- [ ] 這個系統對應 CAP 理論的哪些特性？
- [ ] 如果擴展到多節點，需要哪些改動？
- [ ] 與 Kafka、RabbitMQ 的差異？

---

## 📚 推薦閱讀

### Go 語言

- [Effective Go](https://golang.org/doc/effective_go)
- [Go Concurrency Patterns](https://go.dev/blog/pipelines)
- [sync 套件文件](https://pkg.go.dev/sync)

### 分散式系統

- [Designing Data-Intensive Applications](https://dataintensive.net/) - 第 3 章（儲存與檢索）
- [Write-Ahead Logging](https://en.wikipedia.org/wiki/Write-ahead_logging)
- [Checkpointing in Distributed Systems](https://www.cs.utexas.edu/~lorenzo/corsi/cs380d/papers/chandy.pdf)

### 相關專案

- [etcd WAL](https://github.com/etcd-io/etcd/tree/main/server/storage/wal)
- [BadgerDB](https://github.com/dgraph-io/badger) - Go 語言 LSM-tree 資料庫

---

## 🚀 完成後的下一步

恭喜完成 Phase 1！您已掌握：

- ✅ Go 並發程式設計
- ✅ WAL 與 Checkpoint 機制
- ✅ 崩潰恢復原理
- ✅ 系統監控與測試

**準備 Phase 2**：

- 多節點部署
- HTTP RPC 通訊
- 服務發現與心跳
- Grafana 視覺化監控

加油！💪
