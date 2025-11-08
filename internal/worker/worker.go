// ============================================================================
// Beaver-Raft Worker - Task Execution Unit
// ============================================================================
//
// Package: internal/worker
// File: worker.go
// Function: Work unit that actually executes tasks, each Worker runs in an independent goroutine
//
// How it works:
//   Each Worker is an independent goroutine that continuously executes the following loop:
//   1. Receive task from taskCh (blocking wait)
//   2. Execute task logic (with timeout control)
//   3. Send result to resultCh
//   4. Repeat above process until taskCh is closed
//
// Execution Model:
//   ┌─────────────────────────────────────┐
//   │  Worker Goroutine                   │
//   │  ┌──────────────────────────────┐   │
//   │  │ for task := range taskCh     │   │
//   │  │   ├─ Context with timeout    │   │
//   │  │   ├─ execute(task)            │   │
//   │  │   └─ send result to resultCh │   │
//   │  └──────────────────────────────┘   │
//   └─────────────────────────────────────┘
//
// Timeout Control:
//   Use context.WithTimeout to ensure tasks don't execute indefinitely:
//   - Each task has independent Context
//   - After timeout, Context.Done() channel closes
//   - execute() method monitors Done() channel
//   - Timeout returns context.DeadlineExceeded error
//
// Task Execution Logic (Simulation):
//   Current implementation is for testing simulation:
//   - Random delay 0-500ms (simulate CPU-intensive work)
//   - 10% failure rate (simulate errors in real environment)
//   - Production environment should replace with actual business logic
//
// Error Handling:
//   - Timeout error: ctx.Err() returns DeadlineExceeded
//   - Execution failure: Returns custom error
//   - All errors are encapsulated in Result and returned
//
// Resource Management:
//   - Context calls cancel() after each task execution for release
//   - Avoid Context leak and resource waste
//   - Worker automatically cleans up all resources on exit
//
// ============================================================================

package worker

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Worker represents a work execution unit
// Each Worker runs in an independent goroutine, receives tasks from task channel and executes them
type Worker struct {
	id       int           // Worker unique identifier, used for logging and debugging
	taskCh   <-chan Task   // Task channel (read-only), receives tasks to execute
	resultCh chan<- Result // Result channel (write-only), sends task execution results
}

// newWorker creates a new Worker instance
func newWorker(id int, taskCh <-chan Task, resultCh chan<- Result) *Worker {
	return &Worker{
		id:       id,
		taskCh:   taskCh,
		resultCh: resultCh,
	}
}

// Run is the main loop of Worker, receives tasks from task channel and executes them
// After each task execution, sends the result to result channel
func (w *Worker) Run() {
	for task := range w.taskCh {
		start := time.Now()

		// Create Context with timeout, ensure task won't execute longer than specified time
		ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)

		// Execute task logic, and release Context resources after completion
		err := w.execute(ctx, task.Payload)
		cancel() // Release resources

		// Encapsulate execution result as Result struct
		result := Result{
			JobID:    task.ID,
			Success:  err == nil,
			Error:    err,
			Duration: time.Since(start),
		}

		// Attempt to send result to result channel
		select {
		case w.resultCh <- result:
			// Successfully reported result
		default:
			// If result channel is full or closed, ignore (rare case)
			// In production environment, should log for troubleshooting
		}
	}
}

// execute executes the actual task logic
// - Uses Context with timeout to ensure task won't execute indefinitely
// - Simulated work logic includes random delay and 10% failure rate
func (w *Worker) execute(ctx context.Context, payload map[string]interface{}) error {
	// Simulate CPU-intensive work, random delay 0-500 milliseconds
	workDuration := time.Duration(rand.Intn(500)) * time.Millisecond

	select {
	case <-ctx.Done():
		// If Context is cancelled or timed out, return corresponding error
		return ctx.Err()

	case <-time.After(workDuration):
		// Simulate 10% failure rate
		if rand.Intn(100) < 10 {
			return errors.New("simulated execution failure")
		}
		return nil // Successful execution
	}
}
