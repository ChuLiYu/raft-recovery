package worker

import (
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// Task represents a task to execute
type Task struct {
	ID      types.JobID            // Task unique identifier
	Attempt int                    // Attempt number assigned by the master
	Payload map[string]interface{} // Data payload required for task execution
	Timeout time.Duration          // Execution timeout duration
}

// Result represents task execution result
type Result struct {
	JobID    types.JobID   // Task ID
	Attempt  int           // Attempt number, used to fence stale acknowledgements
	Success  bool          // Whether execution succeeded
	Error    error         // Error message (if any)
	Duration time.Duration // Actual execution time
}
