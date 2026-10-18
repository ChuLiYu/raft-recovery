package controller

import (
	"context"
	"encoding/json"
	"errors"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/raft"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

func TestRaftApplyWaitsAndReportsErrors(t *testing.T) {
	c, dir := createTestController(t)
	c.config.DisableDispatchLoop = true
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, c, dir)
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	cmd, err := raft.NewEnqueueCommand([]types.Job{{ID: "first", CreatedAt: 123, UpdatedAt: 123}})
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := c.Job("first"); ok {
		t.Fatal("uncommitted job visible")
	}
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, Command: cmd, CommandIndex: 1}
	if err := c.WaitApplied(ctx, 1); err != nil {
		t.Fatal(err)
	}
	if job, ok := c.Job("first"); !ok || job.CreatedAt != 123 {
		t.Fatalf("committed job absent: %+v %v", job, ok)
	}
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, CommandIndex: 2}
	if err := c.WaitApplied(ctx, 2); err != nil {
		t.Fatalf("no-op failed to advance: %v", err)
	}
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, Command: []byte("bad JSON"), CommandIndex: 3}
	if err := c.WaitApplied(ctx, 3); err == nil {
		t.Fatal("malformed command was reported as applied")
	}
}

func TestRaftSnapshotRestoresIndexAndFIFO(t *testing.T) {
	c, dir := createTestController(t)
	c.config.DisableDispatchLoop = true
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, c, dir)
	data := types.SnapshotData{SchemaVer: 3, LastSeq: 10, Jobs: map[types.JobID]*types.Job{
		"z":    {ID: "z", Status: types.StatusPending},
		"a":    {ID: "a", Status: types.StatusPending},
		"done": {ID: "done", Status: types.StatusCompleted},
	}, PendingOrder: []types.JobID{"z", "a"}}
	encoded, err := json.Marshal(data)
	if err != nil {
		t.Fatal(err)
	}
	c.GetApplyCh() <- raft.ApplyMsg{SnapshotValid: true, Snapshot: encoded, SnapshotIndex: 10}
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	if err := c.WaitApplied(ctx, 10); err != nil {
		t.Fatal(err)
	}
	pending := c.PendingJobs(2)
	if len(pending) != 2 || pending[0].ID != "z" || pending[1].ID != "a" {
		t.Fatalf("FIFO not restored: %+v", pending)
	}
	if _, ok := c.Job("done"); !ok {
		t.Fatal("completed tombstone missing")
	}
}

func TestCanceledAppliedResultIsDroppedBeforeLateApply(t *testing.T) {
	c, dir := createTestController(t)
	c.config.DisableDispatchLoop = true
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, c, dir)

	enqueue, err := raft.NewEnqueueCommand([]types.Job{{ID: "dispatch-me", CreatedAt: 1, UpdatedAt: 1}})
	if err != nil {
		t.Fatal(err)
	}
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, Command: enqueue, CommandIndex: 1}
	if err := c.WaitApplied(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	resultID, err := c.RegisterAppliedResult()
	if err != nil {
		t.Fatal(err)
	}
	command, err := raft.NewDispatchCommand(raft.DispatchPayload{
		JobID: "dispatch-me", WorkerID: "worker-1", Attempt: 1,
		At: 2, Deadline: 100, ResultID: resultID,
	})
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := c.WaitApplied(ctx, 2); !errors.Is(err, context.Canceled) {
		t.Fatalf("WaitApplied error = %v, want context.Canceled", err)
	}

	// The RPC has returned; the committed command may still arrive afterward.
	c.DiscardAppliedResult(resultID)
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, Command: command, CommandIndex: 2}
	if err := c.WaitApplied(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	c.mu.Lock()
	got := len(c.applyResults)
	c.mu.Unlock()
	if got != 0 {
		t.Fatalf("late apply retained %d unreachable results", got)
	}
	if job, ok := c.Job("dispatch-me"); !ok || job.Status != types.StatusInFlight {
		t.Fatalf("dispatch command was not applied: %+v, exists=%v", job, ok)
	}
}

func TestAppliedResultRemainsAvailableToWaitingRPC(t *testing.T) {
	c, dir := createTestController(t)
	c.config.DisableDispatchLoop = true
	if err := c.Start(); err != nil {
		t.Fatal(err)
	}
	defer cleanup(t, c, dir)

	enqueue, err := raft.NewEnqueueCommand([]types.Job{{ID: "dispatch-me", CreatedAt: 1, UpdatedAt: 1}})
	if err != nil {
		t.Fatal(err)
	}
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, Command: enqueue, CommandIndex: 1}
	if err := c.WaitApplied(context.Background(), 1); err != nil {
		t.Fatal(err)
	}

	resultID, err := c.RegisterAppliedResult()
	if err != nil {
		t.Fatal(err)
	}
	command, err := raft.NewDispatchCommand(raft.DispatchPayload{
		JobID: "dispatch-me", WorkerID: "worker-1", Attempt: 1,
		At: 2, Deadline: 100, ResultID: resultID,
	})
	if err != nil {
		t.Fatal(err)
	}
	c.GetApplyCh() <- raft.ApplyMsg{CommandValid: true, Command: command, CommandIndex: 2}
	if err := c.WaitApplied(context.Background(), 2); err != nil {
		t.Fatal(err)
	}
	jobs := c.AppliedJobs(resultID)
	if len(jobs) != 1 || jobs[0].ID != "dispatch-me" || jobs[0].WorkerID != "worker-1" {
		t.Fatalf("waiting RPC result was not preserved: %+v", jobs)
	}
	c.mu.Lock()
	got := len(c.applyResults)
	c.mu.Unlock()
	if got != 0 {
		t.Fatalf("consumed result was retained: %d entries", got)
	}
}
