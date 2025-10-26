package snapshot

// ============================================================================
// Snapshot Manager 測試檔案
// 職責：驗證快照的原子性寫入、載入、版本驗證與錯誤處理
// ============================================================================

import (
	"encoding/json"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// ============================================================================
// 基礎功能測試
// ============================================================================

// TestNewManager 測試建立管理器
func TestNewManager(t *testing.T) {
	manager := NewManager("test_snapshot.json")
	assert.NotNil(t, manager)
	assert.Equal(t, "test_snapshot.json", manager.GetPath())
}

// TestWriteAndLoad 測試寫入與載入快照
func TestWriteAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 建立測試資料
	originalData := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-001": {
				ID:      "job-001",
				Status:  types.StatusPending,
				Payload: map[string]interface{}{"key": "value1"},
				Attempt: 0,
			},
			"job-002": {
				ID:      "job-002",
				Status:  types.StatusInFlight,
				Payload: map[string]interface{}{"key": "value2"},
				Attempt: 1,
			},
			"job-003": {
				ID:      "job-003",
				Status:  types.StatusCompleted,
				Payload: map[string]interface{}{"key": "value3"},
				Attempt: 2,
			},
		},
		SchemaVer: 1,
		LastSeq:   100,
	}

	// 寫入快照
	err := manager.Write(originalData)
	require.NoError(t, err)

	// 載入快照
	loadedData, err := manager.Load()
	require.NoError(t, err)

	// 驗證內容一致
	assert.Equal(t, originalData.SchemaVer, loadedData.SchemaVer)
	assert.Equal(t, originalData.LastSeq, loadedData.LastSeq)
	assert.Equal(t, len(originalData.Jobs), len(loadedData.Jobs))

	// 驗證每個任務
	for jobID, originalJob := range originalData.Jobs {
		loadedJob, exists := loadedData.Jobs[jobID]
		require.True(t, exists, "Job %s should exist", jobID)
		assert.Equal(t, originalJob.ID, loadedJob.ID)
		assert.Equal(t, originalJob.Status, loadedJob.Status)
		assert.Equal(t, originalJob.Attempt, loadedJob.Attempt)
	}
}

// TestAtomicWrite 測試原子性寫入（關鍵測試）
func TestAtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 建立初始快照
	initialData := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-old": {
				ID:      "job-old",
				Status:  types.StatusPending,
				Payload: map[string]interface{}{"version": "old"},
			},
		},
		SchemaVer: 1,
		LastSeq:   50,
	}
	err := manager.Write(initialData)
	require.NoError(t, err)

	// 並發測試：在寫入新快照時同時讀取
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: 寫入新快照
	go func() {
		defer wg.Done()
		newData := types.SnapshotData{
			Jobs: map[types.JobID]*types.Job{
				"job-new": {
					ID:      "job-new",
					Status:  types.StatusPending,
					Payload: map[string]interface{}{"version": "new"},
				},
			},
			SchemaVer: 1,
			LastSeq:   100,
		}
		err := manager.Write(newData)
		assert.NoError(t, err)
	}()

	// Goroutine 2: 讀取快照
	var loadedData types.SnapshotData
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // 稍微延遲，增加並發機會
		data, err := manager.Load()
		assert.NoError(t, err)
		loadedData = data
	}()

	wg.Wait()

	// 驗證：應該讀到完整的快照（舊的或新的），不會是半成品
	assert.True(t, loadedData.LastSeq == 50 || loadedData.LastSeq == 100,
		"Should load either old (50) or new (100) snapshot, got %d", loadedData.LastSeq)

	// 驗證 .tmp 檔案應該不存在
	tmpPath := snapshotPath + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "Temp file should not exist after write")
}

// TestExists 測試檔案存在性檢查
func TestExists(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 初始不存在
	assert.False(t, manager.Exists())

	// 寫入後存在
	data := types.SnapshotData{
		Jobs:      make(map[types.JobID]*types.Job),
		SchemaVer: 1,
		LastSeq:   0,
	}
	err := manager.Write(data)
	require.NoError(t, err)
	assert.True(t, manager.Exists())
}

