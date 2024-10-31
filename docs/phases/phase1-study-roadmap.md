# Phase 1 學習路線圖

本文件整合所有學習資源，提供清晰的學習路徑。

---

## 📚 文件索引

| 文件                                                                         | 用途               | 適用時機             |
| ---------------------------------------------------------------------------- | ------------------ | -------------------- |
| **[phase1-snapshot-aware-job-queue.md](phase1-snapshot-aware-job-queue.md)** | 官方需求規格       | 開始前閱讀，理解目標 |
| **[phase1-pseudocode.md](phase1-pseudocode.md)**                             | 各模組詳細假代碼   | 實作時參考邏輯       |
| **[phase1-implementation-guide.md](phase1-implementation-guide.md)**         | 逐步實作指南       | 按順序實作時使用     |
| **[phase1-quick-reference.md](phase1-quick-reference.md)**                   | 常見問題與除錯技巧 | 遇到問題時查閱       |
| **本文件**                                                                   | 學習路線總覽       | 規劃學習進度         |

---

## 🎯 學習目標

完成 Phase 1 後，您將掌握：

### 技術能力

- ✅ Go 語言並發程式設計（goroutine、channel、mutex）
- ✅ 系統程式設計（檔案 I/O、fsync、原子操作）
- ✅ 資料結構設計（佇列、狀態機）
- ✅ 測試方法（單元測試、整合測試、競爭檢測）

### 分散式系統概念

- ✅ Write-Ahead Logging（WAL）
- ✅ Checkpointing（快照機制）
- ✅ 崩潰恢復（Crash Recovery）
- ✅ 冪等性（Idempotency）
- ✅ 可觀測性（Observability）

### 實戰經驗

- ✅ 設計並實作完整系統
- ✅ 效能優化與除錯
- ✅ 撰寫可維護的程式碼

---

## 📅 建議學習時程（3 週）

### 第一週：基礎架構 ⭐

#### Day 1-2：環境準備與資料結構

- [ ] 閱讀 `phase1-snapshot-aware-job-queue.md`
- [ ] 設定 Go 開發環境（VS Code + Go 擴充套件）
- [ ] 建立專案目錄結構
- [ ] 更新 `go.mod`，安裝依賴
- [ ] 實作 `pkg/types/types.go`
- [ ] 實作 `internal/job/queue.go`
- [ ] 撰寫 `queue_test.go`

**學習重點**：

- Go 結構體與介面
- `sync.Mutex` 的使用
- 如何設計不變性

**驗證**：

```bash
go test -v internal/job/
go test -race internal/job/
```

---

#### Day 3-4：WAL 機制

- [ ] 閱讀「WAL 假代碼」章節
- [ ] 實作 `internal/storage/wal/wal.go`
- [ ] 撰寫 `wal_test.go`
- [ ] 測試校驗和驗證

**學習重點**：

- 檔案 I/O 與 `file.Sync()`
- CRC32 校驗和
- JSON 編碼/解碼
- 日誌重放邏輯

**驗證**：

```bash
go test -v internal/storage/wal/
```

**實驗**：

```bash
# 寫入測試事件
cat > test-wal.go <<'EOF'
func main() {
    wal := NewWAL("test.log")
    wal.Append("TEST", "job-001")
    wal.Close()
}
EOF

go run test-wal.go
cat test.log | jq .
```

---

#### Day 5-6：快照機制

- [ ] 實作 `internal/storage/snapshot/snapshot.go`
- [ ] 撰寫 `snapshot_test.go`
- [ ] 測試原子性寫入

**學習重點**：

- 原子性寫入模式（temp file + rename）
- JSON 序列化大型結構
- 版本管理

**驗證**：

```bash
go test -v internal/storage/snapshot/
```

**實驗**：

```bash
# 模擬寫入中斷
# 1. 修改 Write() 在 rename 前加 time.Sleep(5s)
# 2. 執行後在 5s 內 Ctrl+C
# 3. 驗證舊快照未損壞
```

---

#### Day 7：週總結與補強

- [ ] 複習本週程式碼
- [ ] 撰寫整合測試（WAL + Snapshot）
- [ ] 閱讀 `phase1-quick-reference.md` 的 FAQ

**練習題**：

1. WAL 和 Snapshot 各適合什麼場景？
2. 如果快照檔案損壞，如何恢復？
3. 為什麼需要 CRC32 校驗？

---

### 第二週：執行與調度 ⭐⭐

#### Day 8-9：Worker 實作

- [ ] 實作 `internal/worker/worker.go`
- [ ] 實作 `internal/worker/pool.go`
- [ ] 撰寫 `worker_test.go`

**學習重點**：

- `context.WithTimeout` 超時控制
- Channel 通訊模式
- Goroutine 管理
- `sync.WaitGroup` 的使用

**驗證**：

```bash
go test -v internal/worker/
go test -race internal/worker/
```

---

#### Day 10-12：Controller 核心

