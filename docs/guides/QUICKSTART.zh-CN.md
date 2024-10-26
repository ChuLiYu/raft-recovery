# 快速開始指南

本文件幫助您快速理解專案結構並開始實作。

---

## ✅ 已建立的檔案

### 核心模組（含偽代碼註解）

```
✓ cmd/queue/main.go                    - CLI 入口點
✓ internal/cli/cli.go                  - 命令列介面（enqueue/run/status）
✓ internal/controller/controller.go    - 核心調度器（四個循環）
✓ internal/jobmanager/job_manager.go             - 佇列狀態管理
✓ internal/wal/wal.go                 - Write-Ahead Log
✓ internal/snapshot/snapshot_manager.go - 快照管理
✓ internal/worker/worker_pool.go       - Worker 執行池
```

### 文件資源

```
✓ docs/ai-notes.md                     - AI 設計筆記（必讀！）
✓ IMPLEMENTATION.md                     - 實作指引（本文件）
✓ docs/phase1-pseudocode.md            - 完整假代碼（備用）
✓ docs/phase1-quick-reference.md       - 快速參考手冊
```

---

## 📖 每個檔案的結構

所有 `.go` 檔案都包含：

### 1️⃣ 職責說明（3-6 行）

```go
// ============================================================================
// 職責說明：
// 1. 維護「每個任務只存在於一個集合」的不變性
// 2. 提供狀態轉換方法（Enqueue -> InFlight -> Completed/Dead）
// 3. 支援快照序列化與反序列化
// ============================================================================
```

### 2️⃣ 偽代碼註解（含流程、Lock、Error Handling）

```go
/*
PopPending() *Job:
  【Lock 範圍】mu.Lock() ... mu.Unlock()

  if len(queue) == 0:
    return nil

  job := queue[0]
  queue = queue[1:]
  return &job

  【測試場景】
    - 空佇列回傳 nil
    - FIFO 順序正確
*/
```

### 3️⃣ TODO（實作優先順序）

```go
// ============================================================================
// TODO（實作優先順序）
// ============================================================================

// TODO 1: 實作基礎資料結構與 Enqueue/PopPending（先讓佇列運作）
// TODO 2: 實作狀態轉換方法（MarkInFlight/MarkCompleted/Requeue）
// TODO 3: 實作 Snapshot/Restore 與 Validate（確保持久化與不變性）
```

---

## 🎯 實作方式

### 方法 1：跟著偽代碼實作（推薦）

1. **開啟檔案**

```bash
code internal/jobmanager/job_manager.go
```

2. **閱讀職責說明**（檔案頂部）

3. **查看偽代碼註解**

   - 每個方法都有詳細流程
   - 標註了 Lock 範圍
   - 指出 Error Handling 點

4. **根據偽代碼手寫實作**

```go
// 看到偽代碼：
/*
PopPending() *Job:
  【Lock 範圍】mu.Lock() ... mu.Unlock()
  if len(queue) == 0:
    return nil
  ...
*/

// 你寫實作：
func (jm *JobManager) PopPending() *Job {
    s.mu.Lock()
    defer s.mu.Unlock()

    if len(s.queue) == 0 {
        return nil
    }
    // ...
}
```

5. **對照 TODO 順序**
   - 先做 TODO 1（最基礎）
   - 再做 TODO 2（核心邏輯）
   - 最後 TODO 3（進階功能）

---

### 方法 2：測試驅動開發（TDD）

1. **建立測試檔案**

```bash
touch internal/jobmanager/job_manager_test.go
```

2. **根據偽代碼中的「測試場景」寫測試**

```go
// 偽代碼中建議的測試場景：
/*
TestEnqueueDequeue:
  - 加入 10 個任務
  - 依序彈出，驗證 FIFO
  - 彈空後回傳 nil
*/

// 你寫測試：
func TestEnqueueDequeue(t *testing.T) {
    jobManager := jobmanager.NewJobManager()

    // 加入 10 個任務
    for i := 0; i < 10; i++ {
        jobManager.Enqueue(Job{ID: fmt.Sprintf("task-%d", i)})
    }

    // 依序彈出
    for i := 0; i < 10; i++ {
        job := jobManager.PopPending()
        assert.Equal(t, fmt.Sprintf("task-%d", i), job.ID)
    }

    // 彈空後回傳 nil
    assert.Nil(t, jobManager.PopPending())
}
```

3. **實作讓測試通過**

4. **重複**：下一個測試 → 實作 → 通過