// ============================================================================
// 錯誤處理測試
// ============================================================================

// TestFirstBoot 測試首次啟動（無快照）
func TestFirstBoot(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "non_existent_snapshot.json")
	manager := NewManager(snapshotPath)

	// 載入不存在的快照應該回傳空狀態，不是錯誤
	loadedData, err := manager.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, loadedData.SchemaVer)
	assert.Equal(t, uint64(0), loadedData.LastSeq)
	assert.NotNil(t, loadedData.Jobs)
	assert.Equal(t, 0, len(loadedData.Jobs))
}

// TestVersionMismatch 測試版本不相容
func TestVersionMismatch(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 手動建立版本號為 2 的快照
	invalidData := types.SnapshotData{
		Jobs:      make(map[types.JobID]*types.Job),
		SchemaVer: 2, // 不相容的版本
		LastSeq:   0,
	}
	jsonBytes, err := json.MarshalIndent(invalidData, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(snapshotPath, jsonBytes, 0644)
	require.NoError(t, err)

	// 載入應該回傳版本不相容錯誤
	_, err = manager.Load()
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrIncompatibleVersion)
}

// TestCorrupted 測試損壞的快照
func TestCorrupted(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 寫入無效的 JSON（半截斷）
	corruptedJSON := `{"jobs": {"job-001": {"id": "job-001", "status": "pending"`
	err := os.WriteFile(snapshotPath, []byte(corruptedJSON), 0644)
	require.NoError(t, err)

	// 載入應該回傳損壞錯誤
	_, err = manager.Load()
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrCorruptedSnapshot)
}

// TestWriteFailure 測試寫入失敗（唯讀目錄）
func TestWriteFailure(t *testing.T) {
	tempDir := t.TempDir()

	// 建立唯讀目錄
	readOnlyDir := filepath.Join(tempDir, "readonly")
	err := os.Mkdir(readOnlyDir, 0444)
	require.NoError(t, err)
	defer os.Chmod(readOnlyDir, 0755) // 測試結束後恢復權限

	snapshotPath := filepath.Join(readOnlyDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	data := types.SnapshotData{
		Jobs:      make(map[types.JobID]*types.Job),
		SchemaVer: 1,
		LastSeq:   0,
	}

	// 寫入應該失敗
	err = manager.Write(data)
	assert.Error(t, err)
}

// ============================================================================
// 進階功能測試
// ============================================================================

// TestWriteWithBackup 測試帶備份的寫入
func TestWriteWithBackup(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 寫入初始快照
	initialData := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-001": {
				ID:     "job-001",
				Status: types.StatusPending,
			},
		},
		SchemaVer: 1,
		LastSeq:   50,
	}
	err := manager.Write(initialData)
	require.NoError(t, err)

	// 使用備份模式寫入新快照
	newData := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-002": {
				ID:     "job-002",
				Status: types.StatusCompleted,
			},
		},
		SchemaVer: 1,
		LastSeq:   100,
	}
	err = manager.WriteWithBackup(newData, 3)
	require.NoError(t, err)

	// 驗證新快照存在
	loadedData, err := manager.Load()
	require.NoError(t, err)
	assert.Equal(t, uint64(100), loadedData.LastSeq)

	// 驗證備份檔案存在
	files, err := os.ReadDir(tempDir)
	require.NoError(t, err)

	backupFound := false
	for _, file := range files {
		if file.Name() != "test_snapshot.json" && !file.IsDir() {
			backupFound = true
			break
		}
	}
	assert.True(t, backupFound, "Backup file should exist")
}

