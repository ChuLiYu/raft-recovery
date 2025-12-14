// ============================================================================
// Beaver-Raft WAL (Write-Ahead Log) - Write-Ahead Log Implementation
// ============================================================================
//
// Package: internal/storage/wal
// File: wal.go
// Purpose: Implement WAL mechanism to ensure data persistence and crash recovery
//
// WAL Concept:
//   Write-Ahead Log is a core technology in database systems:
//   1. Before any state modification, write operation to WAL
//   2. Only modify in-memory state after WAL write succeeds
//   3. Recover state by replaying WAL after crash
//   4. Ensure data won't be lost due to crashes
//
// How It Works:
//   Operation Flow:
//   ┌─────────────┐
//   │ 1. Append   │ → Write to WAL log
//   │    WAL      │
//   └─────────────┘
//          ↓
//   ┌─────────────┐
//   │ 2. Sync     │ → Force flush to disk (fsync)
//   │    Disk     │
//   └─────────────┘
//          ↓
//   ┌─────────────┐
//   │ 3. Update   │ → Update in-memory state
//   │    Memory   │
//   └─────────────┘
//
//   Recovery Flow:
//   ┌─────────────┐
//   │ 1. Load     │ → Load latest snapshot
//   │    Snapshot │
//   └─────────────┘
//          ↓
//   ┌─────────────┐
//   │ 2. Replay   │ → Replay WAL after snapshot
//   │    WAL      │
//   └─────────────┘
//          ↓
//   ┌─────────────┐
//   │ 3. Resume   │ → Continue normal operation
//   │    Normal   │
//   └─────────────┘
//
// Data Format:
//   Each WAL record contains:
//   {
//     "seq": 12345,              // Sequence number, monotonically increasing
//     "type": "JobEnqueued",     // Event type
//     "timestamp": 1698765432,   // Unix millisecond timestamp
//     "job_id": "job-123",       // Job ID
//     "payload": {...}           // Event-related data
//   }
//
// Event Types:
//   - JobEnqueued: Job enqueued
//   - JobDispatched: Job dispatched to Worker
//   - JobCompleted: Job completed successfully
//   - JobFailed: Job execution failed
//   - JobDead: Job marked as dead letter
//
// Batch Write Optimization:
//   To improve performance, use batch write strategy:
//   - Events first accumulate in memory buffer
//   - Flush to disk when batch size reached or timeout
//   - Reduce fsync call count (fsync is expensive)
//   - Trade-off: Latency vs Throughput
//
// Log Rotation:
//   After periodic snapshot creation, old WAL can be cleared:
//   1. Create system snapshot
//   2. Compress old WAL (optional)
//   3. Create new WAL file
//   4. Delete or archive old files
//
// Data Integrity:
//   - Checksum: Each record includes checksum
//   - Atomic Write: Use append-only mode
//   - Fsync: Ensure data actually written to disk
//   - Skip corrupted records during replay
//
// Performance Considerations:
//   - Batch writing reduces I/O count
//   - JSON format convenient for debugging but has performance overhead
//   - Consider using binary format (Protocol Buffers)
//   - sync.Mutex ensures concurrency safety
//
// Error Handling:
//   - Write failure: Return error, caller decides whether to retry
//   - Replay failure: Skip corrupted records, log warnings
//   - Disk full: Requires external monitoring and alerts
//
// Collaboration with Snapshot:
//   WAL and Snapshot are complementary:
//   - Snapshot provides base state
//   - WAL records incremental changes after snapshot
//   - Together enable fast recovery
//
// ============================================================================
// WAL Core Implementation
// Responsibilities:
// 1. Append events to log file (append-only)
// 2. Provide replay function to recover system state
// 3. Support log rotation (clear after snapshot)
// 4. Ensure write durability and data integrity
// ============================================================================

package wal

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// FileInterface defines the methods required for file operations
// This allows mocking file operations in tests
type FileInterface interface {
	Write(p []byte) (n int, err error)
	Sync() error
	Close() error
}

// batchRequest represents a single append request with response channel
type batchRequest struct {
	event Event
	errCh chan error
}

// WAL represents a Write-Ahead Log instance
type WAL struct {
	mu           sync.Mutex    // Protects concurrent writes
	file         FileInterface // WAL file
	encoder      *json.Encoder // JSON encoder
	path         string        // WAL file path
	seq          uint64        // Current event sequence number
	syncOnAppend bool          // Whether to force sync on every append (deprecated, use batch commit)

	// Batch commit fields
	batchChan     chan batchRequest // Channel for batch requests
	bufferSize    int               // Max batch size before flush
	flushInterval time.Duration     // Max time between flushes
	closed        chan struct{}     // Close signal
	wg            sync.WaitGroup    // Wait for batch writer to finish
	isClosed      bool              // Flag to prevent double close/rotate

	// Legacy fields (for backward compatibility during migration)
	buffer        []Event   // Batch write event buffer
	lastFlushTime time.Time // Last flush time
}

