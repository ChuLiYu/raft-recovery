package raft

import (
	"errors"
	"fmt"
	"sync"
)

var ErrLogNotFound = errors.New("log entry not found")
var ErrIndexOutOfRange = errors.New("index out of range")

// LogStore contains a contiguous log suffix. Index zero is a virtual entry.
type LogStore interface {
	FirstIndex() (int64, error)
	LastIndex() (int64, error)
	GetLog(index int64) (*LogEntry, error)
	StoreLog(entry *LogEntry) error
	StoreLogs(entries []*LogEntry) error
	DeleteRange(min, max int64) error
}

type MemoryLogStore struct {
	mu      sync.RWMutex
	entries []LogEntry
	base    int64
}

func NewMemoryLogStore() *MemoryLogStore {
	return &MemoryLogStore{entries: []LogEntry{{Index: 0, Term: 0}}}
}
func (m *MemoryLogStore) FirstIndex() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.base, nil
}
func (m *MemoryLogStore) LastIndex() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.base + int64(len(m.entries)) - 1, nil
}
func (m *MemoryLogStore) GetLog(index int64) (*LogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	off := index - m.base
	if off < 0 || off >= int64(len(m.entries)) {
		return nil, ErrLogNotFound
	}
	e := m.entries[off]
	e.Command = append([]byte(nil), e.Command...)
	return &e, nil
}
func (m *MemoryLogStore) StoreLog(e *LogEntry) error { return m.StoreLogs([]*LogEntry{e}) }
func (m *MemoryLogStore) StoreLogs(entries []*LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	for _, p := range entries {
		if p == nil || p.Index != m.base+int64(len(m.entries)) {
			return fmt.Errorf("non-contiguous log append at %d", p.Index)
		}
		e := *p
		e.Command = append([]byte(nil), e.Command...)
		m.entries = append(m.entries, e)
	}
	return nil
}
func (m *MemoryLogStore) DeleteRange(min, max int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if min > max {
		return nil
	}
	last := m.base + int64(len(m.entries)) - 1
	if min <= m.base && max >= last {
		return errors.New("cannot erase entire log boundary")
	}
	if min <= m.base {
		if max >= last {
			return ErrIndexOutOfRange
		}
		m.entries = append([]LogEntry(nil), m.entries[max-m.base+1:]...)
		m.base = max + 1
		return nil
	}
	if max >= last {
		if min > last {
			return nil
		}
		m.entries = m.entries[:min-m.base]
		return nil
	}
	return errors.New("cannot delete middle of log")
}
func (m *MemoryLogStore) replace(base int64, entries []LogEntry) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.base = base
	m.entries = append([]LogEntry(nil), entries...)
}
