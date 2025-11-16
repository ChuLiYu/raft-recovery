package wal

// ============================================================================
// Checksum Calculation
// Responsibility: Calculate and verify CRC32 checksum for WAL events
// ============================================================================

import (
	"hash/crc32"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// CalculateChecksum calculates the CRC32 checksum for an event
//
// Algorithm:
// - Concatenate key fields of the event into a string
// - Calculate using CRC32-IEEE polynomial
//
// Parameters:
//
//	eventType - Event type
//	jobID     - Task ID
//	seq       - Event sequence number
//
// Returns:
//
//	uint32 checksum
func CalculateChecksum(eventType EventType, jobID types.Job, seq uint64) uint32 {
	// Combine key fields of the event
	// Use Type + JobID + Seq to calculate checksum
	// Exclude Timestamp as it will change during replay
	data := string(eventType) + string(jobID.ID) + string(rune(seq))

	// Calculate checksum using CRC32-IEEE
	return crc32.ChecksumIEEE([]byte(data))
}

// VerifyChecksum verifies if the event's checksum is correct
//
// Parameters:
//
//	event - Event to verify
//
// Returns:
//
//	bool - true indicates checksum is correct
func VerifyChecksum(event Event) bool {
	// Recalculate expected checksum
	// Note: Need to create a types.Job to match CalculateChecksum's signature
	job := types.Job{ID: event.JobID}
	expected := CalculateChecksum(event.Type, job, event.Seq)

	// Compare calculated checksum with stored checksum in event
	return event.Checksum == expected
}

// TODO: Advanced feature considerations
//
// 1. Multiple checksum algorithm support:
//    - CRC32 (fast, detects random errors)
//    - SHA256 (secure, prevents tampering)
//    - Let user choose?
//
// 2. Checksum scope:
//    - Currently only checksums Type + JobID + Seq
//    - Should Timestamp be included?
//    - Should entire Event JSON be included?
//
// 3. Performance optimization:
//    - Pre-allocate string buffer to avoid repeated allocation
//    - Use strings.Builder
