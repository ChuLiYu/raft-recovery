package raft

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

type testNetwork struct {
	mu      sync.Mutex
	nodes   map[string]*Raft
	blocked map[string]bool
}
type testTransport struct {
	net  *testNetwork
	from string
}

func (n *testNetwork) target(from, to string) (*Raft, error) {
	n.mu.Lock()
	defer n.mu.Unlock()
	if n.blocked[from+">"+to] {
		return nil, errors.New("partitioned")
	}
	node := n.nodes[to]
	if node == nil {
		return nil, errors.New("offline")
	}
	return node, nil
}
func (t testTransport) SendRequestVote(peer string, a *RequestVoteArgs) (*RequestVoteReply, error) {
	n, e := t.net.target(t.from, peer)
	if e != nil {
		return nil, e
	}
	r := new(RequestVoteReply)
	n.RequestVote(a, r)
	return r, nil
}
func (t testTransport) SendAppendEntries(peer string, a *AppendEntriesArgs) (*AppendEntriesReply, error) {
	n, e := t.net.target(t.from, peer)
	if e != nil {
		return nil, e
	}
	r := new(AppendEntriesReply)
	n.AppendEntries(a, r)
	return r, nil
}
func (t testTransport) SendInstallSnapshot(peer string, a *InstallSnapshotArgs) (*InstallSnapshotReply, error) {
	n, e := t.net.target(t.from, peer)
	if e != nil {
		return nil, e
	}
	r := new(InstallSnapshotReply)
	n.InstallSnapshot(a, r)
	return r, nil
}
func (n *testNetwork) partition(id string, on bool) {
	n.mu.Lock()
	defer n.mu.Unlock()
	for _, p := range []string{"a", "b", "c"} {
		if p != id {
			n.blocked[id+">"+p] = on
			n.blocked[p+">"+id] = on
		}
	}
}
func waitUntil(t *testing.T, fn func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if fn() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("condition timed out")
}
func cluster(t *testing.T, persistent bool) (*testNetwork, map[string]*Raft, map[string]chan ApplyMsg) {
	t.Helper()
	n := &testNetwork{nodes: map[string]*Raft{}, blocked: map[string]bool{}}
	nodes := map[string]*Raft{}
	applies := map[string]chan ApplyMsg{}
	for _, id := range []string{"a", "b", "c"} {
		ch := make(chan ApplyMsg, 256)
		applies[id] = ch
		c := Config{ID: id, Peers: []string{"a", "b", "c"}, ElectionTimeout: 120 * time.Millisecond, HeartbeatInterval: 20 * time.Millisecond}
		tr := testTransport{net: n, from: id}
		var rf *Raft
		if persistent {
			var err error
			rf, err = NewPersistentRaft(c, filepath.Join(t.TempDir(), id), tr, ch)
			if err != nil {
				t.Fatal(err)
			}
		} else {
			rf = NewRaft(c, NewMemoryLogStore(), tr, ch)
		}
		nodes[id] = rf
		n.nodes[id] = rf
	}
	for _, rf := range nodes {
		rf.Start()
		t.Cleanup(rf.Stop)
	}
	return n, nodes, applies
}
func leaderOf(nodes map[string]*Raft, exclude string) *Raft {
	var leader *Raft
	for id, n := range nodes {
		if id == exclude {
			continue
		}
		_, yes := n.GetState()
		if yes {
			if leader != nil {
				return nil
			}
			leader = n
		}
	}
	return leader
}
func TestSingleNodeDurableRestartSnapshot(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan ApplyMsg, 16)
	c := Config{ID: "one", ElectionTimeout: 30 * time.Millisecond, HeartbeatInterval: 10 * time.Millisecond}
	r, err := NewPersistentRaft(c, dir, testTransport{}, ch)
	if err != nil {
		t.Fatal(err)
	}
	r.Start()
	waitUntil(t, func() bool { _, yes := r.GetState(); return yes })
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	index, term, err := r.ProposeContext(ctx, []byte("work"))
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.lastApplied >= index })
	if err := r.Snapshot(index, []byte("state")); err != nil {
		t.Fatal(err)
	}
	suffixIndex, _, err := r.ProposeContext(ctx, []byte("after-snapshot"))
	if err != nil {
		t.Fatal(err)
	}
	waitUntil(t, func() bool { r.mu.Lock(); defer r.mu.Unlock(); return r.lastApplied >= suffixIndex })
	r.Stop()
	ch2 := make(chan ApplyMsg, 16)
	r2, err := NewPersistentRaft(c, dir, testTransport{}, ch2)
	if err != nil {
		t.Fatal(err)
	}
	r2.Start()
	defer r2.Stop()
	select {
	case msg := <-ch2:
		if !msg.SnapshotValid || msg.SnapshotIndex != index || msg.SnapshotTerm != term || string(msg.Snapshot) != "state" {
			t.Fatalf("unexpected restore: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("snapshot not restored")
	}
	select {
	case msg := <-ch2:
		if !msg.CommandValid || msg.CommandIndex != suffixIndex || string(msg.Command) != "after-snapshot" {
			t.Fatalf("committed suffix not restored: %+v", msg)
		}
	case <-time.After(time.Second):
		t.Fatal("committed suffix not replayed")
	}
	r2.mu.Lock()
	got := r2.currentTerm
	r2.mu.Unlock()
	if got < term {
		t.Fatalf("term regressed: %d < %d", got, term)
	}
}
func TestVoteRequiresFreshLogAndPersists(t *testing.T) {
	dir := t.TempDir()
	ch := make(chan ApplyMsg, 1)
	c := Config{ID: "a", Peers: []string{"a", "b", "c"}}
	r, err := NewPersistentRaft(c, dir, testTransport{}, ch)
	if err != nil {
		t.Fatal(err)
	}
	r.mu.Lock()
	r.currentTerm = 4
	if err := r.appendLocked(LogEntry{Index: 1, Term: 4, Command: []byte("x")}); err != nil {
		t.Fatal(err)
	}
	r.mu.Unlock()
	var reply RequestVoteReply
	r.RequestVote(&RequestVoteArgs{Term: 5, CandidateID: "b", LastLogIndex: 100, LastLogTerm: 3}, &reply)
	if reply.VoteGranted {
		t.Fatal("stale term won vote")
	}
	r.RequestVote(&RequestVoteArgs{Term: 5, CandidateID: "b", LastLogIndex: 1, LastLogTerm: 4}, &reply)
	if !reply.VoteGranted {
		t.Fatal("fresh candidate denied")
	}
	r2, err := NewPersistentRaft(c, dir, testTransport{}, make(chan ApplyMsg, 1))
	if err != nil {
		t.Fatal(err)
	}
	r2.mu.Lock()
	defer r2.mu.Unlock()
	if r2.currentTerm != 5 || r2.votedFor != "b" || r2.lastIndexLocked() != 1 {
		t.Fatalf("durable state lost: term=%d vote=%q last=%d", r2.currentTerm, r2.votedFor, r2.lastIndexLocked())
	}
}
func TestPartitionCommitConflictRepair(t *testing.T) {
	net, nodes, _ := cluster(t, false)
	waitUntil(t, func() bool { return leaderOf(nodes, "") != nil })
	old := leaderOf(nodes, "")
	oldID := old.config.ID
	net.partition(oldID, true)
	waitUntil(t, func() bool { return leaderOf(nodes, oldID) != nil })
	ctx, cancel := context.WithTimeout(context.Background(), 150*time.Millisecond)
	defer cancel()
	_, _, err := old.ProposeContext(ctx, []byte("uncommitted"))
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("isolated leader proposal: %v", err)
	}
	newLeader := leaderOf(nodes, oldID)
	ctx2, cancel2 := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel2()
	idx, term, err := newLeader.ProposeContext(ctx2, []byte("committed"))
	if err != nil {
		t.Fatal(err)
	}
	net.partition(oldID, false)
	waitUntil(t, func() bool {
		old.mu.Lock()
		defer old.mu.Unlock()
		if old.commitIndex < idx {
			return false
		}
		e := old.entries[idx-old.lastIncludedIndex]
		return e.Term == term && string(e.Command) == "committed"
	})
}
func TestSnapshotCatchup(t *testing.T) {
	net, nodes, _ := cluster(t, false)
	waitUntil(t, func() bool { return leaderOf(nodes, "") != nil })
	leader := leaderOf(nodes, "")
	var lagID string
	for id := range nodes {
		if id != leader.config.ID {
			lagID = id
			break
		}
	}
	net.partition(lagID, true)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	var idx int64
	for i := 0; i < 5; i++ {
		var err error
		idx, _, err = leader.ProposeContext(ctx, []byte(fmt.Sprintf("entry-%d", i)))
		if err != nil {
			t.Fatal(err)
		}
	}
	waitUntil(t, func() bool { leader.mu.Lock(); defer leader.mu.Unlock(); return leader.lastApplied >= idx })
	if err := leader.Snapshot(idx, []byte("checkpoint")); err != nil {
		t.Fatal(err)
	}
	net.partition(lagID, false)
	lag := nodes[lagID]
	waitUntil(t, func() bool {
		lag.mu.Lock()
		defer lag.mu.Unlock()
		return lag.lastIncludedIndex >= idx && lag.commitIndex >= idx
	})
	lag.mu.Lock()
	defer lag.mu.Unlock()
	if string(lag.snapshot) != "checkpoint" {
		t.Fatalf("snapshot missing: %q", lag.snapshot)
	}
}
func TestRejectCorruptState(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "raft-state.json"), []byte("{"), 0600); err != nil {
		t.Fatal(err)
	}
	_, err := NewPersistentRaft(Config{ID: "a"}, dir, testTransport{}, make(chan ApplyMsg, 1))
	if err == nil {
		t.Fatal("corrupt state accepted")
	}
	if err := writeDiskState(dir, diskState{Version: 1, Entries: []LogEntry{{}}}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "raft-state.json")
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	data = bytes.Replace(data, []byte(`"vote":""`), []byte(`"vote":"other"`), 1)
	if err := os.WriteFile(path, data, 0600); err != nil {
		t.Fatal(err)
	}
	if _, err := NewPersistentRaft(Config{ID: "a"}, dir, testTransport{}, make(chan ApplyMsg, 1)); err == nil {
		t.Fatal("checksum mismatch accepted")
	}
}

