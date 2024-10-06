# WAL (Write-Ahead Log) 模組

## 📋 模組概覽

WAL 模組負責記錄 Beaver-Raft Job Queue 的所有狀態變更事件，提供崩潰恢復能力。

### 核心職責

1. **事件追加**：將狀態變更事件追加到日誌檔案
2. **事件重放**：從日誌檔案重建系統狀態
3. **完整性保證**：使用 CRC32 校驗和防止資料損壞
4. **日誌旋轉**：快照後清空 WAL，避免檔案無限增長

---

## 🏗️ 檔案結構

```
internal/storage/wal/
├── types.go           # 型別定義（Event, EventType, EventHandler）
├── wal.go             # WAL 核心實作
├── checksum.go        # 校驗和計算與驗證
├── errors.go          # 錯誤定義
├── batch_writer.go    # 批次寫入優化（選用）
├── utils.go           # 工具函式（驗證、修復、統計）
├── wal_test.go        # 測試檔案
└── README.md          # 本文件
```

---

## 🔑 核心概念

### Event（事件）

每個事件記錄一次狀態變更：

```go
type Event struct {
    Seq       uint64    // 全域唯一序號（單調遞增）
    Type      EventType // 事件類型
    JobID     jobmanager.JobID  // 關聯的任務 ID
    Timestamp int64     // Unix 毫秒時間戳
    Checksum  uint32    // CRC32 校驗和
}
```

### EventType（事件類型）

| 類型       | 說明                     | 觸發時機                   |
| ---------- | ------------------------ | -------------------------- |
| `ENQUEUE`  | 任務加入佇列             | Controller.Enqueue()       |
| `DISPATCH` | 任務分派給 Worker        | Controller.Dispatch()      |
| `ACK`      | Worker 確認完成          | Worker.Complete()          |
| `RETRY`    | 任務重新排隊             | Controller.Requeue()       |
| `TIMEOUT`  | 任務超時                 | Controller.HandleTimeout() |
| `DEAD`     | 任務失敗（超過重試次數） | Controller.MarkDead()      |

---

## 🚀 使用方式

### 基本使用

```go
// 1. 建立 WAL 實例
wal, err := NewWAL("/data/wal/job.log")
if err != nil {
    log.Fatal(err)
}
defer wal.Close()

// 2. 追加事件
err = wal.Append(EventEnqueue, "job-001")
err = wal.Append(EventDispatch, "job-001")
err = wal.Append(EventAck, "job-001")

// 3. 重放事件（恢復時）
handler := func(event Event) error {
    switch event.Type {
    case EventEnqueue:
        // 重建佇列狀態
    case EventDispatch:
        // 標記為執行中
    case EventAck:
        // 標記為完成
    }
    return nil
}
err = wal.Replay(handler)

// 4. 日誌旋轉（快照後）
err = wal.Rotate()
```

### 批次寫入（高吞吐量場景）

```go
// 建立批次寫入器
bw := NewBatchWriter(wal,
    100,          // 緩衝 100 個事件
    10*time.Millisecond, // 或每 10ms flush
)
defer bw.Close()

// 使用批次寫入器
bw.Append(EventEnqueue, "job-001")
bw.Append(EventEnqueue, "job-002")
// ... 自動批次 flush
```

---

## 🔄 與其他模組的互動

### 與 Controller 的互動

```
Controller 狀態變更流程：
1. Controller.Enqueue(job)
   ├─> WAL.Append(ENQUEUE, job.ID)  // 先寫 WAL
   └─> State.Enqueue(job)             // 再修改記憶體狀態

2. Controller.Dispatch(job)
   ├─> WAL.Append(DISPATCH, job.ID)
   └─> State.MarkInFlight(job.ID)

3. Controller.HandleAck(job)
   ├─> WAL.Append(ACK, job.ID)
   └─> State.MarkCompleted(job.ID)
```

### 與 Snapshot 的協作

```
正常執行週期：
T0: [WAL] 記錄事件 1-100
T1: [Snapshot] 快照當前狀態（記錄 last_seq=100）
T2: [WAL] Rotate（清空日誌，seq 重置為 0）
T3: [WAL] 記錄事件 1-50
T4: [崩潰]

恢復流程：
T5: [Snapshot] 載入快照（恢復到 seq=100 的狀態）
T6: [WAL] 重放事件 1-50（增量恢復）
T7: [完成] 系統恢復到 T4 崩潰前的狀態
```

---

## 🛡️ 可靠性保證

### 1. 資料完整性

- **Checksum**：每個事件包含 CRC32 校驗和
- **驗證**：Replay 時驗證每個事件的 checksum
- **失敗處理**：校驗和不符立即中止 Replay

### 2. 持久性

- **Fsync**：Append 後呼叫 `file.Sync()` 確保寫入磁碟
- **原子性**：Rotate 使用 temp file + rename 確保原子替換

### 3. 冪等性

重放時需確保操作冪等：

