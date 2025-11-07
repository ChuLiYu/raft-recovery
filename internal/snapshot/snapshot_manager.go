// ============================================================================
// Beaver-Raft Snapshot Manager - System State Persistence
// ============================================================================
//
// Package: internal/snapshot
// File: snapshot_manager.go
// Purpose: Periodic system state saves for fast crash recovery
//
// Design Goals:
//   1. Fast Recovery - Snapshot restore faster than replaying all WAL logs
//   2. Data Safety - Atomic writes prevent snapshot corruption
//   3. Version Compatibility - Schema version evolution support
//   4. Readability - JSON format for debugging and manual inspection
//
// Snapshot Strategy:
//   Hybrid approach with periodic snapshots + WAL:
//
//   Timeline:
//   ├─ Snapshot 1 (T1)
//   ├─ WAL entry 1
//   ├─ WAL entry 2
//   ├─ WAL entry 3
//   ├─ Snapshot 2 (T2)  ← Latest snapshot
//   ├─ WAL entry 4      ← Needs replay
//   └─ WAL entry 5      ← Needs replay
//
//   Recovery Process:
//   1. Load latest snapshot (Snapshot 2)
//   2. Replay WAL after snapshot (entries 4, 5)
//   3. Total recovery time = snapshot load + minimal WAL replay
//
// Atomic Writes:
//   To prevent corruption from mid-write crashes:
//   1. Write to temp file snapshot.json.tmp
//   2. Call os.Rename() when complete
//   3. os.Rename() is atomic (POSIX guarantee)
//   4. Ensures snapshot is either complete or non-existent
//
// Data Format:
//   JSON snapshot contains:
//   {
//     "jobs": {              // Complete job states
//       "job-1": {...},
//       "job-2": {...}
//     },
//     "schema_ver": 1,       // Schema version
//     "last_seq": 12345      // Last WAL sequence number
//   }
//
// Schema Versioning:
//   - V1: Current version with basic job info
//   - Future versions: Add new fields, maintain backward compatibility
//   - Load-time version check, error if incompatible
//
// Error Handling:
//   - ErrSnapshotNotFound: First startup, no snapshot (normal)
//   - ErrCorruptedSnapshot: JSON parse failure, corrupted
//   - ErrIncompatibleVersion: Schema version mismatch
//
// Performance:
//   - sync.Mutex ensures write atomicity
//   - Indented JSON (debugging friendly, acceptable overhead)
//   - Future: Consider compression for size reduction
//
// Responsibilities:
//   1. Serialize system state to JSON snapshot files
//   2. Atomic writes (temp file + rename) prevent corruption
//   3. Validate schema version compatibility on load
//   4. Enable fast recovery with WAL (< 3s target)
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
// Error Definitions
// ============================================================================

var (
	ErrCorruptedSnapshot   = errors.New("snapshot file is corrupted")
	ErrIncompatibleVersion = errors.New("snapshot schema version is incompatible")
	ErrSnapshotNotFound    = errors.New("snapshot file not found")
)

// ============================================================================
// Data Structure Definitions
// ============================================================================

// Manager handles snapshot persistence
type Manager struct {
	path string     // Snapshot file path
	mu   sync.Mutex // Protects file operations
}

// Uses pkg/types.SnapshotData structure (defined in pkg/types/types.go):
//   - Jobs: map[JobID]*Job  // Unified job storage
//   - SchemaVer: int        // Version number (currently 1)
//   - LastSeq: uint64       // Last WAL sequence number

// ============================================================================
// Core Method Implementation
// ============================================================================

// NewManager creates a snapshot manager instance
func NewManager(path string) *Manager {
	return &Manager{
		path: path,
	}
}

