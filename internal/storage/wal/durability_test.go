package wal

import (
	"bytes"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

func TestFullJobReplayAndSequenceAfterRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.wal")
	w, err := NewWAL(path, false, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	job := &types.Job{ID: "payload-job", Payload: map[string]interface{}{"message": "original"}, Attempt: 2, WorkerID: "worker-1", Status: types.StatusInFlight}
	if err := w.Append(EventDispatch, job); err != nil {
		t.Fatal(err)
	}
	job.Payload["message"] = "mutated"
	if err := w.Close(); err != nil {
		t.Fatal(err)
	}
	w, err = NewWAL(path, false, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if w.GetLastSeq() != 1 {
		t.Fatal("sequence not recovered")
	}
	if err := w.Replay(func(e *Event) error {
		if e.Job == nil || e.Job.Payload["message"] != "original" || e.Job.Attempt != 2 || e.Job.WorkerID != "worker-1" {
			t.Fatalf("incomplete replay: %+v", e.Job)
		}
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if err := w.Rotate(); err != nil {
		t.Fatal(err)
	}
	if err := w.Append(EventAck, job); err != nil {
		t.Fatal(err)
	}
	if w.GetLastSeq() != 2 {
		t.Fatal("rotation reset sequence")
	}
}

func TestPayloadCorruptionFailsClosed(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.wal")
	w, err := NewWAL(path, false, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	if err := w.Append(EventEnqueue, &types.Job{ID: "job", Payload: map[string]interface{}{"value": "original"}}); err != nil {
		t.Fatal(err)
	}
	w.Close()
	b, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	b = bytes.ReplaceAll(b, []byte("original"), []byte("tampered"))
	if err := os.WriteFile(path, b, 0600); err != nil {
		t.Fatal(err)
	}
	if err := ValidateWAL(path); err == nil {
		t.Fatal("payload corruption accepted")
	}
	w, err = NewWAL(path, false, 1, time.Millisecond)
	if err != nil {
		t.Fatal(err)
	}
	defer w.Close()
	if err := w.Append(EventEnqueue, &types.Job{ID: "after-corruption"}); err == nil {
		t.Fatal("append accepted after corrupt recovery")
	}
}
