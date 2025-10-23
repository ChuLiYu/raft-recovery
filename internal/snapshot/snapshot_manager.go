// ============================================================================
// Beaver-Raft 快照管理器 - 系統狀態持久化
// ============================================================================
//
// Package: internal/snapshot
// 文件: snapshot_manager.go
// 功能: 定期保存系統完整狀態，實現快速崩潰恢復
//
// 設計目標:
//   1. 快速恢復 - 從快照恢復比重放所有 WAL 日誌快得多
//   2. 數據安全 - 使用原子性寫入防止快照損壞
//   3. 版本兼容 - 支持 schema 版本演進
//   4. 可讀性 - JSON 格式便於調試和手動檢查
//
// 快照策略:
//   採用定期快照 + WAL 的混合方案：
//
//   時間軸：
//   ├─ Snapshot 1 (T1)
//   ├─ WAL entry 1
//   ├─ WAL entry 2
//   ├─ WAL entry 3
//   ├─ Snapshot 2 (T2)  ← 最新快照
//   ├─ WAL entry 4      ← 需要重放
//   └─ WAL entry 5      ← 需要重放
//
//   恢復流程：
//   1. 加載最新快照（Snapshot 2）
//   2. 重放快照後的 WAL（entry 4, 5）
//   3. 總恢復時間 = 快照加載時間 + 少量 WAL 重放時間
//
// 原子性寫入:
//   為防止寫入過程中崩潰導致快照損壞，採用以下流程：
//   1. 寫入臨時文件 snapshot.json.tmp
//   2. 寫入完成後調用 os.Rename()
//   3. os.Rename() 是原子操作（POSIX 保證）
//   4. 確保快照文件要麼是完整的，要麼不存在
//
// 數據格式:
//   JSON 格式的快照包含：
//   {
//     "jobs": {              // 所有任務的完整狀態
//       "job-1": {...},
//       "job-2": {...}
//     },
//     "schema_ver": 1,       // Schema 版本號
//     "last_seq": 12345      // WAL 最後序列號
//   }
//
// Schema 版本管理:
//   - V1: 當前版本，包含基本任務信息
//   - 未來版本: 可以添加新字段，保持向後兼容
//   - 加載時檢查版本，不兼容則返回錯誤
//
// 錯誤處理:
//   - ErrSnapshotNotFound: 首次啟動，沒有快照文件（正常情況）
//   - ErrCorruptedSnapshot: JSON 解析失敗，快照損壞
//   - ErrIncompatibleVersion: Schema 版本不兼容
//
// 性能優化:
//   - 使用 sync.Mutex 確保寫入原子性
//   - JSON 縮排格式（便於調試，性能影響可接受）
//   - 考慮未來使用壓縮減小文件大小
//
// 職責說明：
//   1. 將系統完整狀態序列化為 JSON 快照檔
//   2. 使用原子性寫入（temp file + rename）防止損壞
//   3. 載入時驗證 schema 版本相容性
//   4. 配合 WAL 實現快速恢復（< 3s 目標）
//
// ============================================================================

package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ============================================================================
// 錯誤定義
// ============================================================================

var (
	ErrCorruptedSnapshot   = errors.New("snapshot file is corrupted")
	ErrIncompatibleVersion = errors.New("snapshot schema version is incompatible")
	ErrSnapshotNotFound    = errors.New("snapshot file not found")
)

// ============================================================================
// 資料結構定義
// ============================================================================

// Manager 快照管理器
type Manager struct {
	path string     // 快照檔案路徑
	mu   sync.Mutex // 保護檔案操作
}

// 使用 pkg/types.SnapshotData 結構（已在 pkg/types/types.go 定義）：
//   - Jobs: map[JobID]*Job  // 所有任務的統一儲存
//   - SchemaVer: int        // 版本號（目前為 1）
//   - LastSeq: uint64       // WAL 最後序號

// ============================================================================
// 核心方法實作
// ============================================================================

// NewManager 建立快照管理器實例
func NewManager(path string) *Manager {
	return &Manager{
		path: path,
	}
}

