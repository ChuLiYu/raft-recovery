# Snapshot 與 WAL 的設計與協作

## 1. 系統背景

在分散式系統中，為了確保系統的可靠性和快速恢復，我們需要保存系統的狀態。這裡使用了兩種技術：

1. **WAL (Write-Ahead Log)**：記錄所有狀態變更的事件。
2. **Snapshot (快照)**：定期保存系統的完整狀態。

這兩者的結合，能夠在系統崩潰後快速恢復，並且避免資料丟失。

---

## 2. WAL 是什麼？

WAL 是一種「事件日誌」，用來記錄系統中每一個狀態變更的操作。

### 特性
- **順序寫入**：只追加事件，不修改。
- **持久化**：每次寫入都同步到磁碟，確保不丟失。
- **可重放**：從頭到尾執行一遍 WAL，就能恢復系統狀態。

### 範例
假設我們有一個任務處理系統，WAL 會記錄以下事件：

```plaintext
[seq=1] ENQUEUE job_1
[seq=2] DISPATCH job_1
[seq=3] ACK job_1
```

這些事件描述了：
1. 任務 `job_1` 被加入隊列。
2. 任務 `job_1` 被分配給 Worker。
3. 任務 `job_1` 被成功執行。

---

## 3. Snapshot 是什麼？

Snapshot 是系統在某一時刻的完整狀態的保存。

### 特性
- **完整狀態**：包含所有任務的當前狀態。
- **定期生成**：避免 WAL 無限增長。
- **快速恢復**：直接載入快照，不需重放所有 WAL。

### 範例
假設我們在某一時刻保存了以下快照：

```json
{
  "Jobs": {
    "job_1": {"Status": "completed"},
    "job_2": {"Status": "pending"}
  },
  "LastSeq": 3
}
```

這個快照記錄了：
- `job_1` 已完成。
- `job_2` 還在等待處理。
- 快照包含了 WAL 的第 3 個事件。

---

## 4. 為什麼需要兩者搭配？

### 問題 1：只用 WAL

如果只用 WAL，系統崩潰後需要重放所有事件：

```plaintext
[seq=1] ENQUEUE job_1
[seq=2] DISPATCH job_1
[seq=3] ACK job_1
...
[seq=1000000] ENQUEUE job_1000
```

**缺點：**
- 恢復時間太長（需要重放 100 萬個事件）。
- WAL 檔案會無限增長，佔用大量磁碟空間。

### 問題 2：只用 Snapshot

如果只用快照，系統崩潰後會丟失快照之後的變更：

```plaintext
快照：包含 seq=3
崩潰前：處理到 seq=10
```

**缺點：**
- 丟失 seq=4 到 seq=10 的變更。

### 解決方案：Snapshot + WAL

1. **定期保存快照**：記錄完整狀態。
2. **持續記錄 WAL**：保存快照之後的所有變更。
3. **恢復時結合使用**：先載入快照，再重放 WAL。

---

## 5. 系統如何協作？

### 正常運行

1. **WAL 持續記錄事件**：
   ```plaintext
   [seq=1] ENQUEUE job_1
   [seq=2] DISPATCH job_1
   [seq=3] ACK job_1
   ```

2. **定期保存快照**：
   ```json
   {
     "Jobs": {
       "job_1": {"Status": "completed"}
     },
     "LastSeq": 3
   }
   ```

3. **清理舊 WAL**：刪除 seq <= 3 的事件。

### 崩潰恢復

1. **載入快照**：恢復到 seq=3 的狀態。
2. **重放 WAL**：只重放 seq > 3 的事件。

---

## 6. 具體實現

### 快照的保存邏輯

```go
func (m *Manager) Write(data SnapshotData) error {
    tmpPath := m.path + ".tmp"

    // 1. 寫入臨時檔案
    if err := os.WriteFile(tmpPath, jsonBytes, 0644); err != nil {
        return fmt.Errorf("failed to write temp snapshot: %w", err)
    }

    // 2. 原子性重新命名
    if err := os.Rename(tmpPath, m.path); err != nil {
        os.Remove(tmpPath)
        return fmt.Errorf("failed to rename snapshot: %w", err)
    }

    return nil
}
```

### 恢復邏輯

```go
func (c *Controller) Recover() error {
    // Step 1: 載入快照
    snapshot, err := c.snapshotManager.Load()
    if err != nil {
        return fmt.Errorf("failed to load snapshot: %w", err)
    }

    // Step 2: 重放 WAL
    err = c.wal.Replay(func(event Event) error {
        if event.Seq <= snapshot.LastSeq {
            return nil  // 跳過已快照的事件
        }
        return c.applyEvent(event)
    })

    return err
}
```

---

## 7. 總結

| 特性         | WAL                  | Snapshot            | 組合使用             |
|--------------|----------------------|---------------------|---------------------|
| **持久化**   | ✅ 實時              | ✅ 定期             | ✅✅ 雙重保障         |
| **恢復速度** | ❌ 慢（需重放所有）  | ✅ 快（直接載入）   | ✅✅ 最快             |
| **磁碟使用** | ❌ 無限增長          | ✅ 固定大小         | ✅ 可控             |
| **資料完整性** | ✅ 完整             | ⚠️ 可能丟失快照間的資料 | ✅✅ 完全無損         |
| **複雜度**   | ⭐ 簡單              | ⭐ 簡單             | ⭐⭐ 中等             |

---

## 8. 類比：影片編輯

- **WAL**：錄影機，記錄每一幀。
- **Snapshot**：關鍵幀，定期保存完整畫面。
- **組合**：專業影片編輯軟體，快速跳轉 + 完整恢復。

---

## 9. 下一步

1. **完成 Controller 的整合邏輯**。
2. **加入壓縮支援**，減少快照大小。
3. **優化 WAL 清理策略**，進一步提升效能。