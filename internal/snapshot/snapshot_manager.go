package snapshot

// ============================================================================
// 職責說明：
// 1. 將系統完整狀態序列化為 JSON 快照檔
// 2. 使用原子性寫入（temp file + rename）防止損壞
// 3. 載入時驗證 schema 版本相容性
// 4. 配合 WAL 實現快速恢復（< 3s 目標）
// ============================================================================

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/ChuLiYu/beaver-raft/pkg/types"
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
