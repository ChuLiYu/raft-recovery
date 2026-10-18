package jobmanager

import (
	"encoding/json"
	"reflect"
	"testing"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

func TestReplicatedFencingAndSnapshot(t *testing.T) {
	jm := NewJobManager()
	for _, id := range []types.JobID{"a", "b", "c"} {
		if err := jm.ApplyEnqueue(types.Job{ID: id, CreatedAt: 100, UpdatedAt: 100}); err != nil {
			t.Fatal(err)
		}
	}
	if _, ok, err := jm.ApplyDispatch("b", "w", 1, 200, 300); err != nil || ok {
		t.Fatalf("out-of-order dispatch accepted: %v %v", ok, err)
	}
	if _, ok, err := jm.ApplyDispatch("a", "w", 1, 200, 300); err != nil || !ok {
		t.Fatalf("dispatch failed: %v %v", ok, err)
	}
	if got := jm.Expired(300); len(got) != 1 || got[0].ID != "a" {
		t.Fatalf("dispatch not indexed for timeout: %+v", got)
	}
	if ok, err := jm.ApplyTimeout("a", 1, 299, 2); err != nil || ok {
		t.Fatalf("early timeout: %v %v", ok, err)
	}
	if ok, err := jm.ApplyTimeout("a", 1, 300, 2); err != nil || !ok {
		t.Fatalf("timeout failed: %v %v", ok, err)
	}
	if got := jm.Pending(3); !reflect.DeepEqual([]types.JobID{got[0].ID, got[1].ID, got[2].ID}, []types.JobID{"b", "c", "a"}) {
		t.Fatalf("FIFO order: %v", got)
	}
	if _, ok, err := jm.ApplyDispatch("b", "w", 1, 310, 400); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := jm.ApplyAck("b", "w", 1, types.StatusCompleted, 320, 2); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := jm.ApplyAck("b", "w", 1, types.StatusCompleted, 321, 2); err != nil || !ok {
		t.Fatalf("duplicate completion ACK rejected: %v %v", ok, err)
	}
	if _, ok, err := jm.ApplyDispatch("c", "w", 1, 330, 430); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := jm.ApplyTimeout("c", 1, 430, 1); err != nil || !ok {
		t.Fatal(err)
	}
	if _, ok, err := jm.ApplyDispatch("a", "w2", 2, 440, 540); err != nil || !ok {
		t.Fatal(err)
	}
	if ok, err := jm.ApplyAck("a", "w2", 2, types.StatusCompleted, 540, 2); err != nil || ok {
		t.Fatalf("expired ACK accepted: %v %v", ok, err)
	}
	if ok, err := jm.ApplyAck("a", "w", 1, types.StatusCompleted, 450, 2); err != nil || ok {
		t.Fatalf("stale ACK accepted: %v %v", ok, err)
	}
	if ok, err := jm.ApplyAck("a", "w2", 2, types.StatusCompleted, 460, 2); err != nil || !ok {
		t.Fatal(err)
	}
	jm.AdvanceApplied(42)
	snapshot := jm.PartialSnapshot()
	encoded, err := json.Marshal(snapshot)
	if err != nil {
		t.Fatal(err)
	}
	var decoded types.SnapshotData
	if err := json.Unmarshal(encoded, &decoded); err != nil {
		t.Fatal(err)
	}
	restored := NewJobManager()
	if err := restored.Restore(decoded); err != nil {
		t.Fatal(err)
	}
	if restored.GetLastAppliedIndex() != 42 || !restored.IsCompleted("a") || !restored.IsCompleted("b") || !restored.IsDead("c") {
		t.Fatalf("snapshot lost terminal state: %+v", restored.Stats())
	}
	if ok, err := restored.ApplyAck("b", "w", 1, types.StatusCompleted, 999, 2); err != nil || !ok {
		t.Fatalf("completion ACK retry lost across compaction: %v %v", ok, err)
	}
	if err := restored.ApplyEnqueue(types.Job{ID: "a"}); err != nil {
		t.Fatal(err)
	}
	if restored.GetTotalJobs() != 3 {
		t.Fatalf("completed ID was re-enqueued")
	}
}

func TestReplicatedPendingFIFOAcrossSnapshot(t *testing.T) {
	jm := NewJobManager()
	for _, id := range []types.JobID{"z", "a", "m"} {
		if err := jm.ApplyEnqueue(types.Job{ID: id, CreatedAt: 1}); err != nil {
			t.Fatal(err)
		}
	}
	copy := NewJobManager()
	if err := copy.Restore(jm.PartialSnapshot()); err != nil {
		t.Fatal(err)
	}
	got := copy.Pending(3)
	if len(got) != 3 || got[0].ID != "z" || got[1].ID != "a" || got[2].ID != "m" {
		t.Fatalf("order changed: %+v", got)
	}
}

func TestReplicatedSnapshotRejectsMissingFIFOEntry(t *testing.T) {
	jm := NewJobManager()
	err := jm.Restore(types.SnapshotData{SchemaVer: 3, Jobs: map[types.JobID]*types.Job{
		"first":  {ID: "first", Status: types.StatusPending},
		"second": {ID: "second", Status: types.StatusPending},
	}, PendingOrder: []types.JobID{"first"}})
	if err == nil {
		t.Fatal("incomplete replicated FIFO was silently restored")
	}
	if jm.GetTotalJobs() != 0 {
		t.Fatal("failed restore modified live state")
	}
}
