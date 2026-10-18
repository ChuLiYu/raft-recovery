package server

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"sync"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/internal/raft"
	"github.com/ChuLiYu/raft-recovery/internal/worker"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

// Server implements the gRPC server for FalconQueueService.
type Server struct {
	pb.UnimplementedFalconQueueServiceServer

	controller *controller.Controller
	raftNode   *raft.Raft

	// Worker Registry
	mu      sync.RWMutex
	workers map[string]*WorkerInfo
}

// WorkerInfo tracks the state of a registered worker
type WorkerInfo struct {
	NodeID     string
	Address    string
	Capacity   int32
	Tags       []string
	LastSeen   time.Time
	ExpiryTime time.Time
}

// NewServer creates a new gRPC server instance.
func NewServer(ctrl *controller.Controller, rf *raft.Raft) *Server {
	return &Server{
		controller: ctrl,
		raftNode:   rf,
		workers:    make(map[string]*WorkerInfo),
	}
}

// RequestVote handles Raft RequestVote RPC
func (s *Server) RequestVote(ctx context.Context, req *pb.RequestVoteRequest) (*pb.RequestVoteResponse, error) {
	if s.raftNode == nil {
		return nil, fmt.Errorf("raft node not initialized")
	}

	args := &raft.RequestVoteArgs{
		Term:         req.Term,
		CandidateID:  req.CandidateId,
		LastLogIndex: req.LastLogIndex,
		LastLogTerm:  req.LastLogTerm,
	}

	reply := &raft.RequestVoteReply{}
	s.raftNode.RequestVote(args, reply)

	return &pb.RequestVoteResponse{
		Term:        reply.Term,
		VoteGranted: reply.VoteGranted,
	}, nil
}

// AppendEntries handles Raft AppendEntries RPC
func (s *Server) AppendEntries(ctx context.Context, req *pb.AppendEntriesRequest) (*pb.AppendEntriesResponse, error) {
	if s.raftNode == nil {
		return nil, fmt.Errorf("raft node not initialized")
	}

	entries := make([]raft.LogEntry, len(req.Entries))
	for i, e := range req.Entries {
		entries[i] = raft.LogEntry{
			Term:    e.Term,
			Index:   e.Index,
			Command: e.Command,
		}
	}

	args := &raft.AppendEntriesArgs{
		Term:         req.Term,
		LeaderID:     req.LeaderId,
		PrevLogIndex: req.PrevLogIndex,
		PrevLogTerm:  req.PrevLogTerm,
		Entries:      entries,
		LeaderCommit: req.LeaderCommit,
	}

	reply := &raft.AppendEntriesReply{}
	s.raftNode.AppendEntries(args, reply)

	return &pb.AppendEntriesResponse{
		Term:          reply.Term,
		Success:       reply.Success,
		ConflictIndex: reply.ConflictIndex,
		ConflictTerm:  reply.ConflictTerm,
	}, nil
}

func (s *Server) InstallSnapshot(ctx context.Context, req *pb.InstallSnapshotRequest) (*pb.InstallSnapshotResponse, error) {
	if s.raftNode == nil {
		return nil, status.Error(codes.FailedPrecondition, "raft node not initialized")
	}
	args := &raft.InstallSnapshotArgs{Term: req.Term, LeaderID: req.LeaderId, LastIncludedIndex: req.LastIncludedIndex, LastIncludedTerm: req.LastIncludedTerm, Data: req.Data}
	reply := &raft.InstallSnapshotReply{}
	s.raftNode.InstallSnapshot(args, reply)
	return &pb.InstallSnapshotResponse{Term: reply.Term, Success: reply.Success}, nil
}

func (s *Server) requireLeader() error {
	if s.raftNode == nil {
		return nil
	}
	if _, leader := s.raftNode.GetState(); leader {
		return nil
	}
	return status.Errorf(codes.FailedPrecondition, "not leader; leader=%s", s.raftNode.LeaderID())
}

func (s *Server) proposeApplied(ctx context.Context, cmd []byte) (int64, error) {
	index, _, err := s.raftNode.ProposeContext(ctx, cmd)
	if err != nil {
		if errors.Is(err, raft.ErrNotLeader) {
			return 0, status.Errorf(codes.FailedPrecondition, "not leader; leader=%s", s.raftNode.LeaderID())
		}
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, status.FromContextError(err).Err()
		}
		return 0, status.Errorf(codes.Unavailable, "raft proposal: %v", err)
	}
	if err := s.controller.WaitApplied(ctx, index); err != nil {
		if errors.Is(err, context.DeadlineExceeded) || errors.Is(err, context.Canceled) {
			return 0, status.FromContextError(err).Err()
		}
		return 0, status.Errorf(codes.Unavailable, "state apply: %v", err)
	}
	return index, nil
}

