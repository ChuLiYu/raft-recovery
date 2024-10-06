# WAL 模組快速開始指南

## 🎯 目標

這份指南將帶您一步步實作 WAL 模組，從最簡單的功能開始，逐步建構完整的系統。

---

## 📅 5 天實作計畫

### Day 1：基礎建設 ✨

**目標**：能夠建立 WAL 並寫入第一個事件

#### Step 1: 實作型別定義（30 分鐘）

**檔案**：`types.go`

```go
// 1. 移除假代碼註解
// 2. 實作 Event 結構（已定義好）
// 3. 實作 EventType 常數（已定義好）
// 4. 思考：Event 是否需要更多欄位？
```

**驗證**：

```bash
go build ./internal/storage/wal
```

#### Step 2: 實作校驗和（30 分鐘）

**檔案**：`checksum.go`

```go
func CalculateChecksum(eventType EventType, jobID string, seq uint64) uint32 {
    // TODO: 組合字串
    data := string(eventType) + jobID + strconv.FormatUint(seq, 10)

    // TODO: 計算 CRC32
    return crc32.ChecksumIEEE([]byte(data))
}

func VerifyChecksum(event Event) bool {
    // TODO: 重新計算並比較
    expected := CalculateChecksum(event.Type, event.JobID, event.Seq)
    return event.Checksum == expected
}
```

**驗證**：

```bash
go test -run TestCalculateChecksum
```

#### Step 3: 實作錯誤定義（15 分鐘）

**檔案**：`errors.go`

```go
// 1. 完成 ChecksumError.Error()
func (e *ChecksumError) Error() string {
    return fmt.Sprintf("wal: checksum mismatch at seq=%d (expected=%08x, got=%08x)",
        e.Seq, e.Expected, e.Actual)
}

// 2. 完成 CorruptionError.Error()
func (e *CorruptionError) Error() string {
    return fmt.Sprintf("wal: corrupted at seq=%d offset=%d: %v",
        e.Seq, e.Offset, e.Cause)
}
```

#### Step 4: 實作 WAL 基礎（2 小時）

**檔案**：`wal.go`

```go
// 只實作最基本的功能
func NewWAL(path string) (*WAL, error) {
    // TODO: 開啟檔案（O_CREATE|O_APPEND|O_RDWR）
    file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
    if err != nil {
        return nil, err
    }

    // TODO: 建立 encoder
    encoder := json.NewEncoder(file)

    // 暫時不處理已存在檔案的 seq 讀取（Phase 2 再做）
    return &WAL{
        file:    file,
        encoder: encoder,
        path:    path,
        seq:     0,
    }, nil
}

func (w *WAL) Append(eventType EventType, jobID string) error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // TODO: seq++
    w.seq++

    // TODO: 建立 event
    event := Event{
        Seq:       w.seq,
        Type:      eventType,
        JobID:     jobID,
        Timestamp: time.Now().UnixMilli(),
        Checksum:  CalculateChecksum(eventType, jobID, w.seq),
    }

    // TODO: 寫入
    if err := w.encoder.Encode(event); err != nil {
        return err
    }

    // TODO: Sync（先忽略效能，確保持久性）
    return w.file.Sync()
}
```

**驗證**：

```bash
# 建立簡單測試
go test -run TestNewWAL
go test -run TestAppend
```

**Day 1 完成標誌**：

- ✅ 能夠建立 WAL
- ✅ 能夠寫入事件
- ✅ 事件包含正確的 checksum
- ✅ 測試通過

---

### Day 2：重放與恢復 🔄

**目標**：能夠從 WAL 恢復狀態

#### Step 1: 實作 Replay（1.5 小時）

**檔案**：`wal.go`

```go
func (w *WAL) Replay(handler EventHandler) error {
    // TODO: 重新開啟檔案（只讀）
    file, err := os.Open(w.path)
    if err != nil {
        return err
    }
    defer file.Close()

    // TODO: 建立 decoder
    decoder := json.NewDecoder(file)

    // TODO: 循環讀取
    for decoder.More() {
        var event Event
        if err := decoder.Decode(&event); err != nil {
            if err == io.EOF {
                break
            }
            return &CorruptionError{Seq: event.Seq, Cause: err}
        }

        // TODO: 驗證 checksum
        if !VerifyChecksum(event) {
            return &ChecksumError{
                Seq:      event.Seq,
                Expected: CalculateChecksum(event.Type, event.JobID, event.Seq),
                Actual:   event.Checksum,
            }
        }

        // TODO: 呼叫 handler
        if err := handler(event); err != nil {
            return err
        }
    }

    return nil
}
```

