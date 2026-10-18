package raft

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

type State int

const (
	Follower State = iota
	Candidate
	Leader
)

func (s State) String() string {
	switch s {
	case Follower:
		return "Follower"
	case Candidate:
		return "Candidate"
	case Leader:
		return "Leader"
	}
	return "Unknown"
}

type LogEntry struct {
	Term, Index int64
	Command     []byte
}
type Config struct {
	ID                                 string
	Peers                              []string
	ElectionTimeout, HeartbeatInterval time.Duration
}
type Transport interface {
	SendRequestVote(string, *RequestVoteArgs) (*RequestVoteReply, error)
	SendAppendEntries(string, *AppendEntriesArgs) (*AppendEntriesReply, error)
}
type snapshotTransport interface {
	SendInstallSnapshot(string, *InstallSnapshotArgs) (*InstallSnapshotReply, error)
}
type ApplyMsg struct {
	CommandValid                bool
	Command                     []byte
	CommandIndex, CommandTerm   int64
	SnapshotValid               bool
	Snapshot                    []byte
	SnapshotIndex, SnapshotTerm int64
}

var ErrNotLeader = errors.New("not raft leader")
var ErrStopped = errors.New("raft stopped")

type Raft struct {
	mu                                  sync.Mutex
	applyMu                             sync.Mutex
	config                              Config
	transport                           Transport
	logStore                            LogStore
	diskDir                             string
	logger                              *slog.Logger
	currentTerm                         int64
	votedFor                            string
	entries                             []LogEntry
	lastIncludedIndex, lastIncludedTerm int64
	snapshot                            []byte
	state                               State
	leaderID                            string
	commitIndex, lastApplied            int64
	nextIndex, matchIndex               map[string]int64
	electionDeadline                    time.Time
	votes                               int
	commitNotify                        chan struct{}
	applyWake                           chan struct{}
	replicateWake                       map[string]chan struct{}
	stopCh                              chan struct{}
	startOnce, stopOnce                 sync.Once
	started                             bool
	fatal                               error
	applyCh                             chan ApplyMsg
	pendingSnapshot                     bool
}