// TestLargeSnapshot 測試大型快照的寫入與載入
func TestLargeSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 建立包含 1000 個任務的大型快照
	largeData := types.SnapshotData{
		Jobs:      make(map[types.JobID]*types.Job),
		SchemaVer: 1,
		LastSeq:   10000,
	}

	for i := 0; i < 1000; i++ {
		jobID := types.JobID(string(rune('a'+i%26)) + string(rune('0'+i/26)))
		largeData.Jobs[jobID] = &types.Job{
			ID:      jobID,
			Status:  types.StatusPending,
			Payload: map[string]interface{}{"index": i},
			Attempt: i % 5,
		}
	}

	// 寫入大型快照
	start := time.Now()
	err := manager.Write(largeData)
	require.NoError(t, err)
	writeDuration := time.Since(start)
	t.Logf("Write duration for 1000 jobs: %v", writeDuration)

	// 載入大型快照
	start = time.Now()
	loadedData, err := manager.Load()
	require.NoError(t, err)
	loadDuration := time.Since(start)
	t.Logf("Load duration for 1000 jobs: %v", loadDuration)

	// 驗證資料完整性
	assert.Equal(t, len(largeData.Jobs), len(loadedData.Jobs))
	assert.Equal(t, largeData.LastSeq, loadedData.LastSeq)

	// 驗證效能（應該在合理時間內完成）
	assert.Less(t, writeDuration, 1*time.Second, "Write should complete in < 1s")
	assert.Less(t, loadDuration, 1*time.Second, "Load should complete in < 1s")
}

// ============================================================================
// 並發安全測試
// ============================================================================

// TestConcurrentWrites 測試並發寫入
func TestConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	numGoroutines := 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// 並發寫入
	for i := 0; i < numGoroutines; i++ {
		go func(index int) {
			defer wg.Done()
			data := types.SnapshotData{
				Jobs: map[types.JobID]*types.Job{
					types.JobID(string(rune('a' + index))): {
						ID:     types.JobID(string(rune('a' + index))),
						Status: types.StatusPending,
					},
				},
				SchemaVer: 1,
				LastSeq:   uint64(index),
			}
			err := manager.Write(data)
			assert.NoError(t, err)
		}(i)
	}

	wg.Wait()

	// 驗證最終快照是有效的
	loadedData, err := manager.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, loadedData.SchemaVer)
	assert.NotNil(t, loadedData.Jobs)
}

// TestConcurrentReads 測試並發讀取
func TestConcurrentReads(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// 先寫入一個快照
	data := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-001": {
				ID:     "job-001",
				Status: types.StatusPending,
			},
		},
		SchemaVer: 1,
		LastSeq:   100,
	}
	err := manager.Write(data)
	require.NoError(t, err)

	// 並發讀取
	numGoroutines := 20
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	for i := 0; i < numGoroutines; i++ {
		go func() {
			defer wg.Done()
			loadedData, err := manager.Load()
			assert.NoError(t, err)
			assert.Equal(t, uint64(100), loadedData.LastSeq)
			assert.Equal(t, 1, len(loadedData.Jobs))
		}()
	}

	wg.Wait()
}

// ============================================================================
// Benchmark 測試
// ============================================================================

// BenchmarkWrite 測試寫入效能
func BenchmarkWrite(b *testing.B) {
	tempDir := b.TempDir()
	snapshotPath := filepath.Join(tempDir, "benchmark_snapshot.json")
	manager := NewManager(snapshotPath)

	data := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-001": {
				ID:      "job-001",
				Status:  types.StatusPending,
				Payload: map[string]interface{}{"key": "value"},
			},
		},
		SchemaVer: 1,
		LastSeq:   100,
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_ = manager.Write(data)
	}
}

// BenchmarkLoad 測試載入效能
func BenchmarkLoad(b *testing.B) {
	tempDir := b.TempDir()
	snapshotPath := filepath.Join(tempDir, "benchmark_snapshot.json")
	manager := NewManager(snapshotPath)

	// 先寫入快照
	data := types.SnapshotData{
		Jobs: map[types.JobID]*types.Job{
			"job-001": {
				ID:      "job-001",
				Status:  types.StatusPending,
				Payload: map[string]interface{}{"key": "value"},
			},
		},
		SchemaVer: 1,
		LastSeq:   100,
	}
	_ = manager.Write(data)

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		_, _ = manager.Load()
	}
}
