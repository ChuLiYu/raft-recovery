# Phase 1 實作指引

本文件提供實作順序與各檔案的偽代碼位置。

---

## 📁 檔案結構

```text
beaver-raft/
├── cmd/
│   └── queue/
│       └── main.go                      ✅ 已建立（偽代碼）
│
├── internal/
│   ├── cli/
│   │   └── cli.go                       ✅ 已建立（偽代碼）
│   ├── controller/
│   │   └── controller.go                ✅ 已建立（偽代碼）
│   ├── state/
│   │   └── job_manager.go                     ✅ 已建立（偽代碼）
│   ├── wal/
│   │   └── wal.go                       ✅ 已建立（偽代碼）
│   ├── snapshot/
│   │   └── snapshot_manager.go          ✅ 已建立（偽代碼）
│   └── worker/
│       └── worker_pool.go               ✅ 已建立（偽代碼）
│
└── docs/
    └── ai-notes.md                      ✅ 已建立（設計筆記）
```text

---

## 🎯 實作順序（按依賴關係）

### 第 1 步：狀態管理（1-2 天）

**檔案**：`internal/jobmanager/job_manager.go`

**包含**：

- Job, State 結構定義
- Enqueue/PopPending 基本操作
- 狀態轉換（MarkInFlight/MarkCompleted/Requeue）
- Snapshot/Restore 方法
- Validate 不變性檢查

**測試先行**：

```bash
# 先寫測試，再實作
touch internal/jobmanager/job_manager_test.go

# 測試重點
- TestEnqueueDequeue
- TestStateTransitions
- TestInvariant
- TestConcurrency（go test -race）
```text

---

### 第 2 步：WAL 日誌（2-3 天）

**檔案**：`internal/wal/wal.go`

**包含**：

- Event 結構與 CRC32 校驗
- Append 追加事件
- Replay 重放邏輯
- Rotate 日誌旋轉

**測試重點**：

```bash
touch internal/wal/wal_test.go

- TestAppendAndReplay
- TestChecksum（手動破壞檔案測試）
- TestRotate
```text

---

### 第 3 步：快照管理（1-2 天）

**檔案**：`internal/snapshot/snapshot_manager.go`

**包含**：

- Write 原子性寫入（temp + rename）
- Load 載入與驗證
- 版本檢查

**測試重點**：

```bash
touch internal/snapshot/snapshot_test.go

- TestWriteAndLoad
- TestAtomicWrite（關鍵！）
- TestVersionMismatch
```text

---

### 第 4 步：Worker 執行（2-3 天）

**檔案**：`internal/worker/worker_pool.go`

**包含**：

- Worker 結構與 Run() 循環
- Pool 管理（Start/Stop）
- Task/Result 通道
- 超時控制（context.WithTimeout）

**測試重點**：

```bash
touch internal/worker/worker_pool_test.go

- TestWorkerExecution
- TestTimeout
- TestGracefulShutdown
- TestConcurrency
```text

---

### 第 5 步：Controller 核心（3-4 天）

**檔案**：`internal/controller/controller.go`

**包含**：

- loadSnapshot + replayWAL（恢復流程）
- 四個循環：dispatch, result, timeout, snapshot
- 冪等性保證
- EnqueueJobs 公開方法

**測試重點**：

```bash
touch internal/controller/controller_test.go

- TestStartup
- TestCrashRecovery（核心！< 3s）
- TestIdempotency
- TestConcurrency
```text

---

### 第 6 步：CLI 介面（2-3 天）

**檔案**：`internal/cli/cli.go`

**包含**：

- enqueue 命令
- run 命令（訊號處理）
- status 命令
- 配置載入（YAML/環境變數/旗標）

**測試重點**：

```bash
touch internal/cli/cli_test.go

- TestEnqueueCommand
- TestRunCommand
- TestStatusCommand
```text

---

### 第 7 步：入口點（1 天）

**檔案**：`cmd/queue/main.go`

**包含**：

- 呼叫 cli.BuildCLI()
- Panic recovery
- 版本資訊（可選）

**編譯測試**：

```bash
go build -o bin/queue cmd/queue/main.go
./bin/queue --help
```text

---

## 📝 每個檔案的使用方式