func TestAppendEntriesCommitsOnlyMatchedPrefix(t *testing.T) {
	r := NewRaft(Config{ID: "f", Peers: []string{"f", "l"}}, NewMemoryLogStore(), testTransport{}, make(chan ApplyMsg, 10))
	r.mu.Lock()
	for i := int64(1); i <= 3; i++ {
		if err := r.appendLocked(LogEntry{Index: i, Term: 1, Command: []byte("old")}); err != nil {
			t.Fatal(err)
		}
	}
	r.mu.Unlock()
	var reply AppendEntriesReply
	r.AppendEntries(&AppendEntriesArgs{Term: 2, LeaderID: "l", PrevLogIndex: 1, PrevLogTerm: 1, LeaderCommit: 3}, &reply)
	if !reply.Success {
		t.Fatal("matching heartbeat rejected")
	}
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.commitIndex != 1 {
		t.Fatalf("committed unmatched suffix through %d", r.commitIndex)
	}
}

func TestStoppedRaftRejectsRPCMutation(t *testing.T) {
	r := NewRaft(Config{ID: "f"}, NewMemoryLogStore(), testTransport{}, make(chan ApplyMsg, 1))
	r.Stop()
	var vote RequestVoteReply
	r.RequestVote(&RequestVoteArgs{Term: 10, CandidateID: "x"}, &vote)
	var appendReply AppendEntriesReply
	r.AppendEntries(&AppendEntriesArgs{Term: 10, LeaderID: "x", PrevLogIndex: 0, PrevLogTerm: 0, Entries: []LogEntry{{Index: 1, Term: 10}}}, &appendReply)
	r.mu.Lock()
	defer r.mu.Unlock()
	if vote.VoteGranted || appendReply.Success || r.currentTerm != 0 || r.lastIndexLocked() != 0 {
		t.Fatal("stopped node accepted an RPC")
	}
}

func TestProposalCannotSucceedAfterSameIndexIsReplaced(t *testing.T) {
	r := NewRaft(Config{ID: "a", Peers: []string{"a", "b"}}, NewMemoryLogStore(), testTransport{}, make(chan ApplyMsg, 1))
	r.mu.Lock()
	r.currentTerm = 1
	r.state = Leader
	r.mu.Unlock()
	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, _, err := r.ProposeContext(ctx, []byte("stale"))
		result <- err
	}()
	waitUntil(t, func() bool {
		r.mu.Lock()
		defer r.mu.Unlock()
		return r.lastIndexLocked() == 1
	})
	r.mu.Lock()
	if err := r.logStore.DeleteRange(1, 1); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	r.entries = r.entries[:1]
	r.currentTerm = 2
	r.state = Follower
	if err := r.appendLocked(LogEntry{Term: 2, Index: 1, Command: []byte("replacement")}); err != nil {
		r.mu.Unlock()
		t.Fatal(err)
	}
	r.commitIndex = 1
	r.signalCommitLocked()
	r.mu.Unlock()
	if err := <-result; !errors.Is(err, ErrNotLeader) {
		t.Fatalf("replaced proposal returned %v", err)
	}
}
