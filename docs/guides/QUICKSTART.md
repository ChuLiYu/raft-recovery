# Quick Start Guide# 快速開始指南



**English** | **[中文版](QUICKSTART.zh-CN.md)**本文件幫助您快速理解專案結構並開始實作。



> Understand project structure and start implementing---



This guide helps developers understand the codebase structure and begin contributing to Beaver-Raft.## ✅ 已建立的檔案



---### 核心模組（含偽代碼註解）



## ✅ Project Structure```

✓ cmd/queue/main.go                    - CLI 入口點

### Core Modules (with implementation)✓ internal/cli/cli.go                  - 命令列介面（enqueue/run/status）

✓ internal/controller/controller.go    - 核心調度器（四個循環）

```text✓ internal/jobmanager/job_manager.go             - 佇列狀態管理

✓ cmd/queue/main.go                    - CLI entry point✓ internal/wal/wal.go                 - Write-Ahead Log

✓ internal/cli/cli.go                  - Command-line interface✓ internal/snapshot/snapshot_manager.go - 快照管理

✓ internal/controller/controller.go    - Core orchestrator (4 loops)✓ internal/worker/worker_pool.go       - Worker 執行池

✓ internal/jobmanager/job_manager.go   - Job queue & state management```

✓ internal/storage/wal/wal.go          - Write-Ahead Log

✓ internal/snapshot/snapshot_manager.go - Snapshot management### 文件資源

✓ internal/worker/worker_pool.go       - Worker pool execution

✓ internal/metrics/metrics.go          - Prometheus metrics```

```✓ docs/ai-notes.md                     - AI 設計筆記（必讀！）

✓ IMPLEMENTATION.md                     - 實作指引（本文件）

### Documentation✓ docs/phase1-pseudocode.md            - 完整假代碼（備用）

✓ docs/phase1-quick-reference.md       - 快速參考手冊

```text```

✓ docs/ai-notes.md                     - Design decisions (must-read!)

✓ IMPLEMENTATION_ORDER.md              - Step-by-step implementation---

✓ docs/phase1-architecture.md          - Architecture design

✓ docs/phase1-quick-reference.md       - Quick reference## 📖 每個檔案的結構

```

所有 `.go` 檔案都包含：

---

### 1️⃣ 職責說明（3-6 行）

## 📖 File Structure

```go

All `.go` files follow a consistent structure:// ============================================================================

// 職責說明：

### 1️⃣ Responsibility Statement (3-6 lines)// 1. 維護「每個任務只存在於一個集合」的不變性

// 2. 提供狀態轉換方法（Enqueue -> InFlight -> Completed/Dead）

```go// 3. 支援快照序列化與反序列化

// ============================================================================// ============================================================================

// Responsibilities:```

// 1. Maintain job state invariants (each job in exactly one collection)

// 2. Provide state transition methods (PENDING → IN_FLIGHT → COMPLETED/FAILED)### 2️⃣ 偽代碼註解（含流程、Lock、Error Handling）

// 3. Support snapshot serialization/deserialization

// ============================================================================```go

```/*

PopPending() *Job:

### 2️⃣ Implementation Comments (flow, locks, error handling)  【Lock 範圍】mu.Lock() ... mu.Unlock()



```go  if len(queue) == 0:

/*    return nil

PopPending() *Job:

  1. Lock mutex  job := queue[0]

  2. If pending queue empty, return nil  queue = queue[1:]

  3. Pop first job, add to in-flight map  return &job

  4. Unlock, return job

*/  【測試場景】

```    - 空佇列回傳 nil

    - FIFO 順序正確

### 3️⃣ Data Structures*/

```

```go

type JobManager struct {### 3️⃣ TODO（實作優先順序）

    pending   []*Job            // Pending job queue

    inFlight  map[JobID]*Job    // In-flight jobs```go

    completed map[JobID]bool    // Completed jobs// ============================================================================

    mu        sync.RWMutex      // Protects all fields// TODO（實作優先順序）

}// ============================================================================