// SubmitJob handles job submission from clients.
func (s *Server) SubmitJob(ctx context.Context, req *pb.SubmitJobRequest) (*pb.SubmitJobResponse, error) {
	if err := s.requireLeader(); err != nil {
		return nil, err
	}
	// 1. Convert request to types.Job
	jobID := req.JobId
	if jobID == "" {
		// Generate ID if not provided (simple implementation)
		jobID = fmt.Sprintf("job-%d", time.Now().UnixNano())
	}

	var payload map[string]interface{}
	if len(req.Payload) > 0 {
		if err := json.Unmarshal(req.Payload, &payload); err != nil {
			return &pb.SubmitJobResponse{
				Success:      false,
				ErrorMessage: "Invalid payload JSON: " + err.Error(),
			}, nil
		}
	}

	job := types.Job{
		ID:        types.JobID(jobID),
		Payload:   payload,
		Status:    types.StatusPending,
		Timeout:   time.Duration(req.TimeoutMs) * time.Millisecond,
		CreatedAt: time.Now().UnixMilli(),
		UpdatedAt: time.Now().UnixMilli(),
	}

	// 2. Propose via Raft (Phase 3)
	if s.raftNode != nil {
		cmd, err := raft.NewEnqueueCommand([]types.Job{job})
		if err != nil {
			return &pb.SubmitJobResponse{Success: false, ErrorMessage: "Failed to encode command"}, nil
		}

		if _, err := s.proposeApplied(ctx, cmd); err != nil {
			return nil, err
		}

		return &pb.SubmitJobResponse{Success: true, JobId: jobID}, nil
	}

	// Fallback to local Enqueue if Raft not enabled (Standalone Mode)
	if err := s.controller.EnqueueJobs([]types.Job{job}); err != nil {
		return &pb.SubmitJobResponse{
			Success:      false,
			ErrorMessage: "Enqueue failed: " + err.Error(),
		}, nil
	}

	return &pb.SubmitJobResponse{
		Success: true,
		JobId:   jobID,
	}, nil
}