```go
// ❌ 錯誤：非冪等
case EventAck:
    state.MarkCompleted(jobID)  // 如果已完成會出錯

// ✅ 正確：冪等
case EventAck:
    if !state.IsCompleted(jobID) {
        state.MarkCompleted(jobID)
    }
```

---

## ⚙️ 設計決策

### Q: 為什麼使用 JSON 格式？

**優點**：

- 人類可讀，方便除錯
- Go 原生支援，無需額外依賴
- 靈活擴展（可增加欄位）

**缺點**：

- 空間效率較低（vs Protobuf/MessagePack）
- 解析速度較慢

**決策**：Phase 1 優先可讀性與開發速度，Phase 2 再考慮二進位格式。

### Q: 為什麼每次 Append 都 Sync？

**權衡**：

- **可靠性優先**：確保每個事件都持久化，崩潰時不丟失
- **效能影響**：Sync 很慢（~1ms），限制吞吐量

**解決方案**：

- 預設：每次 Sync（保證可靠性）
- 進階：使用 `BatchWriter` 批次 Sync（提升效能，輕微風險）

### Q: Rotate 時為什麼重置 seq？

**原因**：

- 簡化實作：每個 WAL 檔案獨立編號
- Snapshot 已記錄 `last_seq`，恢復時有明確分界點

**注意**：

- 不同 WAL 檔案的 seq 不具全域唯一性
- 需要依賴 Snapshot 的 `last_seq` 區分

---

## 📊 效能指標

### 目標效能（Phase 1）

| 指標         | 目標值           |
| ------------ | ---------------- |
| 寫入吞吐量   | ≥ 200 events/s   |
| Replay 速度  | ≥ 10000 events/s |
| 崩潰恢復時間 | < 3s             |
| 單事件大小   | ~100 bytes       |

### 效能調優建議

1. **批次寫入**：使用 `BatchWriter` 可提升 5-10 倍吞吐量
2. **預分配檔案**：減少檔案系統開銷
3. **使用 SSD**：Fsync 延遲顯著降低

---

## 🧪 測試策略

### 單元測試

- `TestAppend`：驗證事件追加
- `TestReplay`：驗證事件重放
- `TestRotate`：驗證日誌旋轉
- `TestChecksum`：驗證校驗和機制

### 錯誤注入測試

- 手動破壞 WAL 檔案，驗證錯誤處理
- Mock 檔案系統失敗（Sync, Write）

### 並發測試

- `TestConcurrentAppend`：多 goroutine 並發寫入
- 使用 `go test -race` 檢測競態條件

### 整合測試

- 與 Snapshot 配合的完整恢復流程
- 模擬真實崩潰場景

---

## 🔧 故障排除

### WAL 檔案損壞

**症狀**：Replay 回傳 `ErrCorruptedWAL` 或 `ErrChecksumMismatch`

**診斷**：

```bash
# 驗證 WAL 完整性
go run tools/validate_wal.go /data/wal/job.log

# 查看損壞位置
go run tools/dump_wal.go /data/wal/job.log
```

**修復**：

```bash
# 嘗試自動修復
go run tools/repair_wal.go /data/wal/job.log /data/wal/job.repaired.log
```

### Fsync 效能問題

**症狀**：Append 耗時 > 1ms，吞吐量低

**解決方案**：

1. 使用 SSD（Fsync 延遲降低至 0.1ms）
2. 啟用批次寫入模式
3. 調整檔案系統參數（如 `noatime`）

---

## 📚 TODO 清單

### 實作優先順序

1. **基礎實作**（必須）

   - [ ] `types.go` - 定義資料結構
   - [ ] `wal.go` - 實作 NewWAL, Append, Replay
   - [ ] `checksum.go` - 實作校驗和計算
   - [ ] `errors.go` - 定義錯誤類型

2. **核心功能**（必須）

   - [ ] `wal.go` - 實作 Rotate, Close
   - [ ] `wal_test.go` - 撰寫基礎測試

3. **進階功能**（選用）
   - [ ] `batch_writer.go` - 批次寫入優化
   - [ ] `utils.go` - 工具函式（驗證、修復）

### 思考題（實作前）

1. **Checksum 範圍**：應該包含哪些欄位？Timestamp 要不要？
2. **錯誤處理**：Replay 遇到損壞事件是中止還是跳過？
3. **備份策略**：Rotate 時舊檔案如何處理？
4. **批次寫入**：如何平衡延遲與吞吐量？
5. **並發控制**：是否允許並發 Append？並發 Replay？

---

## 📖 參考資料

- [Write-Ahead Logging (Wikipedia)](https://en.wikipedia.org/wiki/Write-ahead_logging)
- [PostgreSQL WAL Implementation](https://www.postgresql.org/docs/current/wal-intro.html)
- [etcd WAL Design](https://etcd.io/docs/v3.5/learning/design-learner/)
- [CRC32 校驗和](https://pkg.go.dev/hash/crc32)
