package raft

import "errors"

type RequestVoteArgs struct {
	Term                      int64
	CandidateID               string
	LastLogIndex, LastLogTerm int64
}
type RequestVoteReply struct {
	Term        int64
	VoteGranted bool
}
type AppendEntriesArgs struct {
	Term                      int64
	LeaderID                  string
	PrevLogIndex, PrevLogTerm int64
	Entries                   []LogEntry
	LeaderCommit              int64
}
type AppendEntriesReply struct {
	Term                        int64
	Success                     bool
	ConflictIndex, ConflictTerm int64
}
type InstallSnapshotArgs struct {
	Term                                int64
	LeaderID                            string
	LastIncludedIndex, LastIncludedTerm int64
	Data                                []byte
}
type InstallSnapshotReply struct {
	Term    int64
	Success bool
}

func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	if args == nil || rf.fatal != nil || rf.stoppedLocked() {
		return
	}
	if args.Term < rf.currentTerm {
		return
	}
	if args.Term > rf.currentTerm {
		rf.stepDownLocked(args.Term, "")
	}
	reply.Term = rf.currentTerm
	if rf.fatal != nil {
		return
	}
	upToDate := args.LastLogTerm > rf.lastTermLocked() || (args.LastLogTerm == rf.lastTermLocked() && args.LastLogIndex >= rf.lastIndexLocked())
	if upToDate && (rf.votedFor == "" || rf.votedFor == args.CandidateID) {
		rf.votedFor = args.CandidateID
		if rf.persistLocked() != nil {
			return
		}
		reply.VoteGranted = true
		rf.resetElectionLocked()
	}
}
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	if args == nil || rf.fatal != nil || rf.stoppedLocked() {
		return
	}
	if args.Term < rf.currentTerm {
		return
	}
	if args.Term > rf.currentTerm {
		rf.stepDownLocked(args.Term, args.LeaderID)
	} else if rf.state != Follower {
		rf.stepDownLocked(args.Term, args.LeaderID)
	}
	reply.Term = rf.currentTerm
	if rf.fatal != nil {
		return
	}
	rf.leaderID = args.LeaderID
	rf.resetElectionLocked()
	last := rf.lastIndexLocked()
	if args.PrevLogIndex < rf.lastIncludedIndex {
		reply.ConflictIndex = rf.lastIncludedIndex + 1
		return
	}
	if args.PrevLogIndex > last {
		reply.ConflictIndex = last + 1
		return
	}
	prevTerm, _ := rf.termAtLocked(args.PrevLogIndex)
	if prevTerm != args.PrevLogTerm {
		reply.ConflictTerm = prevTerm
		ci := args.PrevLogIndex
		for ci > rf.lastIncludedIndex {
			t, _ := rf.termAtLocked(ci - 1)
			if t != prevTerm {
				break
			}
			ci--
		}
		reply.ConflictIndex = ci
		return
	}
	for i, e := range args.Entries {
		expected := args.PrevLogIndex + 1 + int64(i)
		if e.Index != expected {
			reply.ConflictIndex = expected
			return
		}
		if e.Index <= rf.lastIndexLocked() {
			oldTerm, _ := rf.termAtLocked(e.Index)
			if oldTerm == e.Term {
				continue
			}
			if e.Index <= rf.commitIndex {
				reply.ConflictIndex = rf.commitIndex + 1
				return
			}
			if err := rf.logStore.DeleteRange(e.Index, rf.lastIndexLocked()); err != nil {
				rf.fatal = err
				rf.state = Follower
				return
			}
			rf.entries = rf.entries[:e.Index-rf.lastIncludedIndex]
		}
		for _, newEntry := range args.Entries[i:] {
			if err := rf.logStore.StoreLog(&newEntry); err != nil {
				rf.fatal = err
				rf.state = Follower
				return
			}
			newEntry.Command = append([]byte(nil), newEntry.Command...)
			rf.entries = append(rf.entries, newEntry)
		}
		if rf.persistLocked() != nil {
			return
		}
		break
	}
	if args.LeaderCommit > rf.commitIndex {
		newCommit := args.LeaderCommit
		lastMatched := args.PrevLogIndex + int64(len(args.Entries))
		if newCommit > lastMatched {
			newCommit = lastMatched
		}
		if newCommit > rf.commitIndex {
			rf.commitIndex = newCommit
			if rf.persistLocked() != nil {
				return
			}
			rf.signalCommitLocked()
		}
	}
	reply.Success = true
	reply.Term = rf.currentTerm
}
func (rf *Raft) InstallSnapshot(args *InstallSnapshotArgs, reply *InstallSnapshotReply) {
	rf.applyMu.Lock()
	defer rf.applyMu.Unlock()
	rf.mu.Lock()
	defer rf.mu.Unlock()
	reply.Term = rf.currentTerm
	if args == nil || rf.fatal != nil || rf.stoppedLocked() {
		return
	}
	if args.Term < rf.currentTerm {
		return
	}
	if args.Term > rf.currentTerm || rf.state != Follower {
		rf.stepDownLocked(args.Term, args.LeaderID)
	}
	reply.Term = rf.currentTerm
	if rf.fatal != nil {
		return
	}
	rf.leaderID = args.LeaderID
	rf.resetElectionLocked()
	if args.LastIncludedIndex <= rf.lastIncludedIndex {
		reply.Success = true
		return
	}
	var suffix []LogEntry
	term, matches := rf.termAtLocked(args.LastIncludedIndex)
	if args.LastIncludedIndex <= rf.commitIndex && (!matches || term != args.LastIncludedTerm) {
		return
	}
	if matches && term == args.LastIncludedTerm {
		suffix = append([]LogEntry(nil), rf.entries[args.LastIncludedIndex-rf.lastIncludedIndex:]...)
	} else {
		suffix = []LogEntry{{Index: args.LastIncludedIndex, Term: args.LastIncludedTerm}}
	}
	suffix[0].Command = nil
	oldApplied := rf.lastApplied
	rf.entries = suffix
	rf.lastIncludedIndex = args.LastIncludedIndex
	rf.lastIncludedTerm = args.LastIncludedTerm
	rf.snapshot = append([]byte(nil), args.Data...)
	if rf.commitIndex < args.LastIncludedIndex {
		rf.commitIndex = args.LastIncludedIndex
	}
	if rf.commitIndex > rf.lastIndexLocked() {
		rf.commitIndex = rf.lastIndexLocked()
	}
	if err := rf.persistLocked(); err != nil {
		return
	}
	if replacer, ok := rf.logStore.(interface{ replace(int64, []LogEntry) }); ok {
		replacer.replace(args.LastIncludedIndex, suffix)
	} else {
		rf.fatal = errors.New("log store does not support snapshot installation")
		return
	}
	if oldApplied < args.LastIncludedIndex {
		rf.pendingSnapshot = true
		rf.lastApplied = args.LastIncludedIndex
		rf.signalCommitLocked()
	}
	reply.Success = true
}
