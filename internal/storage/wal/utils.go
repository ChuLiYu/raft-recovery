package wal

// ============================================================================
// WAL Utility Functions
// Purpose: Provide WAL-related helper functionality
// ============================================================================

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
)

// ============================================================================
// File Operation Helpers
// ============================================================================

// GetLastEvent reads the last event from a WAL file
//
// Use cases:
// - NewWAL needs to get last_seq to continue numbering
// - Validate WAL integrity
//
// Parameters:
//
//	path - WAL file path
//
// Returns:
//
//	Last event, error (returns ErrEmptyWAL if file is empty)
func GetLastEvent(path string) (*Event, error) {
	var last *Event
	err := scanEvents(path, func(event Event) error { copy := event; last = &copy; return nil })
	if err != nil {
		return nil, err
	}
	if last == nil {
		return nil, ErrEmptyWAL
	}
	return last, nil
}

// CountEvents counts the total number of events in WAL
//
// Use cases:
// - Debugging and diagnostics
// - Statistics and monitoring
func CountEvents(path string) (int, error) {
	count := 0
	err := scanEvents(path, func(Event) error { count++; return nil })
	return count, err
}

// ValidateWAL validates WAL file integrity
//
// Checks:
// - All events have correct JSON format
// - All events have correct checksums
// - seq is sequential and unique
//
// Returns:
//
//	error (if any issues found)
func ValidateWAL(path string) error {
	return scanEvents(path, func(Event) error { return nil })
}

// ============================================================================
// WAL Repair Tools (Advanced Features)
// ============================================================================

// RepairWAL attempts to repair a corrupted WAL
//
// Repair strategy:
// - Scan file, remove invalid events
// - Renumber seq (starting from 1, sequential)
// - Generate new WAL file
//
// Warning: This operation will change event sequence numbers!
func RepairWAL(srcPath, dstPath string) error {
	// TODO: Implement WAL repair (optional)
	// 1. Read from srcPath
	// 2. Filter valid events:
	//    - JSON is parsable
	//    - Checksum is correct
	// 3. Renumber seq
	// 4. Write to dstPath
	// 5. Consider:
	//    - How to handle events with checksum errors but valid JSON?
	//    - Should user confirmation be required?
	//    - Should removed events be logged?

	return fmt.Errorf("WAL repair is not implemented; preserve the original file")
}

// TruncateWAL truncates WAL to specified sequence number
//
// Use cases:
// - Recover to a known good state
// - Roll back erroneous operations
//
// Parameters:
//
//	path - WAL file path
//	seq  - Keep up to this sequence number (exclusive)
func TruncateWAL(path string, seq uint64) error {
	// TODO: Implement WAL truncation (optional)
	// 1. Read all events
	// 2. Filter events with seq < targetSeq
	// 3. Write to new file
	// 4. Atomically replace old file
	// 5. Warning: Ensure operation atomicity!

	return fmt.Errorf("WAL truncation is not implemented")
}

// ============================================================================
// Debugging and Diagnostic Tools
// ============================================================================

// DumpWAL outputs WAL contents (human-readable format)
//
// Use cases:
// - Debugging
// - Manual event inspection
func DumpWAL(path string, w io.Writer) error {
	return scanEvents(path, func(event Event) error {
		_, err := fmt.Fprintf(w, "[%d] %s %s at %d checksum=%08x\n", event.Seq, event.Type, event.JobID, event.Timestamp, event.Checksum)
		return err
	})
}

// CompareWAL compares differences between two WAL files
//
// Use cases:
// - Testing
// - Verify Rotate correctness
func CompareWAL(path1, path2 string) ([]string, error) {
	return nil, fmt.Errorf("WAL comparison is not implemented")
}

// ============================================================================
// Statistics and Analysis
// ============================================================================

// WALStats WAL statistics information
type WALStats struct {
	TotalEvents    int               // Total number of events
	EventTypes     map[EventType]int // Event count by type
	FirstSeq       uint64            // Sequence number of first event
	LastSeq        uint64            // Sequence number of last event
	TimeRange      [2]int64          // Time range [earliest, latest]
	CorruptedCount int               // Number of corrupted events
}

// GetWALStats retrieves WAL statistics
func GetWALStats(path string) (*WALStats, error) {
	stats := &WALStats{EventTypes: make(map[EventType]int)}
	err := scanEvents(path, func(event Event) error {
		if stats.TotalEvents == 0 {
			stats.FirstSeq = event.Seq
			stats.TimeRange = [2]int64{event.Timestamp, event.Timestamp}
		}
		stats.TotalEvents++
		stats.EventTypes[event.Type]++
		stats.LastSeq = event.Seq
		if event.Timestamp < stats.TimeRange[0] {
			stats.TimeRange[0] = event.Timestamp
		}
		if event.Timestamp > stats.TimeRange[1] {
			stats.TimeRange[1] = event.Timestamp
		}
		return nil
	})
	return stats, err
}

// TODO: Other utility tools to consider
//
// 1. WAL Merge:
//    - Merge multiple WAL files into one
//    - For historical data consolidation
//
// 2. WAL Split:
//    - Split large WAL file into multiple smaller files
//    - Split by time or seq range
//
// 3. WAL Compaction:
//    - Remove ENQUEUE/DISPATCH/ACK events for completed jobs
//    - Keep only necessary events (e.g., Dead jobs)
//
// 4. WAL Export:
//    - Convert to other formats (CSV, Parquet)
//    - For data analysis

func scanEvents(path string, visit func(Event) error) error {
	file, err := os.Open(path)
	if err != nil {
		return err
	}
	defer file.Close()
	decoder := json.NewDecoder(file)
	var previous uint64
	for {
		var event Event
		if err := decoder.Decode(&event); err != nil {
			if err == io.EOF {
				return nil
			}
			return fmt.Errorf("decode WAL: %w", err)
		}
		if !VerifyChecksum(event) {
			return ErrChecksumMismatch
		}
		if event.Seq == 0 || (previous != 0 && event.Seq != previous+1) {
			return fmt.Errorf("non-contiguous WAL sequence %d after %d", event.Seq, previous)
		}
		if err := visit(event); err != nil {
			return err
		}
		previous = event.Seq
	}
}