#### Step 2: 完善 NewWAL 的 Seq 讀取（1 小時）

```go
func NewWAL(path string) (*WAL, error) {
    // ... 前面的代碼 ...

    // TODO: 如果檔案已存在，讀取最後的 seq
    stat, _ := file.Stat()
    if stat.Size() > 0 {
        lastEvent, err := getLastEvent(path)
        if err == nil && lastEvent != nil {
            wal.seq = lastEvent.Seq
        }
    }

    return wal, nil
}
```

#### Step 3: 撰寫測試（1 小時）

**檔案**：`wal_test.go`

```go
func TestReplay(t *testing.T) {
    // 1. 建立臨時 WAL
    tmpDir := t.TempDir()
    walPath := filepath.Join(tmpDir, "test.wal")

    // 2. 寫入 10 個事件
    wal, _ := NewWAL(walPath)
    for i := 1; i <= 10; i++ {
        wal.Append(EventEnqueue, fmt.Sprintf("job-%d", i))
    }
    wal.Close()

    // 3. Replay
    wal2, _ := NewWAL(walPath)
    events := []Event{}
    handler := func(e Event) error {
        events = append(events, e)
        return nil
    }
    wal2.Replay(handler)

    // 4. 驗證
    if len(events) != 10 {
        t.Errorf("expected 10 events, got %d", len(events))
    }
}
```

**Day 2 完成標誌**：

- ✅ 能夠 Replay WAL
- ✅ 校驗和驗證正確
- ✅ NewWAL 能繼續已存在檔案的 seq
- ✅ 測試通過

---

### Day 3：日誌旋轉 🔄

**目標**：支援快照後清空 WAL

#### Step 1: 實作 Rotate（1 小時）

```go
func (w *WAL) Rotate() error {
    w.mu.Lock()
    defer w.mu.Unlock()

    // TODO: 關閉當前檔案
    w.file.Close()

    // TODO: 備份舊檔案
    oldPath := w.path + ".old"
    os.Rename(w.path, oldPath)

    // TODO: 建立新檔案
    newFile, err := os.Create(w.path)
    if err != nil {
        return err
    }

    // TODO: 更新 WAL 狀態
    w.file = newFile
    w.encoder = json.NewEncoder(newFile)
    w.seq = 0

    return nil
}
```

#### Step 2: 實作 Close 和 GetLastSeq（30 分鐘）

```go
func (w *WAL) Close() error {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.file.Close()
}

func (w *WAL) GetLastSeq() uint64 {
    w.mu.Lock()
    defer w.mu.Unlock()
    return w.seq
}
```

#### Step 3: 測試（1 小時）

```go
func TestRotate(t *testing.T) {
    // 1. 寫入 5 個事件
    // 2. Rotate
    // 3. 寫入 3 個事件
    // 4. 驗證舊檔案有 5 個
    // 5. 驗證新檔案有 3 個，seq 從 1 開始
}
```

**Day 3 完成標誌**：

- ✅ Rotate 正確清空 WAL
- ✅ 舊檔案被保留
- ✅ Seq 正確重置
- ✅ 測試通過

---

### Day 4：並發與整合 🔗

**目標**：確保並發安全，與 Controller 整合

#### Step 1: 並發測試（1 小時）

```go
func TestConcurrentAppend(t *testing.T) {
    wal, _ := NewWAL(t.TempDir() + "/test.wal")
    defer wal.Close()

    var wg sync.WaitGroup
    for i := 0; i < 10; i++ {
        wg.Add(1)
        go func(id int) {
            defer wg.Done()
            for j := 0; j < 100; j++ {
                wal.Append(EventEnqueue, fmt.Sprintf("job-%d-%d", id, j))
            }
        }(i)
    }
    wg.Wait()

    // 驗證：應有 1000 個事件，seq 連續
}
```

**驗證**：

```bash
go test -race -run TestConcurrent
```

#### Step 2: 整合到 Controller（2 小時）

**檔案**：`internal/controller/controller.go`

