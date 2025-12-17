package raft

// RequestVoteArgs represents the arguments for RequestVote RPC
type RequestVoteArgs struct {
	Term         int64
	CandidateID  string
	LastLogIndex int64
	LastLogTerm  int64
}

// RequestVoteReply represents the reply for RequestVote RPC
type RequestVoteReply struct {
	Term        int64
	VoteGranted bool
}

// RequestVote handles the RequestVote RPC
func (rf *Raft) RequestVote(args *RequestVoteArgs, reply *RequestVoteReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	// 1. Reply false if term < currentTerm
	if args.Term < rf.currentTerm {
		reply.Term = rf.currentTerm
		reply.VoteGranted = false
		return
	}

	// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = Follower
		rf.votedFor = ""
	}

	reply.Term = rf.currentTerm

	// 2. If votedFor is null or candidateId, and candidate’s log is at least as up-to-date as receiver’s log, grant vote
	canVote := (rf.votedFor == "" || rf.votedFor == args.CandidateID)
	isUpToDate := true // TODO: Check log up-to-date (Step 3)

	if canVote && isUpToDate {
		rf.votedFor = args.CandidateID
		reply.VoteGranted = true
		rf.resetElectionTimer() // Granting vote resets election timer
		rf.logger.Info("Vote granted", "candidate", args.CandidateID, "term", args.Term)
	} else {
		reply.VoteGranted = false
	}
}

// AppendEntriesArgs represents the arguments for AppendEntries RPC
type AppendEntriesArgs struct {
	Term         int64
	LeaderID     string
	PrevLogIndex int64
	PrevLogTerm  int64
	Entries      []LogEntry
	LeaderCommit int64
}

// AppendEntriesReply represents the reply for AppendEntries RPC
type AppendEntriesReply struct {
	Term    int64
	Success bool
}

// AppendEntries handles the AppendEntries RPC (Heartbeat & Log Replication)
func (rf *Raft) AppendEntries(args *AppendEntriesArgs, reply *AppendEntriesReply) {
	rf.mu.Lock()
	defer rf.mu.Unlock()

	reply.Term = rf.currentTerm
	reply.Success = false

	// 1. Reply false if term < currentTerm
	if args.Term < rf.currentTerm {
		return
	}

	// If RPC request or response contains term T > currentTerm: set currentTerm = T, convert to follower
	if args.Term > rf.currentTerm {
		rf.currentTerm = args.Term
		rf.state = Follower
		rf.votedFor = ""
	}

	// Valid leader detected, reset timer
	rf.resetElectionTimer()
	rf.leaderID = args.LeaderID

	// 2. Reply false if log doesn't contain an entry at prevLogIndex whose term matches prevLogTerm
	// TODO: Implement log check

	// 3. If an existing entry conflicts with a new one (same index but different terms), delete the existing entry and all that follow it
	// TODO: Implement log conflict resolution

	// 4. Append any new entries not already in the log
	// TODO: Append entries

	// 5. If leaderCommit > commitIndex, set commitIndex = min(leaderCommit, index of last new entry)
	if args.LeaderCommit > rf.commitIndex {
		// rf.commitIndex = min(args.LeaderCommit, rf.getLastLogIndex())
		rf.commitIndex = args.LeaderCommit // Simplified for now
	}

	reply.Success = true
}
