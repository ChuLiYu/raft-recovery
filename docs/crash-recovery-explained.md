# 🔄 Crash Recovery 機制詳解

> 📌 **Note**: 本文檔中的示例使用 5 個任務來說明概念。實際 demo 程序使用 20 個任務以增加捕獲 in-flight 任務的機會。

## 📝 保存邏輯

### 1. WAL (Write-Ahead Log) - 立即保存

**時機**：每個操作立即寫入
**目的**：確保不丟失任何數據

```
提交任務 → 先寫 WAL → 再更新內存
完成任務 → 先寫 WAL → 再更新內存
失敗任務 → 先寫 WAL → 再更新內存
```

**代碼證明**（`internal/controller/controller.go:541`）：
```go
func (c *Controller) EnqueueJobs(jobs []types.Job) error {
    for _, job := range jobs {
        // Write to WAL first ← 先寫 WAL
        if err := c.wal.Append(wal.EventEnqueue, &job); err != nil {
            return fmt.Errorf("failed to append ENQUEUE event: %w", err)
        }

        // Add to JobManager ← 再更新內存
        if err := c.jobManager.Enqueue(job); err != nil {
            return fmt.Errorf("failed to enqueue job: %w", err)
        }
    }
    return nil
}
```

### 2. Snapshot - 定期保存

**時機**：每 30 秒執行一次
**目的**：加速恢復（避免重放太多 WAL 日誌）

```
每 30 秒 → 保存完整系統狀態到 snapshot
```

**配置**（`configs/default.yaml`）：
```yaml
snapshot:
  interval_seconds: 30  # 快照間隔
```

## 🚨 崩潰場景分析

### 場景 1：任務提交後立即崩潰（未到 snapshot 時間）

```
T=0s:  提交 5 個任務 → WAL 立即記錄 ✅
T=5s:  💥 系統崩潰！
       (還沒有 snapshot，因為 30 秒未到)

恢復時：
1. 加載最後的 snapshot (可能是空的)
2. 重放 WAL 日誌 → 找到 5 個 JobEnqueued 事件
3. 重新執行這 5 個 Enqueue 操作
4. ✅ 任務完全恢復！
```

**結論**：✅ **不會丟失**，因為 WAL 已經保存了！

### 場景 2：Snapshot 後崩潰

```
T=0s:   提交 5 個任務 → WAL 記錄
T=30s:  Snapshot 保存 (jobs=5)
T=35s:  提交 3 個新任務 → WAL 記錄
T=40s:  💥 系統崩潰！

恢復時：
1. 加載 T=30s 的 snapshot → 恢復 5 個任務
2. 重放 T=30s 之後的 WAL → 恢復 3 個新任務
3. ✅ 總共恢復 8 個任務！
```

**結論**：✅ **完全恢復**，Snapshot + WAL 配合！

### 場景 3：多次崩潰

```
T=0s:   提交 5 個任務
T=5s:   💥 崩潰 1
T=10s:  恢復 → 5 個任務恢復
T=15s:  提交 2 個新任務
T=20s:  💥 崩潰 2
T=25s:  恢復 → 7 個任務恢復

每次恢復都不會丟失數據！
```

## 📊 WAL vs Snapshot 對比

| 特性 | WAL | Snapshot |
|------|-----|----------|
| **保存時機** | 每個操作立即 | 每 30 秒 |
| **保存內容** | 操作事件 | 完整狀態 |
| **主要作用** | 確保不丟失數據 | 加速恢復 |
| **恢復速度** | 慢（需重放所有日誌） | 快（直接加載狀態） |
| **數據完整性** | ✅ 100% 保證 | ⚠️ 只到上次快照 |
| **配合使用** | ✅ Snapshot + WAL = 快速且完整的恢復 |

## 🎯 恢復流程

```
系統啟動
    ↓
加載最新 Snapshot (如果存在)
    ↓
重放 Snapshot 之後的 WAL 日誌
    ↓
重新排隊所有 in_flight 任務
    ↓
✅ 系統完全恢復！
```

**代碼位置**（`internal/controller/controller.go:140`）：
```go
func (c *Controller) Start() error {
    // 1. Load snapshot
    if err := c.loadSnapshot(); err != nil {
        return fmt.Errorf("loadSnapshot failed: %w", err)
    }

    // 2. Replay WAL
    if err := c.replayWAL(); err != nil {
        return fmt.Errorf("replayWAL failed: %w", err)
    }

    // 3. Requeue in-flight jobs
    inFlightJobs := c.jobManager.GetAllInFlightJobs()
    for _, jobID := range inFlightJobs {
        // ...
    }
}
```

## 🧪 Demo 驗證

### 測試步驟

```bash
# Step 1: 啟動系統並提交任務
./scripts/demo-interactive.sh demo2-start
# → 提交 5 個任務
# → 等待 30 秒看到 snapshot
# → Ctrl+C 崩潰

# Step 2: 恢復驗證
./scripts/demo-interactive.sh demo2-recover
# → 觀察恢復日誌
# → 確認 5 個任務全部恢復
```

### 預期輸出

**demo2-start**：
```
✓ Enqueued 5 jobs
📊 Current Status:
  Completed: 5

INFO Snapshot taken duration=9ms jobs=5  ← 快照保存
```

**demo2-recover**：
```
INFO Snapshot loaded duration=71µs jobs=5  ← 從快照恢復
INFO Recovery completed requeued_jobs=0

📊 Status After Recovery:
  Completed: 5
✓ Recovered 5 total jobs from crash!  ← 驗證成功！
```

## 💡 關鍵結論

1. **WAL 確保零數據丟失**
   - 每個操作都立即寫入
   - 即使立即崩潰也不會丟失

2. **Snapshot 加速恢復**
   - 避免重放過多 WAL 日誌
   - 恢復時間 < 3 秒

3. **兩者配合完美**
   - Snapshot 提供基準狀態
   - WAL 填補 Snapshot 之後的操作
   - 實現快速且完整的恢復

4. **課堂演示重點**
   - 提交任務後立即崩潰 → WAL 保護
   - Snapshot 後崩潰 → Snapshot + WAL 配合
   - 多次崩潰 → 每次都能完整恢復