```go
type Controller struct {
    wal   *wal.WAL
    jobManager *jobmanager.JobManager
    // ...
}

func NewController(walPath string) (*Controller, error) {
    // 1. 建立 WAL
    walInstance, err := wal.NewWAL(walPath)
    if err != nil {
        return nil, err
    }

    // 2. 建立 State
    stateInstance := jobmanager.NewJobManager()

    // 3. Replay WAL
    handler := func(event wal.Event) error {
        switch event.Type {
        case wal.EventEnqueue:
            // TODO: 從哪裡取得 Job？
        case wal.EventDispatch:
            stateInstance.MarkInFlight(event.JobID, time.Now().Add(timeout))
        // ...
        }
        return nil
    }
    walInstance.Replay(handler)

    return &Controller{wal: walInstance, state: stateInstance}, nil
}

func (c *Controller) Enqueue(job Job) error {
    // 1. 寫 WAL
    if err := c.wal.Append(wal.EventEnqueue, job.ID); err != nil {
        return err
    }

    // 2. 修改狀態
    return c.jobManager.Enqueue(job)
}
```

**Day 4 完成標誌**：

- ✅ 通過並發測試
- ✅ Controller 成功整合 WAL
- ✅ 恢復流程正確
- ✅ go test -race 無錯誤

---

### Day 5：優化與完善 🚀

**目標**：效能優化與工具完善

#### Step 1: 批次寫入（選用，2 小時）

**檔案**：`batch_writer.go`

- 實作 `NewBatchWriter()`
- 實作批次 Flush 邏輯
- Benchmark 測試

#### Step 2: 工具函式（選用，1 小時）

**檔案**：`utils.go`

- 實作 `ValidateWAL()`
- 實作 `GetWALStats()`
- 實作 `DumpWAL()`

#### Step 3: 效能測試（1 小時）

```bash
go test -bench=BenchmarkAppend
go test -bench=BenchmarkReplay
go test -bench=BenchmarkBatchWriter
```

**Day 5 完成標誌**：

- ✅ 批次寫入提升吞吐量 5-10 倍
- ✅ 工具函式可用
- ✅ 效能達標（≥ 200 events/s）

---

## 🔍 除錯技巧

### 查看 WAL 內容

```bash
# 人類可讀格式
cat /data/wal.log | jq '.'

# 程式方式
go run tools/dump_wal.go /data/wal.log
```

### 驗證 WAL 完整性

```bash
go run tools/validate_wal.go /data/wal.log
```

### 模擬崩潰恢復

```bash
# 1. 執行系統
go run cmd/queue/main.go run

# 2. 寫入資料
go run cmd/queue/main.go enqueue --file jobs.json

# 3. 強制終止（模擬崩潰）
kill -9 <PID>

# 4. 重新啟動（應自動恢復）
go run cmd/queue/main.go run
```

---

## 📝 實作檢查清單

### 必須實作（Phase 1）

- [ ] `types.go` - Event, EventType 定義
- [ ] `checksum.go` - 校驗和計算與驗證
- [ ] `errors.go` - 錯誤類型定義
- [ ] `wal.go` - NewWAL, Append, Replay, Rotate, Close
- [ ] `wal_test.go` - 所有基礎測試
- [ ] Controller 整合

### 選用實作（Phase 2）

- [ ] `batch_writer.go` - 批次寫入優化
- [ ] `utils.go` - 工具函式
- [ ] 效能優化（Benchmark）

### 測試檢查

- [ ] `TestNewWAL` - 建立 WAL
- [ ] `TestAppend` - 追加事件
- [ ] `TestReplay` - 重放事件
- [ ] `TestRotate` - 日誌旋轉
- [ ] `TestChecksum` - 校驗和驗證
- [ ] `TestConcurrent` - 並發安全
- [ ] `go test -race` 無錯誤
- [ ] 整合測試通過

---

## 💡 常見問題

### Q1: NewWAL 時如何讀取最後的 seq？

**簡單方案**（Day 1）：從 0 開始，忽略已存在檔案  
**完整方案**（Day 2）：掃描檔案取得最後事件的 seq

### Q2: Append 失敗但 Encode 已寫入怎麼辦？

**答**：Sync 失敗時資料可能部分寫入，但不保證持久化。重啟後 Replay 會檢測到損壞（checksum 錯誤）並報錯。

### Q3: Replay 時如何處理重複事件？

**答**：Handler 需要實作冪等性。例如：

```go
case EventAck:
    if !jobManager.IsCompleted(jobID) {
        jobManager.MarkCompleted(jobID)
    }
```

### Q4: WAL 只記錄 JobID，恢復時如何取得完整 Job？

**答**：完整 Job 資料在 Snapshot 中。恢復流程：

1. Load Snapshot（包含完整 Job）
2. Replay WAL（只應用狀態轉換）

---

## 🎓 下一步

完成 WAL 模組後：

1. 📸 實作 Snapshot 模組
2. 👷 實作 Worker Pool
3. 🎮 實作 Controller
4. 🧪 整合測試
5. 📊 效能調優

祝您實作順利！🚀