// RegisterWorker registers a new worker node.
func (s *Server) RegisterWorker(ctx context.Context, req *pb.RegisterWorkerRequest) (*pb.RegisterWorkerResponse, error) {
	if err := s.requireLeader(); err != nil {
		return nil, err
	}
	if req.NodeId == "" {
		return nil, status.Error(codes.InvalidArgument, "worker ID required")
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	leaseDuration := 10 * time.Second

	s.workers[req.NodeId] = &WorkerInfo{
		NodeID:     req.NodeId,
		Address:    req.Address,
		Capacity:   req.Capacity,
		Tags:       req.Tags,
		LastSeen:   time.Now(),
		ExpiryTime: time.Now().Add(leaseDuration),
	}

	return &pb.RegisterWorkerResponse{
		Success:         true,
		LeaseDurationMs: leaseDuration.Milliseconds(),
	}, nil
}

// SendHeartbeat updates the liveness of a worker.
func (s *Server) SendHeartbeat(ctx context.Context, req *pb.HeartbeatRequest) (*pb.HeartbeatResponse, error) {
	if err := s.requireLeader(); err != nil {
		return nil, err
	}
	s.mu.Lock()
	defer s.mu.Unlock()

	info, exists := s.workers[req.NodeId]
	if !exists {
		return &pb.HeartbeatResponse{
			Acknowledged: false,
			ReRegister:   true, // Tell worker to re-register
		}, nil
	}

	// Extend lease
	leaseDuration := 10 * time.Second
	info.LastSeen = time.UnixMilli(req.Timestamp)
	info.ExpiryTime = time.Now().Add(leaseDuration)

	// Update load info (optional, for metrics)
	// info.CurrentLoad = req.CurrentLoad

	return &pb.HeartbeatResponse{
		Acknowledged: true,
		ReRegister:   false,
	}, nil
}

// PollJobs fetches pending jobs for the worker.
func (s *Server) PollJobs(ctx context.Context, req *pb.PollJobsRequest) (*pb.PollJobsResponse, error) {
	if err := s.requireLeader(); err != nil {
		return nil, err
	}
	if req.MaxJobs < 0 || req.MaxJobs > 100 {
		return nil, status.Error(codes.InvalidArgument, "max jobs must be between 0 and 100")
	}
	var jobs []*types.Job
	if s.raftNode == nil {
		var err error
		jobs, err = s.controller.Poll(ctx, int(req.MaxJobs))
		if err != nil {
			return nil, err
		}
	} else {
		s.mu.RLock()
		workerInfo := s.workers[req.WorkerId]
		validWorker := workerInfo != nil && time.Now().Before(workerInfo.ExpiryTime)
		s.mu.RUnlock()
		if !validWorker {
			return nil, status.Error(codes.FailedPrecondition, "worker registration or lease expired")
		}
		for len(jobs) < int(req.MaxJobs) {
			pending := s.controller.PendingJobs(1)
			if len(pending) == 0 {
				break
			}
			job := pending[0]
			resultID, err := s.controller.RegisterAppliedResult()
			if err != nil {
				return nil, status.Errorf(codes.Internal, "register dispatch result: %v", err)
			}
			at := time.Now().UnixMilli()
			timeout := job.Timeout
			if timeout <= 0 {
				timeout = 30 * time.Second
			}
			deadline := at + timeout.Milliseconds()
			cmd, err := raft.NewDispatchCommand(raft.DispatchPayload{JobID: string(job.ID), WorkerID: req.WorkerId, Attempt: job.Attempt + 1, At: at, Deadline: deadline, ResultID: resultID})
			if err != nil {
				s.controller.DiscardAppliedResult(resultID)
				return nil, status.Errorf(codes.Internal, "encode dispatch: %v", err)
			}
			_, err = s.proposeApplied(ctx, cmd)
			if err != nil {
				s.controller.DiscardAppliedResult(resultID)
				return nil, err
			}
			for _, applied := range s.controller.AppliedJobs(resultID) {
				j := applied
				jobs = append(jobs, &j)
			}
			// A competing poll can win the head; retry on the next pending job.
		}
	}

	// Convert types.Job to pb.Job
	pbJobs := make([]*pb.Job, 0, len(jobs))
	for _, job := range jobs {
		payloadBytes, _ := json.Marshal(job.Payload)

		pbJob := &pb.Job{
			Id:        string(job.ID),
			Payload:   payloadBytes,
			Status:    mapStatusToPb(job.Status),
			Attempt:   int32(job.Attempt),
			TimeoutMs: job.Timeout.Milliseconds(),
			CreatedAt: job.CreatedAt,
			UpdatedAt: job.UpdatedAt,
			WorkerId:  req.WorkerId,
		}

		if job.Deadline != nil {
			pbJob.DeadlineMs = *job.Deadline
		}

		pbJobs = append(pbJobs, pbJob)
	}

	return &pb.PollJobsResponse{
		Jobs: pbJobs,
	}, nil
}

// AcknowledgeJob reports job status from worker.
func (s *Server) AcknowledgeJob(ctx context.Context, req *pb.AcknowledgeJobRequest) (*pb.AcknowledgeJobResponse, error) {
	if err := s.requireLeader(); err != nil {
		return nil, err
	}
	jobStatus := mapPbStatusToType(req.Status)
	if jobStatus != types.StatusCompleted && jobStatus != types.StatusDead {
		return nil, status.Error(codes.InvalidArgument, "ACK status must be completed or dead")
	}

	// Phase 3: Propose via Raft
	if s.raftNode != nil {
		job, ok := s.controller.Job(types.JobID(req.JobId))
		completedDuplicate := ok && job.Status == types.StatusCompleted && job.Attempt == int(req.GetAttempt()) && job.WorkerID == req.WorkerId && req.WorkerId != "" && jobStatus == types.StatusCompleted
		if !completedDuplicate && (!ok || job.Status != types.StatusInFlight || job.Attempt != int(req.GetAttempt()) || job.WorkerID != req.WorkerId) {
			return &pb.AcknowledgeJobResponse{Success: false}, nil
		}
		resultID, err := s.controller.RegisterAppliedResult()
		if err != nil {
			return nil, status.Errorf(codes.Internal, "register ACK result: %v", err)
		}
		cmd, err := raft.NewAckAttemptCommand(raft.AckPayload{JobID: req.JobId, WorkerID: req.WorkerId, Attempt: int(req.GetAttempt()), Status: jobStatus, At: time.Now().UnixMilli(), MaxRetry: s.controller.MaxRetry(), ResultID: resultID})
		if err != nil {
			s.controller.DiscardAppliedResult(resultID)
			return nil, status.Errorf(codes.Internal, "encode ACK: %v", err)
		}
		_, err = s.proposeApplied(ctx, cmd)
		if err != nil {
			s.controller.DiscardAppliedResult(resultID)
			return nil, err
		}
		return &pb.AcknowledgeJobResponse{Success: s.controller.AppliedAccepted(resultID)}, nil
	}

	result := &worker.Result{
		JobID:   types.JobID(req.JobId),
		Attempt: int(req.GetAttempt()),
		Success: jobStatus == types.StatusCompleted,
	}

	if err := s.controller.Acknowledge(ctx, req.JobId, jobStatus, result); err != nil {
		return &pb.AcknowledgeJobResponse{Success: false}, nil
	}

	return &pb.AcknowledgeJobResponse{Success: true}, nil
}

// Helpers

func mapStatusToPb(s types.JobStatus) pb.JobStatus {
	switch s {
	case types.StatusPending:
		return pb.JobStatus_JOB_STATUS_PENDING
	case types.StatusInFlight:
		return pb.JobStatus_JOB_STATUS_IN_FLIGHT
	case types.StatusCompleted:
		return pb.JobStatus_JOB_STATUS_COMPLETED
	case types.StatusDead:
		return pb.JobStatus_JOB_STATUS_DEAD
	default:
		return pb.JobStatus_JOB_STATUS_UNSPECIFIED
	}
}

func mapPbStatusToType(s pb.JobStatus) types.JobStatus {
	switch s {
	case pb.JobStatus_JOB_STATUS_PENDING:
		return types.StatusPending
	case pb.JobStatus_JOB_STATUS_IN_FLIGHT:
		return types.StatusInFlight
	case pb.JobStatus_JOB_STATUS_COMPLETED:
		return types.StatusCompleted
	case pb.JobStatus_JOB_STATUS_DEAD:
		return types.StatusDead
	default:
		return types.StatusPending // Fallback
	}
}