### 1. 開啟檔案查看偽代碼

```bash
# 例如查看 job_manager.go
cat internal/jobmanager/job_manager.go

# 你會看到：
# - 職責說明（3-6 行）
# - 資料結構定義（註解形式）
# - 每個方法的偽代碼（包含 Lock 範圍、Error Handling）
# - 3 個 TODO（優先順序）
# - 測試場景建議
```text

### 2. 根據偽代碼手寫實作

```go
// 偽代碼示例（job_manager.go）：
/*
PopPending() *Job:
  【Lock 範圍】mu.Lock() ... mu.Unlock()

  if len(queue) == 0:
    return nil

  job := queue[0]
  queue = queue[1:]
  return &job
*/

// 你的實作：
func (s *State) PopPending() *Job {
    s.mu.Lock()
    defer s.mu.Unlock()

    if len(s.queue) == 0 {
        return nil
    }

    job := s.queue[0]
    s.queue = s.queue[1:]
    return &job
}
```text

### 3. 對照 TODO 優先實作

每個檔案都有 3 個 TODO，按順序實作：

- TODO 1：最基礎功能
- TODO 2：核心邏輯
- TODO 3：進階特性

---

## 🧪 測試驅動開發

### 建議流程

1. **先寫測試**（參考偽代碼中的「測試場景」）
2. **實作最小可用版本**
3. **執行測試**
4. **重構優化**

### 測試指令

```bash
# 單元測試
go test -v ./internal/jobmanager/
go test -v ./internal/wal/
go test -v ./internal/snapshot/
go test -v ./internal/worker/
go test -v ./internal/controller/

# 競爭檢測（必須通過）
go test -race ./...

# 覆蓋率
go test -cover ./...
```text

---

## 🔍 關鍵實作重點

### 1. Lock 使用

- 參考偽代碼中的 **【Lock 範圍】** 註解
- 使用 `defer mu.Unlock()` 確保解鎖
- 避免在鎖內呼叫可能需要鎖的函式（死鎖）

### 2. Error Handling

- 參考偽代碼中的 **【Error Handling】** 註解
- 每個可能失敗的操作都要處理錯誤
- 明確的錯誤訊息（例如：ErrDuplicateJob, ErrNotInFlight）

### 3. 測試場景

- 參考偽代碼中的 **【測試場景】** 註解
- 正常情況 + 邊界情況 + 錯誤情況
- 一定要跑 `go test -race`

---

## 📚 輔助資源

### 已建立的文件

1. **ai-notes.md** - 設計決策與常見問題
2. **phase1-pseudocode.md** - 完整假代碼（備用）
3. **phase1-implementation-guide.md** - 詳細實作指南
4. **phase1-quick-reference.md** - 快速參考手冊

### 實作時查閱順序

1. 先看**該檔案頂部的職責說明**
2. 對照**偽代碼註解**手寫實作
3. 遇到問題查閱 **ai-notes.md**
4. 需要詳細說明看 **phase1-quick-reference.md**

---

## ✅ 實作完成標準

### 每個模組完成時檢查

- [ ] 偽代碼中的所有方法都已實作
- [ ] 3 個 TODO 都已完成
- [ ] 單元測試通過（包含 -race）
- [ ] 測試覆蓋率 > 80%

### 整體完成標準

- [ ] 所有模組完成
- [ ] 整合測試通過
- [ ] 崩潰恢復 < 3s
- [ ] 吞吐量 ≥ 200 jobs/s
- [ ] CLI 可正常使用

---

## 🚀 開始實作

### 第一步

```bash
# 1. 開啟 job_manager.go
code internal/jobmanager/job_manager.go

# 2. 閱讀職責說明與偽代碼

# 3. 建立測試檔案
touch internal/jobmanager/job_manager_test.go

# 4. 先寫第一個測試
# TestEnqueueDequeue

# 5. 實作 Enqueue/PopPending 讓測試通過

# 6. 繼續下一個測試...
```text

---

**提醒**：

- 不要急著一次寫完所有程式碼
- 按照依賴順序，一個模組一個模組完成
- 測試先行，確保每個模組都是可靠的
- 遇到問題隨時回來查閱偽代碼與文件

祝實作順利！💪
