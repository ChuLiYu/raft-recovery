package raft

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

type GrpcTransport struct {
	mu     sync.Mutex
	conns  map[string]*grpc.ClientConn
	closed bool
}

func NewGrpcTransport() *GrpcTransport { return &GrpcTransport{conns: map[string]*grpc.ClientConn{}} }
func (t *GrpcTransport) getClient(peer string) (pb.FalconQueueServiceClient, error) {
	t.mu.Lock()
	defer t.mu.Unlock()
	if t.closed {
		return nil, errors.New("raft transport closed")
	}
	if conn := t.conns[peer]; conn != nil {
		return pb.NewFalconQueueServiceClient(conn), nil
	}
	conn, err := grpc.NewClient(peer, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("dial %s: %w", peer, err)
	}
	t.conns[peer] = conn
	return pb.NewFalconQueueServiceClient(conn), nil
}
func (t *GrpcTransport) Close() error {
	t.mu.Lock()
	if t.closed {
		t.mu.Unlock()
		return nil
	}
	t.closed = true
	conns := t.conns
	t.conns = nil
	t.mu.Unlock()
	var result error
	for _, conn := range conns {
		result = errors.Join(result, conn.Close())
	}
	return result
}
func (t *GrpcTransport) SendRequestVote(peer string, args *RequestVoteArgs) (*RequestVoteReply, error) {
	client, err := t.getClient(peer)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	resp, err := client.RequestVote(ctx, &pb.RequestVoteRequest{Term: args.Term, CandidateId: args.CandidateID, LastLogIndex: args.LastLogIndex, LastLogTerm: args.LastLogTerm})
	if err != nil {
		return nil, err
	}
	return &RequestVoteReply{Term: resp.Term, VoteGranted: resp.VoteGranted}, nil
}
func (t *GrpcTransport) SendAppendEntries(peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	client, err := t.getClient(peer)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 300*time.Millisecond)
	defer cancel()
	entries := make([]*pb.LogEntry, len(args.Entries))
	for i, e := range args.Entries {
		entries[i] = &pb.LogEntry{Term: e.Term, Index: e.Index, Command: e.Command}
	}
	resp, err := client.AppendEntries(ctx, &pb.AppendEntriesRequest{Term: args.Term, LeaderId: args.LeaderID, PrevLogIndex: args.PrevLogIndex, PrevLogTerm: args.PrevLogTerm, Entries: entries, LeaderCommit: args.LeaderCommit})
	if err != nil {
		return nil, err
	}
	return &AppendEntriesReply{Term: resp.Term, Success: resp.Success, ConflictIndex: resp.ConflictIndex, ConflictTerm: resp.ConflictTerm}, nil
}
func (t *GrpcTransport) SendInstallSnapshot(peer string, args *InstallSnapshotArgs) (*InstallSnapshotReply, error) {
	client, err := t.getClient(peer)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	resp, err := client.InstallSnapshot(ctx, &pb.InstallSnapshotRequest{Term: args.Term, LeaderId: args.LeaderID, LastIncludedIndex: args.LastIncludedIndex, LastIncludedTerm: args.LastIncludedTerm, Data: args.Data})
	if err != nil {
		return nil, err
	}
	return &InstallSnapshotReply{Term: resp.Term, Success: resp.Success}, nil
}
