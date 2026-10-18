package raft

import (
	"crypto/sha256"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

// diskState is one crash-atomic image. Replacing it costs O(log size) per write.
type diskState struct {
	Version       int        `json:"version"`
	Term          int64      `json:"term"`
	Vote          string     `json:"vote"`
	Commit        int64      `json:"commit"`
	SnapshotIndex int64      `json:"snapshot_index"`
	SnapshotTerm  int64      `json:"snapshot_term"`
	Snapshot      []byte     `json:"snapshot"`
	Entries       []LogEntry `json:"entries"`
	Checksum      string     `json:"checksum"`
}

func (s diskState) checksum() (string, error) {
	s.Checksum = ""
	b, err := json.Marshal(s)
	if err != nil {
		return "", err
	}
	return fmt.Sprintf("%x", sha256.Sum256(b)), nil
}

func (s *diskState) validate() error {
	if s.Version != 1 {
		return fmt.Errorf("unsupported state version %d", s.Version)
	}
	if s.Term < 0 || s.SnapshotIndex < 0 || s.SnapshotTerm < 0 || s.Commit < s.SnapshotIndex || len(s.Entries) == 0 {
		return errors.New("invalid persisted raft bounds")
	}
	if s.Entries[0].Index != s.SnapshotIndex || s.Entries[0].Term != s.SnapshotTerm {
		return errors.New("invalid persisted snapshot boundary")
	}
	for i := 1; i < len(s.Entries); i++ {
		if s.Entries[i].Index != s.Entries[i-1].Index+1 || s.Entries[i].Term < s.Entries[i-1].Term {
			return errors.New("noncontiguous persisted raft log")
		}
	}
	if s.Commit > s.Entries[len(s.Entries)-1].Index {
		return errors.New("commit beyond persisted log")
	}
	return nil
}
func readDiskState(dir string) (diskState, error) {
	p := filepath.Join(dir, "raft-state.json")
	b, err := os.ReadFile(p)
	if errors.Is(err, os.ErrNotExist) {
		return diskState{Version: 1, Entries: []LogEntry{{}}}, nil
	}
	if err != nil {
		return diskState{}, err
	}
	var s diskState
	if err = json.Unmarshal(b, &s); err != nil {
		return diskState{}, fmt.Errorf("corrupt raft state: %w", err)
	}
	if err = s.validate(); err != nil {
		return diskState{}, err
	}
	want, err := s.checksum()
	if err != nil {
		return diskState{}, err
	}
	if s.Checksum != want {
		return diskState{}, errors.New("corrupt raft state checksum")
	}
	return s, nil
}
func writeDiskState(dir string, s diskState) error {
	if err := s.validate(); err != nil {
		return err
	}
	checksum, err := s.checksum()
	if err != nil {
		return err
	}
	s.Checksum = checksum
	b, err := json.Marshal(s)
	if err != nil {
		return err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return err
	}
	f, err := os.CreateTemp(dir, ".raft-state-*")
	if err != nil {
		return err
	}
	name := f.Name()
	defer os.Remove(name)
	if err = f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err = f.Write(b); err != nil {
		f.Close()
		return err
	}
	if err = f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err = f.Close(); err != nil {
		return err
	}
	if err = os.Rename(name, filepath.Join(dir, "raft-state.json")); err != nil {
		return err
	}
	d, err := os.Open(dir)
	if err != nil {
		return err
	}
	defer d.Close()
	return d.Sync()
}
