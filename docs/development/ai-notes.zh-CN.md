# AI 協助筆記

本文件記錄 AI 協助產生的設計決策、假代碼架構與實作建議。

---

## 📋 專案概覽

**目標**：實作 Beaver-Raft Phase 1 - Snapshot-Aware Job Queue

**核心特性**：

- 單節點任務佇列
- WAL + Snapshot 持久化
- 崩潰恢復（< 3s）
- 吞吐量 ≥ 200 jobs/s
- 並發安全（通過 `go test -race`）

---

## 🏗️ 系統架構

### 模組依賴圖

```text
main.go
  └─> cli.go
       └─> controller.go
            ├─> job_manager.go          (佇列狀態)
            ├─> wal.go            (日誌)
            ├─> snapshot_manager.go (快照)
            └─> worker_pool.go    (執行器)
```

### 資料流向

```text
1. 啟動階段:
   Snapshot.Load() → State.Restore()
   WAL.Replay() → 應用事件到 State

2. 正常執行:
   Controller.Enqueue → WAL.Append → State.Enqueue
   Controller.Dispatch → WAL.Append → State.MarkInFlight → Worker.Execute
   Worker.Result → Controller.Handle → WAL.Append → State.MarkCompleted

3. 定時快照:
   State.Snapshot() → Snapshot.Write() → WAL.Rotate()
```

---

## 🔑 關鍵設計決策

### 1. 為什麼用 WAL + Snapshot？

| 機制     | 優點           | 缺點           | 使用時機          |
| -------- | -------------- | -------------- | ----------------- |
| WAL      | 寫入快、精確   | 恢復慢、檔案大 | 每次狀態變更      |
| Snapshot | 恢復快、檔案小 | 寫入慢         | 定時（例如每 2s） |

**結合效果**：

- WAL 記錄增量（低延遲）
- Snapshot 定期全量保存（快速恢復）
- 恢復 = Load Snapshot + Replay WAL

### 2. 鎖的使用策略

**Controller 使用單一 `sync.Mutex`**：

- 優點：簡單、不會死鎖
- 缺點：可能限制並發

**替代方案**（Phase 2 考慮）：

- 使用 `sync.RWMutex` 優化讀操作
- 細粒度鎖（但需小心死鎖）

**原則**：Phase 1 先求正確，後求效能。

### 3. 冪等性保證

**問題**：WAL 重放時，事件可能已在 Snapshot 中。

**解決**：

```go
// 重放時檢查狀態
case "ACK":
  if !state.IsCompleted(jobID) {  // 冪等性檢查
    state.MarkCompleted(jobID)
  }
```

**測試**：手動重放同一個 WAL 兩次，驗證結果相同。

### 4. 原子性寫入

**Snapshot 使用 temp file + rename**：

```go
os.WriteFile(path + ".tmp", data)  // 寫臨時檔
os.Rename(path + ".tmp", path)     // 原子重命名
```

**為什麼**：

- POSIX 保證 `rename()` 是原子的
- 寫入中途崩潰，舊快照不會損壞

---

## 🧪 測試策略

### 單元測試重點

```go
// job_manager_test.go
TestEnqueueDequeue       // 基本佇列操作
TestStateTransitions     // 狀態轉換正確性
TestInvariant            // 不變性驗證
TestConcurrency          // 並發安全（-race）

// wal_test.go
TestAppendAndReplay      // 寫入與重放
TestChecksum             // 校驗和驗證
TestRotate               // 日誌旋轉

// snapshot_test.go
TestWriteAndLoad         // 序列化
TestAtomicWrite          // 原子性
TestVersionMismatch      // 版本檢查

// worker_pool_test.go
TestWorkerExecution      // 任務執行
TestTimeout              // 超時處理
TestGracefulShutdown     // 優雅關閉
```

### 整合測試重點

```go
// controller_test.go
TestCrashRecovery:
  1. 啟動 Controller
  2. 加入 100 個任務
  3. 等待 50 個完成
  4. Stop()（模擬崩潰）
  5. 重新 Start()
  6. 驗證恢復時間 < 3s
  7. 驗證剩餘任務完成

TestIdempotency:
  - 重放 WAL 兩次
  - 驗證結果相同
```

### 效能測試

```bash
# 吞吐量測試
go test -bench=BenchmarkThroughput -benchtime=10s

# 目標：1000 個任務 < 5s（200 jobs/s）

# 恢復時間測試
go test -run=TestRecoveryTime -count=10
# 目標：平均 < 3s
```

---

## 📝 實作順序建議

### Week 1: 基礎架構

1. `job_manager.go` - 佇列狀態管理
2. `wal.go` - 日誌追加與重放
3. `snapshot_manager.go` - 快照序列化

### Week 2: 執行與協調

1. `worker_pool.go` - 任務執行
2. `controller.go` - 四個循環（dispatch, result, timeout, snapshot）
3. 整合測試

### Week 3: CLI 與完善