- [ ] 實作 `internal/controller/controller.go`
- [ ] 實作所有循環（dispatch、result、timeout、snapshot）
- [ ] 撰寫 `controller_test.go`

**學習重點**：

- 多 goroutine 協調
- 使用 `select` 處理多通道
- WAL 重放的冪等性
- 狀態轉換邏輯

**關鍵函式實作順序**：

1. `NewController()` + `Start()`
2. `loadSnapshot()` + `replayWAL()`
3. `dispatchLoop()`
4. `resultLoop()` + `handleResult()`
5. `timeoutLoop()`
6. `snapshotLoop()`

**驗證**：

```bash
go test -v internal/controller/
```

---

#### Day 13-14：週總結與整合測試

- [ ] 撰寫端到端測試
- [ ] 測試崩潰恢復場景
- [ ] 效能測試（1000 個任務）

**測試清單**：

```go
func TestBasicFlow(t *testing.T)        // 基本流程
func TestCrashRecovery(t *testing.T)    // 崩潰恢復
func TestTimeout(t *testing.T)          // 超時處理
func TestRetry(t *testing.T)            // 重試邏輯
func TestIdempotency(t *testing.T)      // 冪等性
func TestConcurrency(t *testing.T)      // 並發安全
```

---

### 第三週：完善與示範 ⭐⭐⭐

#### Day 15-16：監控與 CLI

- [ ] 實作 `internal/metrics/metrics.go`
- [ ] 實作 `cmd/queue/main.go`（Cobra CLI）
- [ ] 測試 Prometheus 指標

**學習重點**：

- Prometheus client 使用
- Cobra 命令列框架
- 訊號處理（SIGINT/SIGTERM）
- YAML 配置讀取

**驗證**：

```bash
go build -o bin/queue cmd/queue/main.go
./bin/queue --help
./bin/queue run --workers 4
curl http://localhost:9090/metrics
```

---

#### Day 17-18：示範腳本與文件

- [ ] 建立 `scripts/demo.sh`
- [ ] 建立 `Makefile`
- [ ] 撰寫測試任務 JSON
- [ ] 建立 `configs/default.yaml`

**Demo 腳本應展示**：

1. 系統啟動
2. 加入任務
3. 部分任務完成
4. 模擬崩潰（kill -9）
5. 自動恢復
6. 最終狀態驗證

**驗證**：

```bash
make demo
```

---

#### Day 19-20：效能調校與測試

- [ ] 執行負載測試（10,000 個任務）
- [ ] 驗證吞吐量 ≥ 200 jobs/s
- [ ] 驗證恢復時間 < 3s
- [ ] 執行混沌測試
- [ ] 修復所有 race condition

**效能測試**：

```bash
# 吞吐量測試
go test -bench=BenchmarkThroughput -benchtime=10s

# 恢復時間測試
go test -run=TestRecoveryTime -count=10

# 競爭檢測
go test -race ./...
```

---

#### Day 21：總結與展示準備

- [ ] 更新 README.md
- [ ] 加入架構圖（使用 Mermaid）
- [ ] 撰寫效能報告
- [ ] 錄製示範影片（可選）
- [ ] 準備技術分享簡報

**README 應包含**：

1. 專案簡介
2. 架構圖
3. 快速開始
4. 效能指標
5. 技術亮點

---

## ✅ 實作檢查清單

### 核心功能

- [ ] 任務可以加入佇列
- [ ] Worker 並發執行任務
- [ ] 任務完成後正確更新狀態
- [ ] 失敗任務自動重試
- [ ] 超時任務重新排隊
- [ ] 超過重試次數進入死信佇列

### 持久化

- [ ] WAL 正確記錄所有事件
- [ ] WAL 校驗和驗證有效
- [ ] 快照正確保存狀態
- [ ] 原子寫入防止損壞
- [ ] 定時快照與 WAL 旋轉

### 崩潰恢復

- [ ] 載入快照恢復基礎狀態
- [ ] 重放 WAL 恢復增量狀態
- [ ] 恢復時間 < 3 秒
- [ ] 重放具有冪等性
- [ ] 無任務重複執行

### 效能

- [ ] 吞吐量 ≥ 200 jobs/s（1000 任務）
- [ ] 通過 `go test -race` 檢測
- [ ] 無 goroutine 洩漏
- [ ] 記憶體使用穩定

### 監控

- [ ] Prometheus 指標正確暴露
- [ ] 可查看吞吐量、延遲、重試率
- [ ] 可查看佇列深度
- [ ] 記錄恢復時間

### 測試

- [ ] 單元測試覆蓋率 > 80%
- [ ] 整合測試涵蓋主要場景
- [ ] 混沌測試驗證容錯性
- [ ] 所有測試通過

---

## 🎓 學習評估

### 知識檢測

完成後，您應該能回答：

#### 基礎問題

1. Go 的 `sync.Mutex` 和 `sync.RWMutex` 有什麼區別？
2. `defer` 的執行順序是什麼？
3. Channel 的緩衝與非緩衝有何差異？
4. `context.Context` 的作用是什麼？

