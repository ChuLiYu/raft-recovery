package raft

import (
	"context"
	"fmt"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GrpcTransport implements the Transport interface using gRPC
type GrpcTransport struct {
	// Cache connections to peers to avoid reconnecting every time
	conns map[string]*grpc.ClientConn
}

// NewGrpcTransport creates a new GrpcTransport
func NewGrpcTransport() *GrpcTransport {
	return &GrpcTransport{
		conns: make(map[string]*grpc.ClientConn),
	}
}

// getClient returns a gRPC client for the given peer address
func (t *GrpcTransport) getClient(peerAddr string) (pb.FalconQueueServiceClient, error) {
	if conn, ok := t.conns[peerAddr]; ok {
		return pb.NewFalconQueueServiceClient(conn), nil
	}

	// Create new connection
	// In production, we should handle connection lifecycle better (e.g. keepalive, backoff)
	conn, err := grpc.NewClient(peerAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return nil, fmt.Errorf("failed to dial peer %s: %w", peerAddr, err)
	}

	t.conns[peerAddr] = conn
	return pb.NewFalconQueueServiceClient(conn), nil
}

// SendRequestVote sends a RequestVote RPC to a peer
func (t *GrpcTransport) SendRequestVote(peer string, args *RequestVoteArgs) (*RequestVoteReply, error) {
	client, err := t.getClient(peer)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond) // Short timeout for RPCs
	defer cancel()

	req := &pb.RequestVoteRequest{
		Term:         args.Term,
		CandidateId:  args.CandidateID,
		LastLogIndex: args.LastLogIndex,
		LastLogTerm:  args.LastLogTerm,
	}

	resp, err := client.RequestVote(ctx, req)
	if err != nil {
		return nil, err
	}

	return &RequestVoteReply{
		Term:        resp.Term,
		VoteGranted: resp.VoteGranted,
	}, nil
}

// SendAppendEntries sends an AppendEntries RPC to a peer
func (t *GrpcTransport) SendAppendEntries(peer string, args *AppendEntriesArgs) (*AppendEntriesReply, error) {
	client, err := t.getClient(peer)
	if err != nil {
		return nil, err
	}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()

	// Convert log entries
	var entries []*pb.LogEntry
	if args.Entries != nil {
		entries = make([]*pb.LogEntry, len(args.Entries))
		for i, e := range args.Entries {
			entries[i] = &pb.LogEntry{
				Term:    e.Term,
				Index:   e.Index,
				Command: e.Command,
			}
		}
	}

	req := &pb.AppendEntriesRequest{
		Term:         args.Term,
		LeaderId:     args.LeaderID,
		PrevLogIndex: args.PrevLogIndex,
		PrevLogTerm:  args.PrevLogTerm,
		Entries:      entries,
		LeaderCommit: args.LeaderCommit,
	}

	resp, err := client.AppendEntries(ctx, req)
	if err != nil {
		return nil, err
	}

	return &AppendEntriesReply{
		Term:    resp.Term,
		Success: resp.Success,
	}, nil
}
