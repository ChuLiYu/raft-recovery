package worker

import (
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// Task represents a task to execute
type Task struct {
	ID      types.JobID            // Task unique identifier
	Payload map[string]interface{} // Data payload required for task execution
	Timeout time.Duration          // Execution timeout duration
}

// Result represents task execution result
type Result struct {
	JobID    types.JobID   // Task ID
	Success  bool          // Whether execution succeeded
	Error    error         // Error message (if any)
	Duration time.Duration // Actual execution time
}