#### 進階問題

5. WAL 的 `fsync()` 為何重要？不做會有什麼風險？
6. 快照的原子性寫入如何實現？為何重要？
7. 如何確保 WAL 重放的冪等性？
8. 為什麼要用 CRC32 校驗和？

#### 系統設計問題

9. 如果要支援 100 萬個任務，需要哪些優化？
10. 如何擴展到多節點分散式系統？
11. 與 Kafka、RabbitMQ 相比，本系統的優劣？
12. 如果要實作任務優先級，如何設計？

---

## 📊 效能基準

### 目標 KPI

| 指標     | 目標值       | 測量方法             |
| -------- | ------------ | -------------------- |
| 恢復時間 | < 3s         | 載入快照 + 重放 WAL  |
| 吞吐量   | ≥ 200 jobs/s | 1000 個任務總時間    |
| P95 延遲 | < 100ms      | Prometheus histogram |
| 記憶體   | < 100MB      | 穩定運行時           |

### 測試環境

- CPU: 4 核心
- RAM: 8GB
- 磁碟: SSD
- Workers: 8 個

---

## 🔗 外部資源

### Go 語言學習

- [A Tour of Go](https://go.dev/tour/) - 官方互動教學
- [Effective Go](https://go.dev/doc/effective_go) - 最佳實踐
- [Go by Example](https://gobyexample.com/) - 範例學習
- [Go Concurrency Patterns](https://go.dev/blog/pipelines) - 並發模式

### 分散式系統

- [Designing Data-Intensive Applications](https://dataintensive.net/) - 第 3 章
- [Raft 論文](https://raft.github.io/raft.pdf) - 了解背景
- [Write-Ahead Logging - Wikipedia](https://en.wikipedia.org/wiki/Write-ahead_logging)

### 開源專案參考

- [etcd/wal](https://github.com/etcd-io/etcd/tree/main/server/storage/wal) - WAL 實作
- [BadgerDB](https://github.com/dgraph-io/badger) - LSM-tree 資料庫
- [NSQ](https://github.com/nsqio/nsq) - 分散式訊息佇列

---

## 🚀 完成後的成就

完成 Phase 1 後，您將擁有：

### 可展示的專案

- ✅ GitHub 倉庫（含完整程式碼）
- ✅ 詳細的 README 與架構圖
- ✅ 示範影片或 GIF
- ✅ 效能測試報告

### 技術深度

- ✅ 理解 WAL 與 Checkpoint 機制
- ✅ 掌握 Go 並發程式設計
- ✅ 實作過完整的崩潰恢復系統
- ✅ 懂得如何測試與除錯分散式系統

### 面試優勢

- ✅ 可以深入討論系統設計
- ✅ 展示程式碼品質與測試意識
- ✅ 證明對分散式系統的理解
- ✅ 體現自主學習與解決問題的能力

---

## 🎯 進階方向

完成 Phase 1 後，可以：

### 優化 Phase 1

1. **效能優化**：

   - 實作 WAL 批次寫入
   - 使用 `sync.RWMutex` 優化讀取
   - 實作任務優先級佇列

2. **功能擴展**：

   - 支援任務依賴（DAG）
   - 實作延遲任務（Scheduled Jobs）
   - 加入任務取消功能

3. **監控增強**：
   - 建立 Grafana Dashboard
   - 加入分散式追蹤（Jaeger）
   - 實作告警規則

### 進入 Phase 2

開始學習：

- HTTP RPC 通訊
- 服務發現與註冊
- 多節點部署
- 負載平衡

### 深入研究

閱讀論文：

- Raft Consensus Algorithm
- Paxos Made Simple
- The Chubby Lock Service

---

## 📝 學習日誌建議

建立學習日誌記錄：

```markdown
# Day 1: 2024-01-01

## 完成內容

- 建立專案結構
- 實作 types.go
- 開始實作 queue.go

## 學習到的

- Go 的 struct tag 用法
- sync.Mutex 的基本使用

## 遇到的問題

- Q: 如何深拷貝 slice？
- A: 使用 append 或 copy

## 明天計畫

- 完成 queue.go
- 撰寫測試
```

---

## 🏆 結語

Phase 1 是整個 Beaver-Raft 計畫的基石，看似簡單卻包含了分散式系統的核心概念。

**不要著急**：

- 每個概念都花時間理解
- 多看假代碼，理解設計意圖
- 遇到問題先查文件，再實驗

**保持好奇**：

- 問「為什麼」：為什麼這樣設計？
- 做實驗：改改看會怎樣？
- 延伸思考：如果場景變了呢？

**享受過程**：

- 實作系統的樂趣
- 解決問題的成就感
- 掌握知識的滿足感

---

**準備好了嗎？讓我們開始吧！** 🚀

從 `pkg/types/types.go` 開始，一步一步建構您的分散式系統基礎！

祝您學習順利，有任何問題隨時回來查閱這些文件！💪
