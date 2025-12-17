package raft

import (
	"errors"
	"sync"
)

var (
	ErrLogNotFound = errors.New("log entry not found")
	ErrIndexOutOfRange = errors.New("index out of range")
)

// LogStore defines the interface for persisting Raft logs
type LogStore interface {
	// FirstIndex returns the index of the first entry in the log
	FirstIndex() (int64, error)

	// LastIndex returns the index of the last entry in the log
	LastIndex() (int64, error)

	// GetLog returns the log entry at the given index
	GetLog(index int64) (*LogEntry, error)

	// StoreLog stores a single log entry
	StoreLog(entry *LogEntry) error

	// StoreLogs stores multiple log entries
	StoreLogs(entries []*LogEntry) error

	// DeleteRange deletes log entries in the range [min, max] (inclusive)
	DeleteRange(min, max int64) error
}

// MemoryLogStore is an in-memory implementation of LogStore (for testing/prototyping)
type MemoryLogStore struct {
	entries []LogEntry
	mu      sync.RWMutex
}

// NewMemoryLogStore creates a new MemoryLogStore
func NewMemoryLogStore() *MemoryLogStore {
	// Raft logs usually start at index 1. Index 0 is often a dummy entry.
	return &MemoryLogStore{
		entries: []LogEntry{{Term: 0, Index: 0}},
	}
}

func (m *MemoryLogStore) FirstIndex() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries[0].Index, nil
}

func (m *MemoryLogStore) LastIndex() (int64, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.entries[len(m.entries)-1].Index, nil
}

func (m *MemoryLogStore) GetLog(index int64) (*LogEntry, error) {
	m.mu.RLock()
	defer m.mu.RUnlock()
	
	first, _ := m.FirstIndex()
	offset := index - first
	if offset < 0 || offset >= int64(len(m.entries)) {
		return nil, ErrLogNotFound
	}
	return &m.entries[offset], nil
}

func (m *MemoryLogStore) StoreLog(entry *LogEntry) error {
	return m.StoreLogs([]*LogEntry{entry})
}

func (m *MemoryLogStore) StoreLogs(entries []*LogEntry) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	for _, entry := range entries {
		m.entries = append(m.entries, *entry)
	}
	return nil
}

func (m *MemoryLogStore) DeleteRange(min, max int64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	
	// Example: Delete from index 'min' to 'max'
	// This is a simple implementation for truncation
	newEntries := make([]LogEntry, 0)
	for _, entry := range m.entries {
		if entry.Index < min || entry.Index > max {
			newEntries = append(newEntries, entry)
		}
	}
	m.entries = newEntries
	return nil
}
