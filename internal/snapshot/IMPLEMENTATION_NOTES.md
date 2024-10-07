# Snapshot Manager 實作筆記

## ✅ 已完成功能

### 核心功能
1. **原子性寫入**
   - 使用 `temp file + os.Rename` 策略
   - 確保寫入過程中不會損壞現有快照
   - 測試覆蓋：`TestAtomicWrite`

2. **快照載入**
   - 支援首次啟動（無快照）
   - 版本驗證（SchemaVer = 1）
   - 損壞檔案偵測
   - 測試覆蓋：`TestFirstBoot`, `TestVersionMismatch`, `TestCorrupted`

3. **錯誤處理**
   - `ErrCorruptedSnapshot`：快照檔案損壞
   - `ErrIncompatibleVersion`：版本不相容
   - `ErrSnapshotNotFound`：快照不存在

### 進階功能
4. **帶備份的寫入**
   - `WriteWithBackup()` 方法
   - 保留舊版本快照
   - 支援回退機制

## 🎯 設計變更說明

### 原始設計（偽代碼）
```go
// 多集合分離設計
SnapshotData {
    Queue: []Job
    InFlight: map[string]InFlightInfo
    Completed: []string
    Dead: []string
    LastSeq: uint64
    SchemaVer: int
    Timestamp: int64
}
```

### 實際設計（已實作）
```go
// 統一 jobs map 設計（符合專案架構）
SnapshotData {
    Jobs: map[JobID]*Job  // 所有任務的統一儲存
    SchemaVer: int        // 版本號
    LastSeq: uint64       // WAL 序號
}
```

### 變更原因
1. **與 JobManager 一致**
   - `internal/jobmanager/job_manager.go` 使用統一的 `jobs map`
   - 透過 `Job.Status` 區分任務狀態
   - 同時維護快速索引（`queue`, `inFlight`, `completed`, `dead`）

2. **簡化快照邏輯**
   - 單一來源，避免狀態不一致
   - 序列化更簡單
   - 恢復時只需重建索引

3. **使用 pkg/types**
   - 統一的資料模型定義
   - 避免重複定義結構
   - 易於維護和擴展

## 📊 測試覆蓋

### 基礎功能測試（6 項）
- ✅ `TestNewManager`：建立管理器
- ✅ `TestWriteAndLoad`：寫入與載入
- ✅ `TestAtomicWrite`：原子性寫入（關鍵）
- ✅ `TestExists`：檔案存在性檢查
- ✅ `TestFirstBoot`：首次啟動
- ✅ `TestVersionMismatch`：版本不相容

### 錯誤處理測試（2 項）
- ✅ `TestCorrupted`：損壞快照
- ✅ `TestWriteFailure`：寫入失敗

### 進階功能測試（2 項）
- ✅ `TestWriteWithBackup`：帶備份寫入
- ✅ `TestLargeSnapshot`：大型快照（1000 任務）

### 並發安全測試（2 項）
- ✅ `TestConcurrentWrites`：並發寫入
- ✅ `TestConcurrentReads`：並發讀取

### Benchmark（2 項）
- ✅ `BenchmarkWrite`：寫入效能
- ✅ `BenchmarkLoad`：載入效能

**總計：14 項測試，100% 通過**

## 🚀 效能指標

根據 `TestLargeSnapshot` 測試結果：
- **寫入 1000 個任務**：< 1 秒
- **載入 1000 個任務**：< 1 秒
- **符合 Phase 1 恢復時間目標**：< 3 秒

## 🔄 與其他模組的整合

### 與 WAL 的整合
```go
// 快照時記錄 WAL 序號
snapshot := types.SnapshotData{
    Jobs:      jobManager.GetAllJobs(),
    SchemaVer: 1,
    LastSeq:   wal.GetLastSeq(),  // 記錄快照點
}

// 恢復時只需重放快照後的 WAL 事件
func Replay(handler EventHandler, lastSeq uint64) error {
    // 跳過 seq <= lastSeq 的事件（已在快照中）
    if event.Seq <= lastSeq {
        continue
    }
    handler(event)
}
```

### 與 JobManager 的整合
```go
// 建立快照
func (jm *JobManager) Snapshot() types.SnapshotData {
    jm.mu.RLock()
    defer jm.mu.RUnlock()
    
    return types.SnapshotData{
        Jobs:      jm.jobs,  // 直接使用統一的 jobs map
        SchemaVer: 1,
        LastSeq:   currentSeq,
    }
}

// 從快照恢復
func (jm *JobManager) Restore(data types.SnapshotData) {
    jm.mu.Lock()
    defer jm.mu.Unlock()
    
    jm.jobs = data.Jobs
    // 重建索引
    jm.queue = []types.JobID{}
    jm.inFlight = make(map[types.JobID]*types.Job)
    jm.completed = make(map[types.JobID]*types.Job)
    jm.dead = make(map[types.JobID]*types.Job)
    
    for jobID, job := range jm.jobs {
        switch job.Status {
        case types.StatusPending:
            jm.queue = append(jm.queue, jobID)
        case types.StatusInFlight:
            jm.inFlight[jobID] = job
        case types.StatusCompleted:
            jm.completed[jobID] = job
        case types.StatusDead:
            jm.dead[jobID] = job
        }
    }
}
```

## 📝 未來優化

### Phase 2 可能的改進
1. **壓縮支援**
   - 使用 gzip 壓縮快照
   - 節省 70% 磁碟空間
   - 適用於大型佇列（10 萬+ 任務）

2. **增量快照**
   - 只儲存變更的任務
   - 減少快照大小
   - 提升寫入效能

3. **自動清理**
   - 定期清理過舊的備份
   - 基於時間或數量限制
   - 避免磁碟空間耗盡

4. **快照驗證**
   - 計算快照的校驗和
   - 啟動時驗證完整性
   - 自動回退到舊版本

## ✅ 完成檢查清單

- [x] 原子性寫入實作
- [x] 載入與版本驗證
- [x] 錯誤處理
- [x] 並發安全
- [x] 單元測試（14 項，100% 通過）
- [x] 效能測試（< 1s for 1000 jobs）
- [x] 與 pkg/types 整合
- [x] 文檔完善
- [ ] 壓縮支援（Phase 2）
- [ ] 增量快照（Phase 2）

## 🎓 關鍵學習點

1. **原子性寫入的重要性**
   - 使用 `temp file + os.Rename` 是標準模式
   - 確保在任何情況下都不會損壞現有資料
   - 測試應覆蓋並發場景

2. **版本管理**
   - 明確的版本號（SchemaVer）
   - 向後相容性考量
   - 清晰的錯誤訊息

3. **測試驅動開發**
   - 先寫測試，再實作
   - 覆蓋正常、邊界、錯誤情況
   - 並發測試必不可少

4. **設計演進**
   - 根據實際需求調整設計
   - 保持與整體架構一致
   - 文檔記錄變更原因