1. `cli.go` - 命令列介面
2. `main.go` - 入口點
3. 效能測試與調校
4. 文件與示範

---

## 🐛 常見問題與解決

### Q1: 死鎖如何避免？

**錯誤做法**：

```go
func dispatch() {
    mu.Lock()
    defer mu.Unlock()
    handleResult()  // 這個也需要鎖！
}
```

**正確做法**：

```go
func dispatch() {
    mu.Lock()
    // 只做必要操作
    mu.Unlock()

    handleResult()  // 鎖外呼叫
}
```

### Q2: Channel 阻塞怎麼辦？

**使用緩衝 Channel**：

```go
taskCh := make(chan Task, 100)  // 緩衝大小 100
```

**或用 select 非阻塞**：

```go
select {
case taskCh <- task:
    // 成功
default:
    // 滿了，記錄或等待
}
```

### Q3: goroutine 洩漏如何檢測？

```go
before := runtime.NumGoroutine()
// ... 執行操作
time.Sleep(1 * time.Second)
after := runtime.NumGoroutine()

if after > before {
    log.Warn("可能有 goroutine 洩漏")
}
```

---

## 🎯 效能優化點（Phase 1 可選）

### 1. WAL 批次寫入

```go
// 累積 100 個事件或 10ms 才 Sync
if len(buffer) >= 100 || time.Since(lastSync) > 10*time.Millisecond {
    flush()
}
```

**效益**：降低 fsync 次數，提升 10x 吞吐量。

### 2. 使用 RWMutex

```go
// 讀多寫少的場景
func Stats() {
    mu.RLock()  // 讀鎖
    defer mu.RUnlock()
    // ...
}
```

### 3. Snapshot 壓縮

```go
import "compress/gzip"

writer := gzip.NewWriter(file)
json.NewEncoder(writer).Encode(data)
```

**效益**：大型佇列可節省 70% 磁碟空間。

---

## 📊 監控指標

### Prometheus 指標設計

```go
// Counter（只增）
queue_jobs_dispatched_total
queue_jobs_completed_total
queue_jobs_retried_total
queue_jobs_dead_total

// Histogram（分布）
queue_job_duration_seconds

// Gauge（可增減）
queue_depth_pending
queue_depth_in_flight

// 恢復時間
queue_recovery_duration_seconds
```

### Grafana 查詢範例

```promql
# 吞吐量（每秒完成數）
rate(queue_jobs_completed_total[1m])

# P95 延遲
histogram_quantile(0.95, rate(queue_job_duration_seconds_bucket[5m]))

# 重試率
rate(queue_jobs_retried_total[1m]) / rate(queue_jobs_dispatched_total[1m])
```

---

## 🔗 參考資料

### 論文與文章

- [Write-Ahead Logging - Wikipedia](https://en.wikipedia.org/wiki/Write-ahead_logging)
- [Raft Paper](https://raft.github.io/raft.pdf) - 第 5.3 節討論日誌壓縮

### 開源實作參考

- [etcd WAL](https://github.com/etcd-io/etcd/tree/main/server/storage/wal)
- [BadgerDB](https://github.com/dgraph-io/badger)

### Go 語言資源

- [Effective Go](https://go.dev/doc/effective_go)
- [Go Concurrency Patterns](https://go.dev/blog/pipelines)

---

## ✅ 實作檢查清單

### 基本功能

- [ ] 任務可以 Enqueue 到佇列
- [ ] Worker 並發執行任務
- [ ] 任務完成後正確更新狀態
- [ ] 失敗任務自動重試
- [ ] 超時任務重新排隊

### 持久化

- [ ] WAL 正確記錄所有事件
- [ ] WAL 校驗和驗證有效
- [ ] Snapshot 正確保存狀態
- [ ] 原子寫入防止損壞

### 崩潰恢復

- [ ] 載入 Snapshot 恢復基礎狀態
- [ ] 重放 WAL 恢復增量狀態
- [ ] 恢復時間 < 3s
- [ ] 重放具有冪等性

### 效能

- [ ] 吞吐量 ≥ 200 jobs/s
- [ ] 通過 `go test -race`
- [ ] 無 goroutine 洩漏

### CLI

- [ ] `enqueue` 命令正常運作
- [ ] `run` 命令可啟動系統
- [ ] `status` 命令顯示狀態
- [ ] SIGINT/SIGTERM 優雅關閉

---

## 🚀 後續計畫

### Phase 2: FalconQueue（多節點）

- HTTP RPC 通訊
- 服務發現與註冊
- 負載平衡
- Grafana Dashboard

### Phase 3: Beaver-Raft（共識）

- Raft 選舉
- 日誌複製
- Partial Snapshot

---

**最後更新**：由 AI 協助生成於 2024-01

**使用方式**：

1. 閱讀本文件理解架構
2. 對照各模組的偽代碼註解
3. 依序實作並測試
4. 遇到問題回來查閱

祝實作順利！🎉
