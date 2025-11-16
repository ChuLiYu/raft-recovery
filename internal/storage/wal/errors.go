package wal

// ============================================================================
// WAL Error Definitions
// Purpose: Define all WAL-related error types
// ============================================================================

import "errors"

// Predefined errors
var (
	// ErrCorruptedWAL indicates WAL file is corrupted (cannot parse JSON)
	ErrCorruptedWAL = errors.New("wal: file is corrupted")

	// ErrChecksumMismatch indicates checksum mismatch (data corruption or tampering)
	ErrChecksumMismatch = errors.New("wal: checksum mismatch")

	// ErrEmptyWAL indicates WAL file is empty (may encounter during replay)
	ErrEmptyWAL = errors.New("wal: file is empty")

	// ErrWALClosed indicates WAL is closed, cannot perform operation
	ErrWALClosed = errors.New("wal: already closed")

	// ErrSyncFailed indicates fsync failed (critical error)
	ErrSyncFailed = errors.New("wal: sync to disk failed")
)

// TODO: Consider error handling strategies
//
// 1. Error classification:
//    - Recoverable errors: Temporary failures, can retry
//      e.g.: Disk temporarily busy
//    - Fatal errors: Serious problems, must stop
//      e.g.: WAL file corrupted, checksum error
//
// 2. Error wrapping:
//    - Use fmt.Errorf("wal: append failed at seq=%d: %w", seq, err)
//    - Provide more context (seq, jobID, file location)
//
// 3. Error reporting:
//    - Should errors be logged?
//    - Should monitoring system (metrics) be notified?
//    - How to let users know WAL has problems?

// ChecksumError represents checksum error with detailed information
type ChecksumError struct {
	Seq      uint64 // Sequence number of failed event
	Expected uint32 // Expected checksum
	Actual   uint32 // Actual checksum
}

func (e *ChecksumError) Error() string {
	// TODO: Implement error message formatting
	// Example: "wal: checksum mismatch at seq=42 (expected=0x12345678, got=0x87654321)"
	return ""
}

// CorruptionError represents WAL corruption error
type CorruptionError struct {
	Seq    uint64 // Sequence number of failed event (if known)
	Offset int64  // Byte offset in file
	Cause  error  // Underlying error
}

func (e *CorruptionError) Error() string {
	// TODO: Implement error message formatting
	return ""
}

func (e *CorruptionError) Unwrap() error {
	return e.Cause
}

// TODO: Advanced error handling considerations
//
// 1. Error recovery mechanism:
//    - When encountering corrupted event, should we skip and continue?
//    - Should we provide "repair" function to remove corrupted events?
//
// 2. Degradation strategy:
//    - If WAL is completely corrupted, allow starting from Snapshot?
//    - How to notify users of possible data loss?
//
// 3. Defensive programming:
//    - Use panic or return error on critical path?
//    - Should WAL write failure stop the entire system?