// Write atomically writes snapshot to disk
//
// Atomic write process:
// 1. Write to temp file (.tmp)
// 2. Use os.Rename to atomically replace original
//
// Parameters:
//   - data: Snapshot data (uses pkg/types.SnapshotData)
//
// Returns:
//   - error: Error on write failure
func (m *Manager) Write(data types.SnapshotData) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// Set version number (currently 1)
	data.SchemaVer = 1

	// Serialize to JSON (indented for readability and debugging)
	jsonBytes, err := json.MarshalIndent(data, "", "  ")
	if err != nil {
		return fmt.Errorf("failed to marshal snapshot: %w", err)
	}

	// Atomic write process
	tmpPath := m.path + ".tmp"

	// 1. Write to temp file
	if err := os.WriteFile(tmpPath, jsonBytes, 0644); err != nil {
		return fmt.Errorf("failed to write temp snapshot: %w", err)
	}

	// 2. Atomic rename (critical step)
	if err := os.Rename(tmpPath, m.path); err != nil {
		// Rename failed, cleanup temp file
		os.Remove(tmpPath)
		return fmt.Errorf("failed to rename snapshot: %w", err)
	}

	return nil
}

// Load reads snapshot from disk
//
// Behavior:
//   - Returns empty SnapshotData if file doesn't exist (first startup)
//   - Validates schema version compatibility
//   - Detects corrupted snapshot files
//
// Returns:
//   - types.SnapshotData: Snapshot data
//   - error: Error on load failure or version incompatibility
func (m *Manager) Load() (types.SnapshotData, error) {
	m.mu.Lock()
	defer m.mu.Unlock()

	var data types.SnapshotData

	// Read file
	jsonBytes, err := os.ReadFile(m.path)
	if err != nil {
		if os.IsNotExist(err) {
			// First startup, no snapshot, return empty state
			return types.SnapshotData{
				Jobs:      make(map[types.JobID]*types.Job),
				SchemaVer: 1,
				LastSeq:   0,
			}, nil
		}
		return data, fmt.Errorf("failed to read snapshot: %w", err)
	}

	// Deserialize
	if err := json.Unmarshal(jsonBytes, &data); err != nil {
		return data, fmt.Errorf("%w: %v", ErrCorruptedSnapshot, err)
	}

	// Validate version
	if data.SchemaVer != 1 {
		return data, fmt.Errorf("%w: got %d, want 1", ErrIncompatibleVersion, data.SchemaVer)
	}

	// Ensure Jobs map is not nil
	if data.Jobs == nil {
		data.Jobs = make(map[types.JobID]*types.Job)
	}

	return data, nil
}

// Exists checks if snapshot file exists
func (m *Manager) Exists() bool {
	_, err := os.Stat(m.path)
	return err == nil
}

// GetPath returns snapshot file path (for testing and debugging)
func (m *Manager) GetPath() string {
	return m.path
}

// ============================================================================
// ✅ Completed TODOs
// ============================================================================

// ✅ TODO 1: Implement Write with atomic write logic (prevent corruption)
// ✅ TODO 2: Implement Load with version validation (ensure compatibility)
// ⏳ TODO 3: Add compression support (optional, skip in Phase 1)

// ============================================================================
// Advanced Features (Future Optimization)
// ============================================================================

// WriteWithBackup writes snapshot and keeps old version backups
//
// For safer snapshot management, retains recent versions
func (m *Manager) WriteWithBackup(data types.SnapshotData, keepBackups int) error {
	m.mu.Lock()
	defer m.mu.Unlock()

	// If old snapshot exists, backup first
	if m.Exists() {
		backupPath := fmt.Sprintf("%s.%s", m.path, time.Now().Format("20060102_150405"))
		if err := os.Rename(m.path, backupPath); err != nil {
			return fmt.Errorf("failed to backup old snapshot: %w", err)
		}

		// TODO: Cleanup old backups (keep recent keepBackups count)
	}

	// Unlock and call original Write method
	m.mu.Unlock()
	err := m.Write(data)
	m.mu.Lock()

	return err
}

// ============================================================================
// Advanced Features (Optional in Phase 1)
// ============================================================================

/*
Compression Support (Future Implementation):

  import "compress/gzip"

  Write():
    gzipWriter := gzip.NewWriter(tmpFile)
    json.NewEncoder(gzipWriter).Encode(data)
    gzipWriter.Close()

  Load():
    gzipReader, _ := gzip.NewReader(file)
    json.NewDecoder(gzipReader).Decode(&data)

  Benefit: Save 70% disk space for large queues (100k jobs)
*/