```

// TODO 1: 實作基礎資料結構與 Enqueue/PopPending（先讓佇列運作）

### 4️⃣ Implementation// TODO 2: 實作狀態轉換方法（MarkInFlight/MarkCompleted/Requeue）

// TODO 3: 實作 Snapshot/Restore 與 Validate（確保持久化與不變性）

```go```

func (jm *JobManager) PopPending() *Job {

    jm.mu.Lock()---

    defer jm.mu.Unlock()

    ## 🎯 實作方式

    if len(jm.pending) == 0 {

        return nil### 方法 1：跟著偽代碼實作（推薦）

    }

    1. **開啟檔案**

    job := jm.pending[0]

    jm.pending = jm.pending[1:]```bash

    jm.inFlight[job.ID] = jobcode internal/jobmanager/job_manager.go

    return job```

}

```2. **閱讀職責說明**（檔案頂部）



---3. **查看偽代碼註解**



## 🎯 Implementation Priority   - 每個方法都有詳細流程

   - 標註了 Lock 範圍

### Phase 1: Core Functionality (Week 1-2)   - 指出 Error Handling 點



**Day 1-3**: Foundation4. **根據偽代碼手寫實作**

- [x] Types & data structures (`pkg/types/`)

- [x] JobManager state machine```go

- [x] Worker pool implementation// 看到偽代碼：

/*

**Day 4-7**: PersistencePopPending() *Job:

- [x] WAL implementation  【Lock 範圍】mu.Lock() ... mu.Unlock()

- [x] Snapshot manager  if len(queue) == 0:

- [x] Recovery logic    return nil

  ...

**Day 8-14**: Integration*/

- [x] Controller with 4 loops

- [x] CLI interface// 你寫實作：

- [x] End-to-end testingfunc (jm *JobManager) PopPending() *Job {

    s.mu.Lock()

### Phase 2: Production-Ready (Week 3)    defer s.mu.Unlock()



**Day 15-17**: Observability    if len(s.queue) == 0 {

- [x] Prometheus metrics        return nil

- [x] Performance tuning    }

- [x] Documentation    // ...

}

**Day 18-21**: Polish```

- [x] Integration tests

- [x] Demo script5. **對照 TODO 順序**

- [x] Bug fixes   - 先做 TODO 1（最基礎）

   - 再做 TODO 2（核心邏輯）

---   - 最後 TODO 3（進階功能）



## 🔧 Development Workflow---



### 1. Setup Environment### 方法 2：測試驅動開發（TDD）



```bash1. **建立測試檔案**

# Clone repository

git clone https://github.com/ChuLiYu/raft-recovery.git```bash

cd raft-recoverytouch internal/jobmanager/job_manager_test.go

```

# Install dependencies

go mod download2. **根據偽代碼中的「測試場景」寫測試**



# Run tests```go

make test// 偽代碼中建議的測試場景：

```/*

TestEnqueueDequeue:

### 2. Understanding the Code  - 加入 10 個任務

  - 依序彈出，驗證 FIFO

**Recommended Reading Order**:  - 彈空後回傳 nil

*/

1. `docs/ai-notes.md` - Understand design decisions

2. `docs/phase1-architecture.md` - System architecture// 你寫測試：

3. `pkg/types/types.go` - Core data structuresfunc TestEnqueueDequeue(t *testing.T) {

4. `internal/jobmanager/job_manager.go` - State management    jobManager := jobmanager.NewJobManager()

5. `internal/controller/controller.go` - Main orchestration

    // 加入 10 個任務

### 3. Making Changes    for i := 0; i < 10; i++ {

        jobManager.Enqueue(Job{ID: fmt.Sprintf("task-%d", i)})

```bash    }

# Create feature branch

git checkout -b feature/my-feature    // 依序彈出

    for i := 0; i < 10; i++ {

# Make changes        job := jobManager.PopPending()

vim internal/module/file.go        assert.Equal(t, fmt.Sprintf("task-%d", i), job.ID)

    }

# Run tests

go test ./internal/module/    // 彈空後回傳 nil

    assert.Nil(t, jobManager.PopPending())

# Run race detector}

go test -race ./...```



# Commit3. **實作讓測試通過**

git commit -m "feat: add new feature"

```4. **重複**：下一個測試 → 實作 → 通過



### 4. Testing---



```bash## 🔢 實作順序

# Unit tests

go test ./internal/...### Week 1：基礎層



# Specific module1. **Day 1-2**: `internal/jobmanager/job_manager.go`

go test -v ./internal/controller/

   - 佇列狀態管理

# With coverage   - 測試不變性

go test -cover ./...

2. **Day 3-4**: `internal/wal/wal.go`

# Integration tests

go test ./test/integration/...   - 日誌追加與重放

   - CRC32 校驗

# Benchmarks

go test -bench=. ./...3. **Day 5-6**: `internal/snapshot/snapshot_manager.go`

```   - 快照序列化

   - 原子性寫入