---

## 🔢 實作順序

### Week 1：基礎層

1. **Day 1-2**: `internal/jobmanager/job_manager.go`

   - 佇列狀態管理
   - 測試不變性

2. **Day 3-4**: `internal/wal/wal.go`

   - 日誌追加與重放
   - CRC32 校驗

3. **Day 5-6**: `internal/snapshot/snapshot_manager.go`
   - 快照序列化
   - 原子性寫入

### Week 2：執行層

4. **Day 8-9**: `internal/worker/worker_pool.go`

   - Worker 執行
   - 超時控制

5. **Day 10-12**: `internal/controller/controller.go`
   - 四個循環
   - 崩潰恢復

### Week 3：介面層

6. **Day 15-16**: `internal/cli/cli.go`

   - 命令列介面
   - 配置管理

7. **Day 17**: `cmd/queue/main.go`
   - 入口點
   - 編譯測試

---

## 🧪 測試指令

### 開發過程

```bash
# 單一模組測試
go test -v ./internal/jobmanager/

# 監聽模式（自動重跑）
# 需安裝 watch: brew install watch
watch -n 1 go test ./internal/jobmanager/

# 競爭檢測（必須通過）
go test -race ./internal/jobmanager/
```

### 完成後

```bash
# 所有測試
go test -v ./...

# 競爭檢測（整體）
go test -race ./...

# 覆蓋率
go test -cover ./...

# 覆蓋率報告
go test -coverprofile=coverage.out ./...
go tool cover -html=coverage.out
```

---

## 📚 遇到問題查閱順序

### 1. 先看該檔案的偽代碼註解

- 每個方法都有詳細說明
- Lock 範圍、Error Handling 都標明了

### 2. 再看 docs/ai-notes.md

- 設計決策理由
- 常見問題 FAQ
- 測試策略

### 3. 查閱 docs/phase1-quick-reference.md

- 技術細節
- 除錯技巧
- 效能優化

### 4. 參考 docs/phase1-pseudocode.md

- 更完整的假代碼
- 各模組詳細說明

---

## 💡 關鍵提醒

### ✅ 務必做到

1. **每個方法都參考偽代碼註解**
2. **Lock 範圍嚴格按照註解標示**
3. **Error Handling 不要跳過**
4. **測試場景都要涵蓋**
5. **執行 `go test -race` 確保無競爭**

### ❌ 避免

1. 不要跳過測試直接寫實作
2. 不要忽略偽代碼中的 Lock 範圍
3. 不要省略錯誤處理
4. 不要一次寫完所有程式碼（模組化進行）

---

## 🎯 第一步行動

### 現在就開始！

1. **開啟第一個檔案**

```bash
code internal/jobmanager/job_manager.go
```

2. **閱讀頂部職責說明**（了解這個模組做什麼）

3. **建立測試檔案**

```bash
touch internal/jobmanager/job_manager_test.go
```

4. **寫第一個測試**（TestEnqueueDequeue）

5. **實作 NewJobManager/Enqueue/PopPending**

6. **跑測試**

```bash
go test -v ./internal/jobmanager/
```

7. **通過後繼續下一個測試**

---

## 📊 進度追蹤

建議建立一個檢查清單：

```markdown
## 模組完成進度

- [ ] internal/jobmanager/job_manager.go

  - [ ] TODO 1: 基礎操作
  - [ ] TODO 2: 狀態轉換
  - [ ] TODO 3: Snapshot/Validate
  - [ ] 測試通過（-race）

- [ ] internal/wal/wal.go
  - [ ] TODO 1: Append 與寫入
  - [ ] TODO 2: Replay 與校驗
  - [ ] TODO 3: Rotate
  - [ ] 測試通過（-race）

...（以此類推）
```

---

## 🚀 期望成果

完成後您將擁有：

✅ **可運行的系統**

```bash
./bin/queue run --workers 8
./bin/queue enqueue --file jobs.json
./bin/queue status
```

✅ **完整測試覆蓋**

- 單元測試 > 80% 覆蓋率
- 整合測試（崩潰恢復）
- 通過競爭檢測

✅ **效能達標**

- 恢復時間 < 3s
- 吞吐量 ≥ 200 jobs/s

✅ **深入理解**

- WAL 與 Checkpoint 機制
- Go 並發程式設計
- 崩潰恢復原理

---

**準備好了嗎？開始實作吧！** 🎉

有任何問題，隨時回來查閱這些偽代碼註解和文件。

祝實作順利！💪
