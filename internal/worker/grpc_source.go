package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"google.golang.org/grpc"
)

// GrpcJobSource is an implementation of JobSource that connects to a remote Master node via gRPC.
type GrpcJobSource struct {
	client     pb.FalconQueueServiceClient
	workerID   string
	workerAddr string // Optional: advertise address
}

// NewGrpcJobSource creates a new GrpcJobSource.
// conn should be an established gRPC connection.
func NewGrpcJobSource(conn grpc.ClientConnInterface, workerID string, address string) *GrpcJobSource {
	return &GrpcJobSource{
		client:     pb.NewFalconQueueServiceClient(conn),
		workerID:   workerID,
		workerAddr: address,
	}
}

// Poll fetches jobs from the remote Master.
func (s *GrpcJobSource) Poll(ctx context.Context, maxJobs int) ([]*types.Job, error) {
	req := &pb.PollJobsRequest{
		WorkerId: s.workerID,
		MaxJobs:  int32(maxJobs),
	}

	resp, err := s.client.PollJobs(ctx, req)
	if err != nil {
		return nil, fmt.Errorf("rpc poll failed: %w", err)
	}

	jobs := make([]*types.Job, 0, len(resp.Jobs))
	for _, pbJob := range resp.Jobs {
		var payload map[string]interface{}
		// Unmarshal payload if present
		if len(pbJob.Payload) > 0 {
			if err := json.Unmarshal(pbJob.Payload, &payload); err != nil {
				// Log warning but skip bad job? Or return error?
				// For now, return incomplete job or empty payload
				payload = make(map[string]interface{})
			}
		}

		job := &types.Job{
			ID:        types.JobID(pbJob.Id),
			Payload:   payload,
			Status:    mapPbStatusToType(pbJob.Status),
			Attempt:   int(pbJob.Attempt),
			Timeout:   time.Duration(pbJob.TimeoutMs) * time.Millisecond,
			CreatedAt: pbJob.CreatedAt,
			UpdatedAt: pbJob.UpdatedAt,
			WorkerID:  pbJob.WorkerId,
		}
		
		if pbJob.DeadlineMs > 0 {
			deadline := pbJob.DeadlineMs
			job.Deadline = &deadline
		}

		jobs = append(jobs, job)
	}

	return jobs, nil
}

// Acknowledge reports job status to the remote Master.
func (s *GrpcJobSource) Acknowledge(ctx context.Context, jobID string, status types.JobStatus, result *Result) error {
	req := &pb.AcknowledgeJobRequest{
		JobId:    jobID,
		WorkerId: s.workerID,
		Status:   mapStatusToPb(status),
		// Result bytes could be sent if we extend the proto
	}

	resp, err := s.client.AcknowledgeJob(ctx, req)
	if err != nil {
		return fmt.Errorf("rpc ack failed: %w", err)
	}
	
	if !resp.Success {
		return fmt.Errorf("master rejected ack")
	}

	return nil
}

// Heartbeat sends a heartbeat to the remote Master.
func (s *GrpcJobSource) Heartbeat(ctx context.Context, nodeID string, load int) error {
	req := &pb.HeartbeatRequest{
		NodeId:      nodeID,
		CurrentLoad: int32(load),
		Timestamp:   time.Now().UnixMilli(),
	}

	resp, err := s.client.SendHeartbeat(ctx, req)
	if err != nil {
		return fmt.Errorf("rpc heartbeat failed: %w", err)
	}

	if resp.ReRegister {
		// Logic to re-register logic should be handled here or propagated.
		// For now, we try to re-register immediately.
		return s.register(ctx)
	}

	return nil
}

func (s *GrpcJobSource) register(ctx context.Context) error {
	req := &pb.RegisterWorkerRequest{
		NodeId:   s.workerID,
		Address:  s.workerAddr,
		Capacity: 10, // Default capacity, could be parameterized
		Tags:     []string{"default"},
	}
	
	resp, err := s.client.RegisterWorker(ctx, req)
	if err != nil {
		return err
	}
	
	if !resp.Success {
		return fmt.Errorf("registration failed")
	}
	
	return nil
}

// Helpers (Duplicated from server for now to avoid shared dependency issues if packages are separated later)
// Ideally these should be in a shared pkg.

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
		return types.StatusPending
	}
}
