package wal

// ============================================================================
// Batch Writer (Optional Optimization)
// Purpose: Batch accumulate events to reduce fsync count and improve performance
// ============================================================================

import (
	"sync"
	"time"
)

// BatchWriter batch writer
//
// Design Philosophy:
// - Accumulate multiple events, write and fsync at once
// - Trade-off: Latency vs Throughput
//
// Use Cases:
// - High throughput scenarios (> 1000 events/s)
// - Acceptable small delay (< 10ms)
type BatchWriter struct {
	wal *WAL // Underlying WAL instance

	mu     sync.Mutex
	buffer []Event     // Pending event buffer
	timer  *time.Timer // Periodic flush timer

	// Configuration
	maxBatchSize  int           // Buffer size threshold
	flushInterval time.Duration // Maximum wait time
}

// NewBatchWriter creates a batch writer
//
// Parameters:
//
//	wal           - Underlying WAL instance
//	maxBatchSize  - Flush immediately after accumulating this many events
//	flushInterval - Maximum wait time before flush (even if not full)
func NewBatchWriter(wal *WAL, maxBatchSize int, flushInterval time.Duration) *BatchWriter {
	// TODO: Implement constructor
	// 1. Create BatchWriter struct
	// 2. Start background goroutine for periodic flush:
	//    go bw.flushLoop()
	// 3. Consider:
	//    - How to gracefully stop background goroutine?
	//    - Need context.Context?

	return nil
}

// Append adds event to buffer
//
// Behavior:
// - Add to buffer
// - If buffer is full, flush immediately
// - Otherwise wait for periodic flush
func (bw *BatchWriter) Append(eventType EventType, jobID string) error {
	// TODO: Implement batch append
	// 1. Create Event (but don't write to file immediately)
	// 2. Add to buffer
	// 3. Check if flush is needed:
	//    if len(buffer) >= maxBatchSize {
	//      return bw.flush()
	//    }
	// 4. Consider:
	//    - Should Append block until flush completes?
	//    - Or async flush and return immediately?

	return nil
}

// Flush immediately writes all buffered events
func (bw *BatchWriter) Flush() error {
	// TODO: Implement forced flush
	// 1. Acquire lock
	// 2. Iterate buffer, write each to WAL
	// 3. Sync once
	// 4. Clear buffer
	// 5. Consider:
	//    - If one event write fails midway, how to handle?
	//    - Should written events be rolled back? Or accept partial write?

	return nil
}

// Close closes batch writer
func (bw *BatchWriter) Close() error {
	// TODO: Implement close logic
	// 1. Stop background goroutine
	// 2. Flush remaining buffered events
	// 3. Don't close underlying WAL (caller's responsibility)

	return nil
}

// ============================================================================
// Private Methods
// ============================================================================

// flushLoop background periodic flush loop
func (bw *BatchWriter) flushLoop() {
	// TODO: Implement periodic flush
	// ticker := time.NewTicker(flushInterval)
	// for range ticker.C {
	//   bw.Flush()
	// }
	//
	// Consider:
	// - How to stop this goroutine? (context or done channel)
	// - How to handle Flush failure? Log? Notify user?
}

// TODO: Advanced considerations for batch writing
//
// 1. Performance testing:
//    - Test impact of different maxBatchSize (1, 10, 100, 1000)
//    - Test impact of different flushInterval (1ms, 10ms, 100ms)
//    - Find optimal balance point
//
// 2. Memory management:
//    - Large buffer consumes memory
//    - Consider using sync.Pool to reuse Event objects
//
// 3. Latency sensitivity:
//    - Batch writing increases latency (worst case = flushInterval)
//    - Should we provide both "immediate mode" and "batch mode" options?
//
// 4. Crash recovery:
//    - Unwritten events in buffer will be lost on crash
//    - How to balance performance vs reliability?
//    - Mission-critical systems may not be suitable for batch writing
