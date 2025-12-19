package raft

import (
	"log/slog"
	"math/rand"
	"sync"
	"time"
)

// State represents the Raft node state
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
	default:
		return "Unknown"
	}
}

// LogEntry represents a log entry (placeholder for now)
type LogEntry struct {
	Term    int64
	Index   int64
	Command []byte
}

// Config holds Raft configuration
type Config struct {
	ID              string
	Peers           []string // List of peer IDs/Addresses
	ElectionTimeout time.Duration
	HeartbeatInterval time.Duration
}

// Transport defines the interface for sending RPCs to peers
type Transport interface {
	SendRequestVote(peer string, args *RequestVoteArgs) (*RequestVoteReply, error)
	SendAppendEntries(peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error)
}

// Raft implements the Raft consensus algorithm
type Raft struct {
	mu sync.Mutex

	// Persistent state
	currentTerm int64
	votedFor    string
	logStore    LogStore
	
	// Snapshot metadata
	lastIncludedIndex int64
	lastIncludedTerm  int64

	// Volatile state
	state       State
	leaderID    string
	commitIndex int64
	lastApplied int64

	// Volatile state on leaders
	nextIndex  map[string]int64
	matchIndex map[string]int64

	// Channels
	applyCh chan ApplyMsg
	stopCh  chan struct{}

	config    Config
	transport Transport
	logger    *slog.Logger

	// Timers
	electionTimer  *time.Timer
	heartbeatTimer *time.Ticker
}

// ApplyMsg is used to send committed entries to the state machine
type ApplyMsg struct {
	CommandValid bool
	Command      []byte
	CommandIndex int64
}

// NewRaft creates a new Raft instance
func NewRaft(config Config, store LogStore, trans Transport, applyCh chan ApplyMsg) *Raft {
	rf := &Raft{
		state:          Follower,
		config:         config,
		logStore:       store,
		transport:      trans,
		applyCh:        applyCh,
		stopCh:         make(chan struct{}),
		logger:         slog.With("component", "raft", "id", config.ID),
		heartbeatTimer: time.NewTicker(config.HeartbeatInterval),
		nextIndex:      make(map[string]int64),
		matchIndex:     make(map[string]int64),
	}
	rf.electionTimer = time.NewTimer(rf.randomElectionTimeout())
	return rf
}

// Start starts the Raft node
func (rf *Raft) Start() {
	go rf.runElectionLoop()
	go rf.runHeartbeatLoop()
}

// ... (Stop and helpers remain same) ...