---

### Week 2：執行層

## 📚 Key Modules Explained

4. **Day 8-9**: `internal/worker/worker_pool.go`

### JobManager (`internal/jobmanager/`)

   - Worker 執行

**Purpose**: State machine for job lifecycle   - 超時控制



**Key Methods**:5. **Day 10-12**: `internal/controller/controller.go`

- `Enqueue(job)` - Add to pending queue   - 四個循環

- `PopPending()` - Get next job   - 崩潰恢復

- `MarkInFlight(jobID, workerID, deadline)` - Job dispatched

- `MarkCompleted(jobID)` - Job succeeded### Week 3：介面層

- `MarkFailed(jobID)` - Job failed

- `GetTimeouts()` - Find timed-out jobs6. **Day 15-16**: `internal/cli/cli.go`



**State Transitions**:   - 命令列介面

```text   - 配置管理

PENDING → IN_FLIGHT → COMPLETED

              ↓7. **Day 17**: `cmd/queue/main.go`

            FAILED   - 入口點

```   - 編譯測試



### Controller (`internal/controller/`)---



**Purpose**: Orchestrates system components## 🧪 測試指令



**Four Main Loops**:### 開發過程



1. **Dispatch Loop**: Get pending jobs → Log to WAL → Send to workers```bash

2. **Result Loop**: Collect worker results → Log to WAL → Update state# 單一模組測試

3. **Timeout Loop**: Check timeouts → Retry or mark failedgo test -v ./internal/jobmanager/

4. **Snapshot Loop**: Periodic full state snapshots

# 監聽模式（自動重跑）

### WAL (`internal/storage/wal/`)# 需安裝 watch: brew install watch

watch -n 1 go test ./internal/jobmanager/

**Purpose**: Durability through operation logging

# 競爭檢測（必須通過）

**Key Operations**:go test -race ./internal/jobmanager/

- `Append(event)` - Write event with fsync```

- `Replay(handler)` - Replay all events

- `Rotate()` - Start new log file### 完成後



**Event Types**:```bash

- `DISPATCH` - Job sent to worker# 所有測試

- `ACK` - Job completedgo test -v ./...

- `FAIL` - Job failed

- `RETRY` - Job retry# 競爭檢測（整體）

go test -race ./...

### Worker Pool (`internal/worker/`)

# 覆蓋率

**Purpose**: Concurrent job executiongo test -cover ./...



**Architecture**:# 覆蓋率報告

```textgo test -coverprofile=coverage.out ./...

Poolgo tool cover -html=coverage.out

 ├─ Worker 1 (goroutine)```

 ├─ Worker 2 (goroutine)

 ├─ Worker 3 (goroutine)---

 └─ Worker N (goroutine)

```## 📚 遇到問題查閱順序



**Key Features**:### 1. 先看該檔案的偽代碼註解

- Fixed-size goroutine pool

- Task distribution via channels- 每個方法都有詳細說明

- Timeout handling with context- Lock 範圍、Error Handling 都標明了

- Graceful shutdown

### 2. 再看 docs/ai-notes.md

---

- 設計決策理由

## 🧪 Testing Strategy- 常見問題 FAQ

- 測試策略

### Unit Tests

### 3. 查閱 docs/phase1-quick-reference.md

Each module has comprehensive unit tests:

- 技術細節

```bash- 除錯技巧

# Run all unit tests- 效能優化

go test ./internal/...

### 4. 參考 docs/phase1-pseudocode.md

# Specific module

go test ./internal/jobmanager/- 更完整的假代碼

- 各模組詳細說明

# With verbosity

go test -v ./internal/controller/---

```

## 💡 關鍵提醒

### Integration Tests

### ✅ 務必做到

End-to-end scenarios in `test/integration/`:

1. **每個方法都參考偽代碼註解**

```bash2. **Lock 範圍嚴格按照註解標示**

# Run integration tests3. **Error Handling 不要跳過**

go test ./test/integration/...4. **測試場景都要涵蓋**

5. **執行 `go test -race` 確保無競爭**

# Specific test

go test -v ./test/integration/ -run TestRecovery### ❌ 避免

```

1. 不要跳過測試直接寫實作