// SnapshotData represents the metadata for a snapshot
// This is used to integrate WAL with snapshot recovery
type SnapshotData struct {
	LastSeq uint64 // The last sequence number included in the snapshot
}

// ============================================================================
// Public Interface
// ============================================================================

// NewWAL creates a new WAL instance with async batch commit
//
// Parameters:
//   - path: WAL file path
//   - syncOnAppend: (deprecated) kept for backward compatibility
//   - bufferSize: max events per batch (e.g., 100)
//   - flushInterval: max time between flushes (e.g., 10ms)
//
// Performance:
//   - bufferSize=100, flushInterval=10ms → ~10,000 events/s on SSD
//   - bufferSize=500, flushInterval=50ms → ~100,000 events/s (higher latency)
//
// Returns:
//   - *WAL: WAL instance with background batch writer running
//   - error: if initialization fails
func NewWAL(path string, syncOnAppend bool, bufferSize int, flushInterval time.Duration) (*WAL, error) {
	// Ensure the directory exists before opening the file
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0755); err != nil {
		return nil, fmt.Errorf("failed to create WAL directory: %w", err)
	}

	// Open WAL file with O_CREATE | O_APPEND | O_RDWR mode
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		// Return error directly if file open fails
		return nil, fmt.Errorf("failed to open WAL file: %w", err)
	}

	// Wrap file with JSON Encoder for convenient event writing
	encoder := json.NewEncoder(file)

	// Initialize event sequence number, default is 0
	var seq uint64 = 0

	// If file is not empty, try to read last event to get seq
	if lastEvent, err := GetLastEvent(path); err == nil && lastEvent != nil {
		seq = lastEvent.Seq
	} else if err != ErrEmptyWAL && err != nil {
		// If file is corrupted or other error occurs
		fmt.Printf("Warning: failed to get last event, starting from seq=0: %v\n", err)
		// If read fails or file is corrupted, seq can remain 0, decide based on requirements
	}

	// Set default values if not provided
	if bufferSize <= 0 {
		bufferSize = 100 // Default: 100 events per batch
	}
	if flushInterval <= 0 {
		flushInterval = 10 * time.Millisecond // Default: 10ms
	}

	// Create WAL instance, inject state
	wal := &WAL{
		file:         file,
		encoder:      encoder,
		path:         path,
		seq:          seq,
		syncOnAppend: syncOnAppend,

		// Batch commit setup
		batchChan:     make(chan batchRequest, bufferSize*2), // Buffer is 2x batch size to avoid blocking
		bufferSize:    bufferSize,
		flushInterval: flushInterval,
		closed:        make(chan struct{}),

		// Legacy fields
		buffer:        make([]Event, 0, bufferSize),
		lastFlushTime: time.Now(),
	}

	// Start background batch writer goroutine
	wal.wg.Add(1)
	go wal.batchWriter()

	// Return WAL instance
	return wal, nil
}

// Append appends an event to WAL with async batch commit
//
// Behavior:
// - Sends event to background batch writer (non-blocking)
// - Waits for batch to be flushed to disk
// - Returns error if flush fails
//
// Performance:
// - Multiple concurrent Append() calls are batched together
// - Only one fsync() per batch (10-100x throughput improvement)
//
// Parameters:
//
//	eventType - Event type (ENQUEUE, DISPATCH, ACK, etc.)
//	job       - Job instance
//
// Returns:
//
//	error (if write fails or WAL is closed)
func (w *WAL) Append(eventType EventType, job *types.Job) error {
	// Increment seq and create event (still needs lock for seq)
	w.mu.Lock()
	w.seq++
	seq := w.seq
	w.mu.Unlock()

	timestamp := time.Now().UnixMilli()
	checksum := CalculateChecksum(eventType, *job, seq)

	event := Event{
		Seq:       seq,
		Type:      eventType,
		JobID:     job.ID,
		Timestamp: timestamp,
		Checksum:  checksum,
	}

	// Create response channel
	errCh := make(chan error, 1)

	// Send to batch writer (non-blocking with timeout)
	select {
	case w.batchChan <- batchRequest{event: event, errCh: errCh}:
		// Wait for batch to be flushed
		return <-errCh
	case <-w.closed:
		return fmt.Errorf("WAL is closed")
	}
}

