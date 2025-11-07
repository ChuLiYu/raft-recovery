package snapshot

// ============================================================================
// Snapshot Manager test file
// Purpose: verify atomic snapshot writes, loading, version checks with error handling
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
// Basic functionality tests
// ============================================================================

// TestNewManager tests creating a manager
func TestNewManager(t *testing.T) {
	manager := NewManager("test_snapshot.json")
	assert.NotNil(t, manager)
	assert.Equal(t, "test_snapshot.json", manager.GetPath())
}

// TestWriteAndLoad tests writing and loading snapshot
func TestWriteAndLoad(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// create test data
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

	// write snapshot
	err := manager.Write(originalData)
	require.NoError(t, err)

	// load snapshot
	loadedData, err := manager.Load()
	require.NoError(t, err)

	// verify contents match
	assert.Equal(t, originalData.SchemaVer, loadedData.SchemaVer)
	assert.Equal(t, originalData.LastSeq, loadedData.LastSeq)
	assert.Equal(t, len(originalData.Jobs), len(loadedData.Jobs))

	// verify each job
	for jobID, originalJob := range originalData.Jobs {
		loadedJob, exists := loadedData.Jobs[jobID]
		require.True(t, exists, "Job %s should exist", jobID)
		assert.Equal(t, originalJob.ID, loadedJob.ID)
		assert.Equal(t, originalJob.Status, loadedJob.Status)
		assert.Equal(t, originalJob.Attempt, loadedJob.Attempt)
	}
}

// TestAtomicWrite tests atomic write (critical test)
func TestAtomicWrite(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// create initial snapshot
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

	// Concurrent test: read while writing a new snapshot
	var wg sync.WaitGroup
	wg.Add(2)

	// Goroutine 1: write new snapshot
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

	// Goroutine 2: read snapshot
	var loadedData types.SnapshotData
	go func() {
		defer wg.Done()
		time.Sleep(5 * time.Millisecond) // small delay to increase concurrency chance
		data, err := manager.Load()
		assert.NoError(t, err)
		loadedData = data
	}()

	wg.Wait()

	// verify: should read a complete snapshot (old or new), never a partial write
	assert.True(t, loadedData.LastSeq == 50 || loadedData.LastSeq == 100,
		"Should load either old (50) or new (100) snapshot, got %d", loadedData.LastSeq)

	// verify .tmp file should not exist
	tmpPath := snapshotPath + ".tmp"
	_, err = os.Stat(tmpPath)
	assert.True(t, os.IsNotExist(err), "Temp file should not exist after write")
}

// TestExists tests file existence check
func TestExists(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// initially doesn't exist
	assert.False(t, manager.Exists())

	// exists after write
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
// Error handling tests
// ============================================================================

// TestFirstBoot tests first boot (no snapshot)
func TestFirstBoot(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "non_existent_snapshot.json")
	manager := NewManager(snapshotPath)

	// Loading a non-existent snapshot should return empty state, not error
	loadedData, err := manager.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, loadedData.SchemaVer)
	assert.Equal(t, uint64(0), loadedData.LastSeq)
	assert.NotNil(t, loadedData.Jobs)
	assert.Equal(t, 0, len(loadedData.Jobs))
}

// TestVersionMismatch tests incompatible version
func TestVersionMismatch(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// Manually create a snapshot with version 2 (incompatible)
	invalidData := types.SnapshotData{
		Jobs:      make(map[types.JobID]*types.Job),
		SchemaVer: 2, // incompatible version
		LastSeq:   0,
	}
	jsonBytes, err := json.MarshalIndent(invalidData, "", "  ")
	require.NoError(t, err)
	err = os.WriteFile(snapshotPath, jsonBytes, 0644)
	require.NoError(t, err)

	// Loading should return incompatible version error
	_, err = manager.Load()
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrIncompatibleVersion)
}

// TestCorrupted tests corrupted snapshot handling
func TestCorrupted(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// Write invalid JSON (truncated)
	corruptedJSON := `{"jobs": {"job-001": {"id": "job-001", "status": "pending"`
	err := os.WriteFile(snapshotPath, []byte(corruptedJSON), 0644)
	require.NoError(t, err)

	// Loading should return corrupted snapshot error
	_, err = manager.Load()
	assert.Error(t, err)
	assert.ErrorIs(t, err, ErrCorruptedSnapshot)
}

// TestWriteFailure tests write failure (read-only directory)
func TestWriteFailure(t *testing.T) {
	tempDir := t.TempDir()

	// create read-only directory
	readOnlyDir := filepath.Join(tempDir, "readonly")
	err := os.Mkdir(readOnlyDir, 0444)
	require.NoError(t, err)
	defer os.Chmod(readOnlyDir, 0755) // restore permissions after test

	snapshotPath := filepath.Join(readOnlyDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	data := types.SnapshotData{
		Jobs:      make(map[types.JobID]*types.Job),
		SchemaVer: 1,
		LastSeq:   0,
	}

	// write should fail
	err = manager.Write(data)
	assert.Error(t, err)
}

// ============================================================================
// Advanced functionality tests
// ============================================================================

// TestWriteWithBackup tests write with backup
func TestWriteWithBackup(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// write initial snapshot
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

	// write new snapshot with backup mode
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

	// verify new snapshot exists
	loadedData, err := manager.Load()
	require.NoError(t, err)
	assert.Equal(t, uint64(100), loadedData.LastSeq)

	// verify backup file exists
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

// TestLargeSnapshot tests writing and loading a large snapshot
func TestLargeSnapshot(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// create a large snapshot containing 1000 jobs
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

	// write large snapshot
	start := time.Now()
	err := manager.Write(largeData)
	require.NoError(t, err)
	writeDuration := time.Since(start)
	t.Logf("Write duration for 1000 jobs: %v", writeDuration)

	// load large snapshot
	start = time.Now()
	loadedData, err := manager.Load()
	require.NoError(t, err)
	loadDuration := time.Since(start)
	t.Logf("Load duration for 1000 jobs: %v", loadDuration)

	// verify data integrity
	assert.Equal(t, len(largeData.Jobs), len(loadedData.Jobs))
	assert.Equal(t, largeData.LastSeq, loadedData.LastSeq)

	// verify performance (should complete within a reasonable time)
	assert.Less(t, writeDuration, 1*time.Second, "Write should complete in < 1s")
	assert.Less(t, loadDuration, 1*time.Second, "Load should complete in < 1s")
}

// ============================================================================
// Concurrency safety tests
// ============================================================================

// TestConcurrentWrites tests concurrent writes
func TestConcurrentWrites(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	numGoroutines := 10
	var wg sync.WaitGroup
	wg.Add(numGoroutines)

	// concurrent writes
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

	// verify final snapshot is valid
	loadedData, err := manager.Load()
	require.NoError(t, err)
	assert.Equal(t, 1, loadedData.SchemaVer)
	assert.NotNil(t, loadedData.Jobs)
}

// TestConcurrentReads tests concurrent reads
func TestConcurrentReads(t *testing.T) {
	tempDir := t.TempDir()
	snapshotPath := filepath.Join(tempDir, "test_snapshot.json")
	manager := NewManager(snapshotPath)

	// write a snapshot first
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

	// concurrent reads
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
// Benchmark tests
// ============================================================================

// BenchmarkWrite tests write performance
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

// BenchmarkLoad tests load performance
func BenchmarkLoad(b *testing.B) {
	tempDir := b.TempDir()
	snapshotPath := filepath.Join(tempDir, "benchmark_snapshot.json")
	manager := NewManager(snapshotPath)

	// write a snapshot first
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