func (rf *Raft) runElectionLoop() {
	for {
		select {
		case <-rf.stopCh:
			return
		case <-rf.electionTimer.C:
			rf.mu.Lock()
			if rf.state != Leader {
				rf.startElection()
			}
			rf.resetElectionTimer()
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) runHeartbeatLoop() {
	for {
		select {
		case <-rf.stopCh:
			return
		case <-rf.heartbeatTimer.C:
			rf.mu.Lock()
			if rf.state == Leader {
				rf.broadcastHeartbeats()
			}
			rf.mu.Unlock()
		}
	}
}

func (rf *Raft) convertToFollower(term int64) {
	rf.state = Follower
	rf.currentTerm = term
	rf.votedFor = ""
	rf.resetElectionTimer()
}

func (rf *Raft) convertToLeader() {
	if rf.state == Leader {
		return
	}
	rf.state = Leader
	rf.logger.Info("Elected as leader", "term", rf.currentTerm)
	
	lastIndex, _ := rf.logStore.LastIndex()
	for _, peer := range rf.config.Peers {
		if peer == rf.config.ID {
			continue
		}
		rf.nextIndex[peer] = lastIndex + 1
		rf.matchIndex[peer] = 0
	}
	
	// Send initial empty AppendEntries RPCs (heartbeats) to each server
	rf.broadcastHeartbeats()
}

func (rf *Raft) broadcastHeartbeats() {
	for _, peer := range rf.config.Peers {
		if peer == rf.config.ID {
			continue
		}
		go rf.replicateToPeer(peer)
	}
}

func (rf *Raft) replicateToPeer(peer string) {
	rf.mu.Lock()
	if rf.state != Leader {
		rf.mu.Unlock()
		return
	}
	
	lastIndex, _ := rf.logStore.LastIndex()
	next := rf.nextIndex[peer]
	
	if next > lastIndex+1 {
		next = lastIndex + 1
	}
	
	prevIndex := next - 1
	prevTerm := int64(0)
	if prevIndex >= 0 {
		prevEntry, err := rf.logStore.GetLog(prevIndex)
		if err == nil {
			prevTerm = prevEntry.Term
		}
	}
	
	var entries []LogEntry
	if lastIndex >= next {
		for i := next; i <= lastIndex; i++ {
			entry, err := rf.logStore.GetLog(i)
			if err == nil {
				entries = append(entries, *entry)
			}
		}
	}
	
	args := &AppendEntriesArgs{
		Term:         rf.currentTerm,
		LeaderID:     rf.config.ID,
		PrevLogIndex: prevIndex,
		PrevLogTerm:  prevTerm,
		Entries:      entries,
		LeaderCommit: rf.commitIndex,
	}
	rf.mu.Unlock()
	
	reply, err := rf.transport.SendAppendEntries(peer, args)
	if err != nil {
		return
	}
	
	rf.mu.Lock()
	defer rf.mu.Unlock()
	
	if rf.state != Leader || args.Term != rf.currentTerm {
		return
	}
	
	if reply.Term > rf.currentTerm {
		rf.convertToFollower(reply.Term)
		return
	}
	
	if reply.Success {
		rf.matchIndex[peer] = prevIndex + int64(len(entries))
		rf.nextIndex[peer] = rf.matchIndex[peer] + 1
		rf.updateCommitIndex()
	} else {
		rf.nextIndex[peer]--
		if rf.nextIndex[peer] < 1 {
			rf.nextIndex[peer] = 1
		}
	}
}

func (rf *Raft) updateCommitIndex() {
	lastIndex, _ := rf.logStore.LastIndex()
	for n := lastIndex; n > rf.commitIndex; n-- {
		count := 1
		for _, peer := range rf.config.Peers {
			if peer != rf.config.ID && rf.matchIndex[peer] >= n {
				count++
			}
		}
		
		entry, err := rf.logStore.GetLog(n)
		if count > len(rf.config.Peers)/2 && err == nil && entry.Term == rf.currentTerm {
			rf.commitIndex = n
			go rf.applyLogs()
			break
		}
	}
}

func (rf *Raft) applyLogs() {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	
	for rf.commitIndex > rf.lastApplied {
		rf.lastApplied++
		entry, err := rf.logStore.GetLog(rf.lastApplied)
		if err == nil {
			msg := ApplyMsg{
				CommandValid: true,
				Command:      entry.Command,
				CommandIndex: entry.Index,
			}
			rf.applyCh <- msg
		}
	}
}

func (rf *Raft) startElection() {
	rf.state = Candidate
	rf.currentTerm++
	rf.votedFor = rf.config.ID
	
	lastIndex, _ := rf.logStore.LastIndex()
	lastLog, _ := rf.logStore.GetLog(lastIndex)
	
	args := &RequestVoteArgs{
		Term:         rf.currentTerm,
		CandidateID:  rf.config.ID,
		LastLogIndex: lastIndex,
		LastLogTerm:  lastLog.Term,
	}
	
	votes := 1
	rf.logger.Info("Starting election", "term", rf.currentTerm)

	for _, peer := range rf.config.Peers {
		if peer == rf.config.ID {
			continue
		}
		
		go func(p string) {
			reply, err := rf.transport.SendRequestVote(p, args)
			if err != nil {
				return
			}
			
			rf.mu.Lock()
			defer rf.mu.Unlock()
			
			if rf.state != Candidate || args.Term != rf.currentTerm {
				return
			}
			
			if reply.Term > rf.currentTerm {
				rf.convertToFollower(reply.Term)
				return
			}
			
			if reply.VoteGranted {
				votes++
				if votes > len(rf.config.Peers)/2 {
					rf.convertToLeader()
				}
			}
		}(peer)
	}
}

func (rf *Raft) resetElectionTimer() {
	if !rf.electionTimer.Stop() {
		select {
		case <-rf.electionTimer.C:
		default:
		}
	}
	rf.electionTimer.Reset(rf.randomElectionTimeout())
}

func (rf *Raft) randomElectionTimeout() time.Duration {
	extra := time.Duration(rand.Int63n(int64(rf.config.ElectionTimeout)))
	return rf.config.ElectionTimeout + extra
}

func (rf *Raft) Stop() {
	close(rf.stopCh)
	rf.heartbeatTimer.Stop()
	rf.electionTimer.Stop()
}

// Propose submits a new command to the Raft log
// Returns index, term, and true if this node is the leader
func (rf *Raft) Propose(command []byte) (int64, int64, bool) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	
	if rf.state != Leader {
		return -1, -1, false
	}
	
	lastIndex, _ := rf.logStore.LastIndex()
	newIndex := lastIndex + 1
	entry := &LogEntry{
		Term:    rf.currentTerm,
		Index:   newIndex,
		Command: command,
	}
	
	rf.logStore.StoreLog(entry)
	rf.logger.Debug("New proposal", "index", newIndex, "term", rf.currentTerm)
	
	// Start replicating immediately
	rf.broadcastHeartbeats()
	
	return newIndex, rf.currentTerm, true
}

// Snapshot truncates the log up to index and saves snapshot data.
func (rf *Raft) Snapshot(index int64, snapshot []byte) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	
	if index <= rf.lastIncludedIndex {
		return
	}
	
	entry, err := rf.logStore.GetLog(index)
	if err != nil {
		return
	}
	
	rf.lastIncludedIndex = index
	rf.lastIncludedTerm = entry.Term
	
	// Compact LogStore
	firstIndex, _ := rf.logStore.FirstIndex()
	rf.logStore.DeleteRange(firstIndex, index)
	
	rf.logger.Info("Raft log compacted", "lastIncludedIndex", index)
}