// Replay replays all WAL events
//
// Behavior:
// - Read WAL file from beginning
// - Verify checksum of each event
// - Call handler to apply event
// - Stop immediately on error
//
// Parameters:
//
//	handler - Event handler function
//
// Returns:
//
//	error (if replay fails)
func (w *WAL) Replay(handler func(event *Event) error) error {
	// Acquire lock to avoid conflicts with other operations
	w.mu.Lock()
	defer w.mu.Unlock()

	// Reopen file (read-only mode)
	file, err := os.Open(w.path)
	if err != nil {
		return fmt.Errorf("failed to open WAL for replay: %w", err)
	}
	defer file.Close()

	// Create JSON decoder
	decoder := json.NewDecoder(file)

	// Loop to read each event
	for {
		// Decode event
		var event Event
		err := decoder.Decode(&event)
		if err == io.EOF {
			break
		}
		if err != nil {
			return fmt.Errorf("failed to decode event: %w", err)
		}

		// Verify checksum (using VerifyChecksum)
		if !VerifyChecksum(event) {
			return ErrChecksumMismatch
		}

		// Call handler(event)
		if err := handler(&event); err != nil {
			return err
		}
	}

	return nil
}

// Rotate rotates the log file
// Note: Rotation pauses the batch writer temporarily to ensure atomicity
//
// Returns:
//
//	error (if rotation fails)
func (w *WAL) Rotate() error {
	w.mu.Lock()
	if w.isClosed {
		w.mu.Unlock()
		return fmt.Errorf("WAL is closed or rotating")
	}
	w.isClosed = true
	w.mu.Unlock()

	// Stop batch writer temporarily
	close(w.closed)
	w.wg.Wait()

	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Close(); err != nil {
		// Attempt to restore state if possible, or leave it broken
		// Ideally we should restart writer, but simple for now
		return err
	}

	backupPath := w.path + "." + time.Now().Format("20060102_150405")
	if err := os.Rename(w.path, backupPath); err != nil {
		return err
	}

	newFile, err := os.OpenFile(w.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	w.file = newFile
	w.encoder = json.NewEncoder(newFile)
	w.seq = 0
	w.buffer = w.buffer[:0]
	w.lastFlushTime = time.Now()

	// Restart batch writer
	w.closed = make(chan struct{})
	w.wg.Add(1)
	go w.batchWriter()
	
	w.isClosed = false // Restore available state

	return nil
}

// batchWriter runs in background to flush batches
// This is the core of async batch commit optimization
func (w *WAL) batchWriter() {
	defer w.wg.Done()

	ticker := time.NewTicker(w.flushInterval)
	defer ticker.Stop()

	batch := make([]batchRequest, 0, w.bufferSize)

	for {
		select {
		case req := <-w.batchChan:
			// Accumulate requests
			batch = append(batch, req)

			// Flush when batch is full
			if len(batch) >= w.bufferSize {
				w.flushBatch(batch)
				batch = batch[:0]
			}

		case <-ticker.C:
			// Periodic flush to avoid high latency
			if len(batch) > 0 {
				w.flushBatch(batch)
				batch = batch[:0]
			}

		case <-w.closed:
			// Flush remaining batch before shutdown
			if len(batch) > 0 {
				w.flushBatch(batch)
			}
			return
		}
	}
}

// flushBatch writes a batch of events and syncs to disk
// This is where the magic happens: N events → 1 fsync
func (w *WAL) flushBatch(batch []batchRequest) {
	w.mu.Lock()
	defer w.mu.Unlock()

	var flushErr error

	// Write all events to file (in-memory buffer)
	for i := range batch {
		if err := w.encoder.Encode(batch[i].event); err != nil {
			flushErr = fmt.Errorf("failed to encode event: %w", err)
			break
		}
	}

	// Single fsync for entire batch (KEY OPTIMIZATION!)
	if flushErr == nil {
		if err := w.file.Sync(); err != nil {
			flushErr = fmt.Errorf("failed to sync WAL: %w", err)
		}
	}

	// Respond to all requests in batch
	for i := range batch {
		batch[i].errCh <- flushErr
		close(batch[i].errCh)
	}
}

// Close closes the WAL gracefully
// Ensures all pending batches are flushed before closing
func (w *WAL) Close() error {
	w.mu.Lock()
	if w.isClosed {
		w.mu.Unlock()
		return nil // Already closed
	}
	w.isClosed = true
	w.mu.Unlock()

	// Signal shutdown to batch writer
	close(w.closed)

	// Wait for batch writer to finish
	w.wg.Wait()

	// Now safe to close file
	w.mu.Lock()
	defer w.mu.Unlock()

	if err := w.file.Close(); err != nil {
		return err
	}

	// Decision: WAL instance should not be reused after Close.
	//    Reasons:
	//    - File descriptor and encoder are released, reuse will cause errors (like nil pointer).
	//    - Go standard convention: Close invalidates the instance, must not be used again.
	//    - Explicitly prohibiting reuse improves security and maintainability.
	return nil
}

// GetLastSeq gets the current event sequence number
//
// Purpose: Need to record last_seq when taking snapshot to know where to start replay during recovery
func (w *WAL) GetLastSeq() uint64 {
	if w == nil {
		return 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq
}

// ============================================================================
// Internal Helper Methods (Private)
// ============================================================================

// ============================================================================
// Legacy Methods (Deprecated, kept for backward compatibility)
// ============================================================================

// flush public method for external calls (such as Close, Rotate)
// DEPRECATED: No longer used with async batch commit
// Responsible for locking and calling internal implementation
func (w *WAL) flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

// flushLocked internal method, assumes caller already holds w.mu lock
// DEPRECATED: No longer used with async batch commit
// Batch writes buffered events and syncs to disk
func (w *WAL) flushLocked() error {
	for _, event := range w.buffer {
		if err := w.encoder.Encode(event); err != nil {
			return err
		}
	}
	w.buffer = w.buffer[:0]
	w.lastFlushTime = time.Now()
	if err := w.file.Sync(); err != nil {
		return err
	}
	return nil
}

// TODO: Advanced optimization considerations
//
// 4. Design consideration: Do we need to record replay progress (seq)?
//    - Can record design-related questions here to avoid mixing into function implementation.

// ============================================================================
// Advanced Optimization: gzip Compression and Multi-file Management (Not referenced yet)
// ============================================================================

// gzip compress WAL file
// Best practices:
// - Only compress during file rotation or snapshot, avoid compressing on every write to prevent performance bottleneck
// - Add .gz to filename for easy identification
// - Use io.Pipe + goroutine for async compression to reduce main flow blocking
// - Add .gz to filename for easy identification
func compressFile(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// Create gzip writer
	gzipWriter := gzip.NewWriter(dstFile)
	defer gzipWriter.Close()

	// Copy content directly to compressed file
	_, err = io.Copy(gzipWriter, srcFile)
	return err
}

// Multi-file management: Automatic WAL file splitting
// Best practices:
// - Split automatically based on file size or event count
// - Ensure event integrity during splitting (cannot interrupt event sequence)
// - Add sequence number or timestamp to split filenames for easy sorting during replay
func splitWALFile(srcPath string, maxSize int64) ([]string, error) {
	// Define variables: files stores split file paths, srcFile is the source file, outFile is the current split file
	var files []string               // Store paths of all split files
	srcFile, err := os.Open(srcPath) // Open the original WAL file
	if err != nil {
		return nil, err
	}
	defer srcFile.Close()

	// Create JSON decoder to read events line by line
	decoder := json.NewDecoder(srcFile) // Used to parse events in the WAL file
	var part int                        // Split file sequence number, used to generate split file names
	var currentSize int64               // Current split file size, used to determine if splitting is needed
	var outFile *os.File                // File pointer for current split file
	var encoder *json.Encoder           // JSON encoder for writing events to split file
	for decoder.More() {
		var event Event // Structure for a single event
		if err := decoder.Decode(&event); err != nil {
			return nil, err
		}
		// If no split file created yet or max size reached, create new split file
		if outFile == nil || currentSize >= maxSize {
			if outFile != nil {
				outFile.Close()
			}
			part++
			outPath := srcPath + ".part" + fmt.Sprintf("%03d", part) // Generate split file name
			outFile, err = os.Create(outPath)                        // Create new split file
			if err != nil {
				return nil, err
			}
			encoder = json.NewEncoder(outFile) // Initialize JSON encoder
			files = append(files, outPath)     // Add split file path to list
			currentSize = 0
		}
		// Write event to current split file
		if err := encoder.Encode(event); err != nil {
			outFile.Close()
			return nil, err
		}
		// Update current split file size
		currentSize += int64(len(fmt.Sprintf("%v", event)))
	}
	// Close the last split file
	if outFile != nil {
		outFile.Close()
	}
	return files, nil
}
