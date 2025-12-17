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

	// Volatile state
	state       State
	leaderID    string
	commitIndex int64
	lastApplied int64

	// Channels
	stopCh chan struct{}

	config    Config
	transport Transport
	logger    *slog.Logger

	// Timers
	electionTimer  *time.Timer
	heartbeatTimer *time.Ticker
}

// NewRaft creates a new Raft instance
func NewRaft(config Config, store LogStore, trans Transport) *Raft {
	rf := &Raft{
		state:          Follower,
		config:         config,
		logStore:       store,
		transport:      trans,
		stopCh:         make(chan struct{}),
		logger:         slog.With("component", "raft", "id", config.ID),
		heartbeatTimer: time.NewTicker(config.HeartbeatInterval),
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

func (rf *Raft) broadcastHeartbeats() {
	for _, peer := range rf.config.Peers {
		if peer == rf.config.ID {
			continue
		}
		
		lastIndex, _ := rf.logStore.LastIndex()
		lastLog, _ := rf.logStore.GetLog(lastIndex)
		
		args := &AppendEntriesArgs{
			Term:         rf.currentTerm,
			LeaderID:     rf.config.ID,
			PrevLogIndex: lastIndex,
			PrevLogTerm:  lastLog.Term,
			Entries:      nil, // Heartbeat has no entries
			LeaderCommit: rf.commitIndex,
		}
		
		go func(p string, a *AppendEntriesArgs) {
			reply, err := rf.transport.SendAppendEntries(p, a)
			if err != nil {
				return
			}
			
			rf.mu.Lock()
			defer rf.mu.Unlock()
			
			if reply.Term > rf.currentTerm {
				rf.convertToFollower(reply.Term)
			}
		}(peer, args)
	}
}

func (rf *Raft) convertToFollower(term int64) {
	rf.state = Follower
	rf.currentTerm = term
	rf.votedFor = ""
	rf.resetElectionTimer()
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
	
	votes := 1 // Vote for self
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

func (rf *Raft) convertToLeader() {
	if rf.state == Leader {
		return
	}
	rf.state = Leader
	rf.logger.Info("Elected as leader", "term", rf.currentTerm)
	
	// TODO: Initialize nextIndex and matchIndex for all peers (Step 3)
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