### Race Detection2. 不要忽略偽代碼中的 Lock 範圍

3. 不要省略錯誤處理

Always run race detector:4. 不要一次寫完所有程式碼（模組化進行）



```bash---

go test -race ./...

```## 🎯 第一步行動



---### 現在就開始！



## 🚀 Running the System1. **開啟第一個檔案**



### Build```bash

code internal/jobmanager/job_manager.go

```bash```

make build

```2. **閱讀頂部職責說明**（了解這個模組做什麼）



### Run Server3. **建立測試檔案**



```bash```bash

./bin/beaver-raft run --workers 8touch internal/jobmanager/job_manager_test.go

``````



### Submit Jobs4. **寫第一個測試**（TestEnqueueDequeue）



```bash5. **實作 NewJobManager/Enqueue/PopPending**

./bin/beaver-raft enqueue --file test/jobs.json

```6. **跑測試**



### Check Status```bash

go test -v ./internal/jobmanager/

```bash```

./bin/beaver-raft status

```7. **通過後繼續下一個測試**



### View Metrics---



```bash## 📊 進度追蹤

curl http://localhost:9090/metrics

```建議建立一個檢查清單：



### Demo (All-in-One)```markdown

## 模組完成進度

```bash

make demo- [ ] internal/jobmanager/job_manager.go

```

  - [ ] TODO 1: 基礎操作

---  - [ ] TODO 2: 狀態轉換

  - [ ] TODO 3: Snapshot/Validate

## 🐛 Debugging Tips  - [ ] 測試通過（-race）



### Enable Verbose Logging- [ ] internal/wal/wal.go

  - [ ] TODO 1: Append 與寫入

```go  - [ ] TODO 2: Replay 與校驗

log.SetLevel(log.DebugLevel)  - [ ] TODO 3: Rotate

```  - [ ] 測試通過（-race）



### Check WAL Contents...（以此類推）

```

```bash

cat data/wal/wal-*.log | jq '.'---

```

## 🚀 期望成果

### Inspect Snapshot

完成後您將擁有：

```bash

cat data/snapshot/snapshot.json | jq '.'✅ **可運行的系統**

```

```bash

### Monitor Goroutines./bin/queue run --workers 8

./bin/queue enqueue --file jobs.json

```bash./bin/queue status

curl http://localhost:6060/debug/pprof/goroutine```

```

✅ **完整測試覆蓋**

---

- 單元測試 > 80% 覆蓋率

## 📖 Additional Resources- 整合測試（崩潰恢復）

- 通過競爭檢測

| Resource | Purpose |

|----------|---------|✅ **效能達標**

| [USAGE_GUIDE.md](USAGE_GUIDE.md) | End-user documentation |

| [PHASE1_SUMMARY.md](PHASE1_SUMMARY.md) | Feature summary |- 恢復時間 < 3s

| [IMPLEMENTATION_ORDER.md](IMPLEMENTATION_ORDER.md) | Module implementation order |- 吞吐量 ≥ 200 jobs/s

| [TEST_COVERAGE_REPORT.md](TEST_COVERAGE_REPORT.md) | Test coverage details |

| [docs/phase1-architecture.md](docs/phase1-architecture.md) | Detailed architecture |✅ **深入理解**



---- WAL 與 Checkpoint 機制

- Go 並發程式設計

## 🤝 Contributing- 崩潰恢復原理



1. Read design docs first---

2. Follow existing code style

3. Add tests for new features**準備好了嗎？開始實作吧！** 🎉

4. Run `make test` before committing

5. Update documentation有任何問題，隨時回來查閱這些偽代碼註解和文件。



---祝實作順利！💪


## 💡 Common Questions

**Q: Where do I start?**
A: Read `docs/ai-notes.md`, then explore `internal/jobmanager/`

**Q: How do I add a new feature?**
A: Follow the pattern in existing modules, add tests first (TDD)

**Q: Tests are failing?**
A: Run `go test -v ./...` to see detailed output

**Q: How to debug race conditions?**
A: Use `go test -race ./...` and review mutex usage

**Q: Where's the entry point?**
A: `cmd/queue/main.go` → `internal/cli/cli.go` → `internal/controller/controller.go`

---

**Happy Coding!** 🦫

For detailed Chinese version, see [QUICKSTART.zh-CN.md](QUICKSTART.zh-CN.md)