// Write 原子性寫入快照
//
// 使用原子性寫入流程：
// 1. 寫入臨時檔案（.tmp）
// 2. 使用 os.Rename 原子性替換原始檔案
//
// 參數：
//   - data: 快照資料（使用 pkg/types.SnapshotData）
//
// 返回值：
//   - error: 寫入失敗時的錯誤
func (m *Manager) Write(data types.SnapshotData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 設定版本號（目前為 1）
	data.SchemaVer = 1

	// 序列化為 JSON（帶縮排，方便人工閱讀與除錯）
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// 原子性寫入流程
	tmpPath := m.path + ".tmp"

	// 1. 寫入臨時檔案
	if err := os.WriteFile(tmpPath, jsonBytes, 0644); err != nil {
		return fmt.Errorf("failed to write temp snapshot: %w", err)
	}

	// 2. 原子性重新命名（關鍵步驟）
	if err := os.Rename(tmpPath, m.path); err != nil {
		// 重新命名失敗，清理臨時檔案
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	return nil
}

// Load 載入快照
//
// 行為：
//   - 如果檔案不存在，回傳空的 SnapshotData（首次啟動）
//   - 驗證 schema 版本是否相容
//   - 偵測損壞的快照檔案
//
// 返回值：
//   - types.SnapshotData: 快照資料
//   - error: 載入失敗或版本不相容時的錯誤
func (m *Manager) Load() (types.SnapshotData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var data types.SnapshotData

	// 讀取檔案
	jsonBytes, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			// 首次啟動，無快照，回傳空狀態
			return types.SnapshotData{
				Jobs:      make(map[types.JobID]*types.Job),
				SchemaVer: 1,
				LastSeq:   0,
			}, nil
		}
		return data, fmt.Errorf("failed to read snapshot: %w", err)
	}

	// 反序列化
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return data, fmt.Errorf("%w: %v", ErrCorruptedSnapshot, err)
	}

	// 驗證版本
	if data.SchemaVer != 1 {
		return data, fmt.Errorf("%w: got %d, want 1", ErrIncompatibleVersion, data.SchemaVer)
	}

	// 確保 Jobs map 不為 nil
	if data.Jobs == nil {
		data.Jobs = make(map[types.JobID]*types.Job)
	}

	return data, nil
}

// Exists 檢查快照檔案是否存在
func (m *Manager) Exists() bool {
	_, err := os.Stat(m.path)
	return err == nil
}

// GetPath 取得快照檔案路徑（用於測試與除錯）
func (m *Manager) GetPath() string {
	return m.path
}

// ============================================================================
// ✅ 已完成的 TODO
// ============================================================================

// ✅ TODO 1: 實作 Write 與原子寫入邏輯（確保不損壞）
// ✅ TODO 2: 實作 Load 與版本驗證（確保相容性）
// ⏳ TODO 3: 加入壓縮支援（可選，Phase 1 可跳過）

// ============================================================================
// 進階功能（未來優化）
// ============================================================================

// WriteWithBackup 寫入快照並保留舊版本備份
//
// 用於更安全的快照管理，保留最近幾個版本
func (m *Manager) WriteWithBackup(data types.SnapshotData, keepBackups int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// 如果存在舊快照，先備份
	if m.Exists() {
		backupPath := fmt.Sprintf("%s.%s", m.path, time.Now().Format("20060102_150405"))
		if err := os.Rename(m.path, backupPath); err != nil {
			return fmt.Errorf("failed to backup old snapshot: %w", err)
		}

		// TODO: 清理過舊的備份檔案（保留最近 keepBackups 個）
	}

	// 解鎖後調用原始 Write 方法
	m.mu.Unlock()
	err := m.Write(data)
	m.mu.Lock()

	return err
}

// ============================================================================
// 進階功能（Phase 1 可選）
// ============================================================================

/*
壓縮支援（未來實作）:

  import "compress/gzip"

  Write():
    gzipWriter := gzip.NewWriter(tmpFile)
    json.NewEncoder(gzipWriter).Encode(data)
    gzipWriter.Close()

  Load():
    gzipReader, _ := gzip.NewReader(file)
    json.NewDecoder(gzipReader).Decode(&data)

  效益: 大型佇列（10萬任務）時可節省 70% 磁碟空間
*/
