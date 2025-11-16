package wal

import "github.com/ChuLiYu/raft-recovery/pkg/types"

// ============================================================================
// WAL Type Definitions
// Responsibility: Define core data structures for WAL
// ============================================================================

// EventType defines WAL event types
type EventType string

const (
	EventEnqueue  EventType = "ENQUEUE"  // Job added to queue
	EventDispatch EventType = "DISPATCH" // Job dispatched to Worker
	EventAck      EventType = "ACK"      // Worker confirms completion
	EventRetry    EventType = "RETRY"    // Job requeued
	EventTimeout  EventType = "TIMEOUT"  // Job timed out
	EventDead     EventType = "DEAD"     // Job failed (exceeded retry count)
)

// Event represents a WAL event record
type Event struct {
	Seq       uint64      `json:"seq"`       // Event sequence number (monotonically increasing)
	Type      EventType   `json:"type"`      // Event type
	JobID     types.JobID `json:"job_id"`    // Job ID (using pkg/types type)
	Timestamp int64       `json:"timestamp"` // Unix millisecond timestamp
	Checksum  uint32      `json:"checksum"`  // CRC32 checksum

	// Phase 2: Extended fields
	// WorkerID  string      `json:"worker_id,omitempty"` // Worker ID processing the job
	// Attempt   int         `json:"attempt,omitempty"`   // Job attempt count
	// Payload   []byte      `json:"payload,omitempty"`   // Partial job data (for debugging)
}

// TODO (Phase 2):
// - Extend Event structure: add WorkerID, Attempt, Payload fields
// - Design Payload structure, consider serialization/deserialization efficiency
// - Evaluate WAL record size and performance impact
// - Add more event types (e.g., CANCEL, PAUSE)
// - Support event versioning (compatible upgrades)

// EventHandler is the function type for processing WAL events
// Used during Replay to apply events to system state
type EventHandler func(event Event) error

// TODO (Phase 2):
// - When handler returns error, should we abort entire Replay?
// - Support lenient mode to "skip corrupted events"
// - Record which events failed processing for debugging and recovery