func normalize(c Config) Config {
	if c.ElectionTimeout <= 0 {
		c.ElectionTimeout = 500 * time.Millisecond
	}
	if c.HeartbeatInterval <= 0 {
		c.HeartbeatInterval = 100 * time.Millisecond
	}
	seen := map[string]bool{}
	p := make([]string, 0, len(c.Peers)+1)
	for _, id := range append([]string{c.ID}, c.Peers...) {
		if id != "" && !seen[id] {
			seen[id] = true
			p = append(p, id)
		}
	}
	c.Peers = p
	return c
}
func NewRaft(c Config, store LogStore, trans Transport, applyCh chan ApplyMsg) *Raft {
	rf, err := newRaft(normalize(c), store, trans, applyCh)
	if err != nil {
		panic(err)
	}
	return rf
}
func NewPersistentRaft(c Config, dir string, trans Transport, applyCh chan ApplyMsg) (*Raft, error) {
	if dir == "" {
		return nil, errors.New("empty raft data directory")
	}
	s, err := readDiskState(dir)
	if err != nil {
		return nil, err
	}
	store := NewMemoryLogStore()
	store.mu.Lock()
	store.entries = append([]LogEntry(nil), s.Entries...)
	store.base = s.SnapshotIndex
	store.mu.Unlock()
	rf, err := newRaft(normalize(c), store, trans, applyCh)
	if err != nil {
		return nil, err
	}
	rf.diskDir = dir
	rf.currentTerm = s.Term
	rf.votedFor = s.Vote
	rf.commitIndex = s.Commit
	rf.lastIncludedIndex = s.SnapshotIndex
	rf.lastIncludedTerm = s.SnapshotTerm
	rf.snapshot = append([]byte(nil), s.Snapshot...)
	rf.lastApplied = s.SnapshotIndex
	rf.pendingSnapshot = s.SnapshotIndex > 0
	return rf, nil
}
func newRaft(c Config, store LogStore, trans Transport, applyCh chan ApplyMsg) (*Raft, error) {
	if c.ID == "" || store == nil || trans == nil || applyCh == nil {
		return nil, errors.New("invalid raft configuration")
	}
	first, err := store.FirstIndex()
	if err != nil {
		return nil, err
	}
	last, err := store.LastIndex()
	if err != nil {
		return nil, err
	}
	if last < first {
		return nil, errors.New("empty log store")
	}
	entries := make([]LogEntry, 0, last-first+1)
	for i := first; i <= last; i++ {
		e, err := store.GetLog(i)
		if err != nil {
			return nil, err
		}
		if e.Index != i {
			return nil, errors.New("noncontiguous log store")
		}
		entries = append(entries, *e)
	}
	rf := &Raft{config: c, transport: trans, logStore: store, applyCh: applyCh, logger: slog.With("component", "raft", "id", c.ID), entries: entries, lastIncludedIndex: first, lastIncludedTerm: entries[0].Term, commitIndex: first, lastApplied: first, state: Follower, nextIndex: map[string]int64{}, matchIndex: map[string]int64{}, commitNotify: make(chan struct{}), applyWake: make(chan struct{}, 1), replicateWake: map[string]chan struct{}{}, stopCh: make(chan struct{})}
	rf.resetElectionLocked()
	for _, p := range c.Peers {
		if p != c.ID {
			rf.replicateWake[p] = make(chan struct{}, 1)
		}
	}
	return rf, nil
}
func (rf *Raft) lastIndexLocked() int64 { return rf.lastIncludedIndex + int64(len(rf.entries)) - 1 }
func (rf *Raft) termAtLocked(index int64) (int64, bool) {
	if index < rf.lastIncludedIndex || index > rf.lastIndexLocked() {
		return 0, false
	}
	return rf.entries[index-rf.lastIncludedIndex].Term, true
}
func (rf *Raft) lastTermLocked() int64 { return rf.entries[len(rf.entries)-1].Term }
func (rf *Raft) persistLocked() error {
	if rf.diskDir == "" {
		return nil
	}
	s := diskState{Version: 1, Term: rf.currentTerm, Vote: rf.votedFor, Commit: rf.commitIndex, SnapshotIndex: rf.lastIncludedIndex, SnapshotTerm: rf.lastIncludedTerm, Snapshot: rf.snapshot, Entries: rf.entries}
	if err := writeDiskState(rf.diskDir, s); err != nil {
		rf.fatal = err
		rf.state = Follower
		rf.leaderID = ""
		rf.signalCommitLocked()
		return err
	}
	return nil
}
func (rf *Raft) signalCommitLocked() {
	close(rf.commitNotify)
	rf.commitNotify = make(chan struct{})
	select {
	case rf.applyWake <- struct{}{}:
	default:
	}
}
func (rf *Raft) resetElectionLocked() {
	rf.electionDeadline = time.Now().Add(rf.config.ElectionTimeout + time.Duration(rand.Int63n(int64(rf.config.ElectionTimeout))))
}
func (rf *Raft) Start() {
	rf.startOnce.Do(func() {
		rf.mu.Lock()
		rf.started = true
		rf.mu.Unlock()
		go rf.runClock()
		go rf.runApplier()
		for p, ch := range rf.replicateWake {
			go rf.runReplicator(p, ch)
		}
		select {
		case rf.applyWake <- struct{}{}:
		default:
		}
	})
}
func (rf *Raft) Stop() {
	rf.stopOnce.Do(func() {
		close(rf.stopCh)
		rf.mu.Lock()
		rf.state = Follower
		rf.leaderID = ""
		rf.signalCommitLocked()
		rf.mu.Unlock()
	})
}
func (rf *Raft) stoppedLocked() bool {
	select {
	case <-rf.stopCh:
		return true
	default:
		return false
	}
}
func (rf *Raft) GetState() (int64, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	return rf.currentTerm, rf.state == Leader && rf.fatal == nil
}
func (rf *Raft) LeaderID() string { rf.mu.Lock(); defer rf.mu.Unlock(); return rf.leaderID }
func (rf *Raft) runClock() {
	period := rf.config.HeartbeatInterval
	if period > 25*time.Millisecond {
		period = 25 * time.Millisecond
	}
	ticker := time.NewTicker(period)
	defer ticker.Stop()
	for {
		select {
		case <-rf.stopCh:
			return
		case <-ticker.C:
			rf.mu.Lock()
			if rf.fatal == nil && !rf.stoppedLocked() {
				if rf.state != Leader && time.Now().After(rf.electionDeadline) {
					rf.startElectionLocked()
				} else if rf.state == Leader {
					rf.wakeReplicationLocked()
				}
			}
			rf.mu.Unlock()
		}
	}
}
func (rf *Raft) startElectionLocked() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.config.ID
	rf.leaderID = ""
	rf.votes = 1
	rf.resetElectionLocked()
	if rf.persistLocked() != nil {
		return
	}
	term := rf.currentTerm
	args := &RequestVoteArgs{Term: term, CandidateID: rf.config.ID, LastLogIndex: rf.lastIndexLocked(), LastLogTerm: rf.lastTermLocked()}
	rf.signalCommitLocked()
	if rf.votes > len(rf.config.Peers)/2 {
		rf.becomeLeaderLocked()
		return
	}
	for _, p := range rf.config.Peers {
		if p == rf.config.ID {
			continue
		}
		go func(peer string) {
			reply, err := rf.transport.SendRequestVote(peer, args)
			if err != nil || reply == nil {
				return
			}
			rf.mu.Lock()
			defer rf.mu.Unlock()
			if rf.stoppedLocked() {
				return
			}
			if reply.Term > rf.currentTerm {
				rf.stepDownLocked(reply.Term, "")
				return
			}
			if rf.state != Candidate || rf.currentTerm != term || rf.fatal != nil {
				return
			}
			if reply.VoteGranted && reply.Term == term {
				rf.votes++
				if rf.votes > len(rf.config.Peers)/2 {
					rf.becomeLeaderLocked()
				}
			}
		}(p)
	}
}
func (rf *Raft) stepDownLocked(term int64, leader string) {
	if term > rf.currentTerm {
		rf.currentTerm = term
		rf.votedFor = ""
		if rf.persistLocked() != nil {
			return
		}
	}
	rf.state = Follower
	rf.leaderID = leader
	rf.resetElectionLocked()
	rf.signalCommitLocked()
}
func (rf *Raft) becomeLeaderLocked() {
	if rf.state == Leader || rf.fatal != nil {
		return
	}
	rf.state = Leader
	rf.leaderID = rf.config.ID
	last := rf.lastIndexLocked()
	for p := range rf.replicateWake {
		rf.nextIndex[p] = last + 1
		rf.matchIndex[p] = 0
	}
	if err := rf.appendLocked(LogEntry{Term: rf.currentTerm, Index: last + 1}); err != nil {
		return
	}
	rf.updateCommitLocked()
	rf.wakeReplicationLocked()
	rf.logger.Info("elected leader", "term", rf.currentTerm)
}
func (rf *Raft) appendLocked(e LogEntry) error {
	if err := rf.logStore.StoreLog(&e); err != nil {
		rf.fatal = err
		rf.state = Follower
		rf.signalCommitLocked()
		return err
	}
	rf.entries = append(rf.entries, e)
	return rf.persistLocked()
}
func (rf *Raft) wakeReplicationLocked() {
	for _, ch := range rf.replicateWake {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}
func (rf *Raft) runReplicator(peer string, wake <-chan struct{}) {
	for {
		select {
		case <-rf.stopCh:
			return
		case <-wake:
			for {
				if !rf.replicateOnce(peer) {
					break
				}
			}
		}
	}
}

// replicateOnce performs at most one RPC. A dedicated goroutine per peer serializes requests.
func (rf *Raft) replicateOnce(peer string) bool {
	rf.mu.Lock()
	if rf.stoppedLocked() || rf.state != Leader || rf.fatal != nil {
		rf.mu.Unlock()
		return false
	}
	term := rf.currentTerm
	next := rf.nextIndex[peer]
	if next < 1 {
		next = 1
	}
	if next <= rf.lastIncludedIndex {
		tr, ok := rf.transport.(snapshotTransport)
		if !ok {
			rf.mu.Unlock()
			return false
		}
		args := &InstallSnapshotArgs{Term: term, LeaderID: rf.config.ID, LastIncludedIndex: rf.lastIncludedIndex, LastIncludedTerm: rf.lastIncludedTerm, Data: append([]byte(nil), rf.snapshot...)}
		rf.mu.Unlock()
		reply, err := tr.SendInstallSnapshot(peer, args)
		if err != nil || reply == nil {
			return false
		}
		rf.mu.Lock()
		defer rf.mu.Unlock()
		if rf.stoppedLocked() {
			return false
		}
		if reply.Term > rf.currentTerm {
			rf.stepDownLocked(reply.Term, "")
			return false
		}
		if rf.state != Leader || rf.currentTerm != term {
			return false
		}
		if !reply.Success {
			return false
		}
		if rf.matchIndex[peer] < args.LastIncludedIndex {
			rf.matchIndex[peer] = args.LastIncludedIndex
		}
		if rf.nextIndex[peer] < args.LastIncludedIndex+1 {
			rf.nextIndex[peer] = args.LastIncludedIndex + 1
		}
		return true
	}
	last := rf.lastIndexLocked()
	if next > last+1 {
		next = last + 1
	}
	prev := next - 1
	pt, _ := rf.termAtLocked(prev)
	entries := make([]LogEntry, 0)
	for i := next; i <= last && len(entries) < 64; i++ {
		e := rf.entries[i-rf.lastIncludedIndex]
		e.Command = append([]byte(nil), e.Command...)
		entries = append(entries, e)
	}
	args := &AppendEntriesArgs{Term: term, LeaderID: rf.config.ID, PrevLogIndex: prev, PrevLogTerm: pt, Entries: entries, LeaderCommit: rf.commitIndex}
	rf.mu.Unlock()
	reply, err := rf.transport.SendAppendEntries(peer, args)
	if err != nil || reply == nil {
		return false
	}
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.stoppedLocked() {
		return false
	}
	if reply.Term > rf.currentTerm {
		rf.stepDownLocked(reply.Term, "")
		return false
	}
	if rf.state != Leader || rf.currentTerm != term {
		return false
	}
	if reply.Success {
		matched := prev + int64(len(entries))
		if matched > rf.matchIndex[peer] {
			rf.matchIndex[peer] = matched
		}
		if matched+1 > rf.nextIndex[peer] {
			rf.nextIndex[peer] = matched + 1
		}
		rf.updateCommitLocked()
		return rf.nextIndex[peer] <= rf.lastIndexLocked()
	}
	oldNext := rf.nextIndex[peer]
	if reply.ConflictIndex > 0 {
		rf.nextIndex[peer] = reply.ConflictIndex
	} else if rf.nextIndex[peer] > 1 {
		rf.nextIndex[peer]--
	}
	if rf.nextIndex[peer] > rf.lastIndexLocked()+1 {
		rf.nextIndex[peer] = rf.lastIndexLocked() + 1
	}
	return rf.nextIndex[peer] != oldNext
}
func (rf *Raft) updateCommitLocked() {
	if rf.state != Leader {
		return
	}
	for n := rf.lastIndexLocked(); n > rf.commitIndex; n-- {
		t, _ := rf.termAtLocked(n)
		if t != rf.currentTerm {
			continue
		}
		count := 1
		for p := range rf.replicateWake {
			if rf.matchIndex[p] >= n {
				count++
			}
		}
		if count > len(rf.config.Peers)/2 {
			rf.commitIndex = n
			if rf.persistLocked() == nil {
				rf.signalCommitLocked()
				rf.wakeReplicationLocked()
			}
			return
		}
	}
}
func (rf *Raft) runApplier() {
	for {
		select {
		case <-rf.stopCh:
			return
		case <-rf.applyWake:
		}
		for {
			rf.applyMu.Lock()
			rf.mu.Lock()
			var msg ApplyMsg
			if rf.pendingSnapshot {
				msg = ApplyMsg{SnapshotValid: true, Snapshot: append([]byte(nil), rf.snapshot...), SnapshotIndex: rf.lastIncludedIndex, SnapshotTerm: rf.lastIncludedTerm}
				rf.pendingSnapshot = false
			} else if rf.lastApplied < rf.commitIndex {
				next := rf.lastApplied + 1
				if next <= rf.lastIncludedIndex {
					rf.lastApplied = rf.lastIncludedIndex
					rf.mu.Unlock()
					rf.applyMu.Unlock()
					continue
				}
				e := rf.entries[next-rf.lastIncludedIndex]
				msg = ApplyMsg{CommandValid: true, Command: append([]byte(nil), e.Command...), CommandIndex: e.Index, CommandTerm: e.Term}
			} else {
				rf.mu.Unlock()
				rf.applyMu.Unlock()
				break
			}
			rf.mu.Unlock()
			select {
			case <-rf.stopCh:
				rf.applyMu.Unlock()
				return
			case rf.applyCh <- msg:
			}
			rf.mu.Lock()
			if msg.SnapshotValid {
				if rf.lastApplied < msg.SnapshotIndex {
					rf.lastApplied = msg.SnapshotIndex
				}
			} else if rf.lastApplied < msg.CommandIndex {
				rf.lastApplied = msg.CommandIndex
			}
			rf.mu.Unlock()
			rf.applyMu.Unlock()
		}
	}
}
func (rf *Raft) ProposeContext(ctx context.Context, command []byte) (int64, int64, error) {
	rf.mu.Lock()
	select {
	case <-rf.stopCh:
		rf.mu.Unlock()
		return 0, 0, ErrStopped
	default:
	}
	if rf.fatal != nil {
		err := rf.fatal
		rf.mu.Unlock()
		return 0, 0, err
	}
	if rf.state != Leader {
		rf.mu.Unlock()
		return 0, 0, ErrNotLeader
	}
	index := rf.lastIndexLocked() + 1
	term := rf.currentTerm
	e := LogEntry{Index: index, Term: term, Command: append([]byte(nil), command...)}
	if err := rf.appendLocked(e); err != nil {
		rf.mu.Unlock()
		return 0, 0, err
	}
	rf.updateCommitLocked()
	rf.wakeReplicationLocked()
	for {
		entryTerm, exists := rf.termAtLocked(index)
		if rf.commitIndex >= index && exists && entryTerm == term && rf.fatal == nil {
			rf.mu.Unlock()
			return index, term, nil
		}
		if rf.state != Leader || rf.currentTerm != term || rf.fatal != nil || !exists || entryTerm != term {
			rf.mu.Unlock()
			return index, term, ErrNotLeader
		}
		notify := rf.commitNotify
		rf.mu.Unlock()
		select {
		case <-ctx.Done():
			return index, term, ctx.Err()
		case <-rf.stopCh:
			return index, term, ErrStopped
		case <-notify:
		}
		rf.mu.Lock()
	}
}
func (rf *Raft) Propose(command []byte) (int64, int64, bool) {
	i, t, err := rf.ProposeContext(context.Background(), command)
	return i, t, err == nil
}
func (rf *Raft) Snapshot(index int64, data []byte) error {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	if rf.fatal != nil {
		return rf.fatal
	}
	if index <= rf.lastIncludedIndex {
		return nil
	}
	if index > rf.lastApplied || index > rf.commitIndex {
		return fmt.Errorf("snapshot index %d not applied", index)
	}
	term, ok := rf.termAtLocked(index)
	if !ok {
		return ErrLogNotFound
	}
	old := rf.lastIncludedIndex
	newEntries := append([]LogEntry(nil), rf.entries[index-old:]...)
	newEntries[0].Command = nil
	rf.entries = newEntries
	rf.lastIncludedIndex = index
	rf.lastIncludedTerm = term
	rf.snapshot = append([]byte(nil), data...)
	if err := rf.persistLocked(); err != nil {
		return err
	}
	if err := rf.logStore.DeleteRange(old, index-1); err != nil {
		rf.fatal = err
		rf.state = Follower
		return err
	}
	rf.wakeReplicationLocked()
	return nil
}
