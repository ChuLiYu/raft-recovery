package wal

// ============================================================================
// WAL 整合範例（僅供參考，不可編譯）
// 展示 Controller 如何使用 WAL 模組
// ============================================================================

/*

// ============================================================================
// 範例 1：Controller 初始化時恢復狀態
// ============================================================================

type Controller struct {
    wal   *WAL
    state *State
    // ... 其他欄位
}

func NewController(walPath string) (*Controller, error) {
    // 1. 建立 WAL 實例
    wal, err := NewWAL(walPath)
    if err != nil {
        return nil, fmt.Errorf("failed to create WAL: %w", err)
    }

    // 2. 建立空白狀態
    jobManager := jobmanager.NewJobManager()

    // 3. 重放 WAL 恢復狀態
    handler := func(event Event) error {
        // TODO: 根據事件類型應用到狀態
        switch event.Type {
        case EventEnqueue:
            // 從哪裡取得 Job 的完整資料？
            // WAL 只記錄 JobID，需要從 Snapshot 或其他地方取得

        case EventDispatch:
            // 標記為執行中（需要重新計算 deadline）
            jobManager.MarkInFlight(event.JobID, time.Now().Add(timeout))

        case EventAck:
            // 冪等處理：如果已完成就跳過
            if !jobManager.IsCompleted(event.JobID) {
                jobManager.MarkCompleted(event.JobID)
            }

        case EventRetry:
            // 重新排隊
            job := jobManager.GetJob(event.JobID)
            if job != nil {
                jobManager.Requeue(*job)
            }

        case EventDead:
            // 標記為失敗
            jobManager.MarkDead(event.JobID)
        }
        return nil
    }

    err = wal.Replay(handler)
    if err != nil {
        return nil, fmt.Errorf("failed to replay WAL: %w", err)
    }

    return &Controller{
        wal:   wal,
        state: state,
    }, nil
}

// ============================================================================
// 範例 2：Controller 操作時寫入 WAL
// ============================================================================

// Enqueue 加入任務
func (c *Controller) Enqueue(job Job) error {
    // TODO: 思考 - 寫入 WAL 和修改狀態的順序
    // 方案 A：先寫 WAL，後修改狀態（Write-Ahead）
    //   優點：崩潰時不會遺失已承諾的操作
    //   缺點：WAL 寫入失敗時狀態未變，需要回滾或拒絕
    //
    // 方案 B：先修改狀態，後寫 WAL
    //   優點：簡單
    //   缺點：崩潰時可能狀態已變但 WAL 未記錄

    // 方案 A 實作（推薦）：
    err := c.wal.Append(EventEnqueue, job.ID)
    if err != nil {
        return fmt.Errorf("failed to write WAL: %w", err)
    }

    err = c.jobManager.Enqueue(job)
    if err != nil {
        // TODO: WAL 已寫入但狀態修改失敗，如何處理？
        // 選項：記錄錯誤，重放時會嘗試重新 Enqueue
        return err
    }

    return nil
}

// Dispatch 分派任務給 Worker
func (c *Controller) Dispatch(jobID string) error {
    err := c.wal.Append(EventDispatch, jobID)
    if err != nil {
        return err
    }

    deadline := time.Now().Add(c.taskTimeout)
    return c.jobManager.MarkInFlight(jobID, deadline)
}

// HandleAck 處理 Worker 完成確認
func (c *Controller) HandleAck(jobID string) error {
    err := c.wal.Append(EventAck, jobID)
    if err != nil {
        return err
    }

    return c.jobManager.MarkCompleted(jobID)
}

// ============================================================================
// 範例 3：與 Snapshot 配合
// ============================================================================

// TakeSnapshot 建立快照並旋轉 WAL
func (c *Controller) TakeSnapshot() error {
    // 1. 加鎖保護（避免狀態變更）
    c.mu.Lock()
    defer c.mu.Unlock()

    // 2. 取得當前狀態
    snapshot := c.jobManager.Snapshot()

    // 3. 記錄 WAL 的 last_seq
    snapshot.LastSeq = c.wal.GetLastSeq()

    // 4. 寫入快照檔案（原子性）
    err := c.snapshotManager.Write(snapshot)
    if err != nil {
        return fmt.Errorf("failed to write snapshot: %w", err)
    }

    // 5. 旋轉 WAL（清空日誌）
    err = c.wal.Rotate()
    if err != nil {
        // TODO: 快照已寫入但 WAL 旋轉失敗，如何處理？
        // 可以繼續使用舊 WAL，下次快照時再試
        return fmt.Errorf("failed to rotate WAL: %w", err)
    }

    return nil
}

// LoadFromSnapshot 從快照恢復（優化版恢復流程）
func (c *Controller) LoadFromSnapshot() error {
    // 1. 載入快照
    snapshot, err := c.snapshotManager.Load()
    if err != nil {
        if errors.Is(err, ErrSnapshotNotFound) {
            // 沒有快照，從空白狀態開始，重放全部 WAL
            return c.replayFullWAL()
        }
        return err
    }

    // 2. 恢復狀態
    err = c.jobManager.Restore(snapshot)
    if err != nil {
        return err
    }

    // 3. 重放快照後的 WAL 事件
    // 問題：如何知道哪些事件在快照之後？
    // 方案 A：WAL 檔案已旋轉，直接重放當前 WAL（所有事件都在快照後）
    // 方案 B：比較 event.Seq 與 snapshot.LastSeq（需要保留舊 WAL）

    err = c.wal.Replay(c.buildReplayHandler())
    if err != nil {
        return err
    }

    return nil
}

// ============================================================================
// 範例 4：批次操作優化
// ============================================================================

// EnqueueBatch 批次加入任務
func (c *Controller) EnqueueBatch(jobs []Job) error {
    // 使用批次寫入器提升效能
    bw := NewBatchWriter(c.wal, 100, 10*time.Millisecond)
    defer bw.Close()

    for _, job := range jobs {
        err := bw.Append(EventEnqueue, job.ID)
        if err != nil {
            return err
        }

        err = c.jobManager.Enqueue(job)
        if err != nil {
            return err
        }
    }

    // 確保所有事件都寫入
    return bw.Flush()
}

// ============================================================================
// 範例 5：錯誤恢復與降級
// ============================================================================

// RecoverFromCorruption 從 WAL 損壞中恢復
func (c *Controller) RecoverFromCorruption(walPath string) error {
    // 1. 驗證 WAL
    err := ValidateWAL(walPath)
    if err != nil {
        log.Printf("WAL validation failed: %v", err)

        // 2. 嘗試修復
        repairedPath := walPath + ".repaired"
        err = RepairWAL(walPath, repairedPath)
        if err != nil {
            // 3. 修復失敗，降級到只使用 Snapshot
            log.Println("WAL repair failed, loading from snapshot only")
            return c.LoadFromSnapshotOnly()
        }

        // 4. 使用修復後的 WAL
        log.Printf("WAL repaired, using %s", repairedPath)
        // ... 使用 repairedPath
    }

    return nil
}

// ============================================================================
// TODO：實作時的思考點
// ============================================================================

TODO 1: WAL 與狀態修改的事務性
  問題：如何確保 WAL 寫入與狀態修改的原子性？
  方案：
    - 先寫 WAL（持久化承諾）
    - 後修改狀態（記憶體操作，快速）
    - 如果狀態修改失敗，重放時會重試

TODO 2: WAL 只記錄 JobID 的問題
  問題：Replay 時如何恢復完整的 Job 資料？
  方案：
    - Snapshot 包含完整 Job 資料
    - WAL 只記錄狀態轉換（ID + Type）
    - 恢復 = Load Snapshot + Replay WAL

TODO 3: Snapshot 與 WAL 的同步
  問題：如何確保 Snapshot 與 WAL 的一致性？
  方案：
    - Snapshot 記錄 LastSeq
    - Rotate 清空 WAL
    - 恢復時：Load Snapshot + Replay 新 WAL

TODO 4: 並發控制
  問題：多個 goroutine 同時寫入 WAL 和狀態？
  方案：
    - Controller 使用單一 mutex 保護
    - WAL.Append 內部已加鎖
    - 簡單但可能限制並發

TODO 5: 效能 vs 可靠性權衡
  問題：每次 Append 都 Sync 很慢，如何優化？
  方案：
    - 預設：每次 Sync（可靠性優先）
    - 進階：批次 Sync（效能優先）
    - 讓使用者選擇？

*/
