package worker

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"sync"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
)

// GrpcJobSource connects to one or more master addresses. Seed addresses are
// tried in rotation so workers recover from an unavailable node or stale leader.
type GrpcJobSource struct {
	workerID   string
	workerAddr string
	addresses  []string
	legacyConn grpc.ClientConnInterface
	mu         sync.Mutex
	conns      map[string]*grpc.ClientConn
	registered map[string]bool
	next       int
}

const seedRPCTimeout = 2 * time.Second

func NewGrpcJobSource(conn grpc.ClientConnInterface, workerID string, address string) *GrpcJobSource {
	return &GrpcJobSource{workerID: workerID, workerAddr: address, addresses: []string{""}, legacyConn: conn, conns: make(map[string]*grpc.ClientConn), registered: make(map[string]bool)}
}

// NewSeedGrpcJobSource creates a source from comma-separated seed addresses.
func NewSeedGrpcJobSource(addresses, workerID, address string) *GrpcJobSource {
	var seeds []string
	for _, seed := range strings.Split(addresses, ",") {
		if seed = strings.TrimSpace(seed); seed != "" {
			seeds = append(seeds, seed)
		}
	}
	return &GrpcJobSource{workerID: workerID, workerAddr: address, addresses: seeds, conns: make(map[string]*grpc.ClientConn), registered: make(map[string]bool)}
}

func (s *GrpcJobSource) client(address string) (pb.FalconQueueServiceClient, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	conn := s.conns[address]
	if conn == nil {
		if address == "" {
			if s.legacyConn != nil {
				return pb.NewFalconQueueServiceClient(s.legacyConn), nil
			}
			return nil, fmt.Errorf("no gRPC connection configured")
		}
		var err error
		conn, err = grpc.NewClient(address, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, err
		}
		s.conns[address] = conn
	}
	return pb.NewFalconQueueServiceClient(conn), nil
}

func (s *GrpcJobSource) begin() (int, string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.addresses) == 0 {
		return 0, ""
	}
	return s.next % len(s.addresses), s.addresses[s.next%len(s.addresses)]
}

func (s *GrpcJobSource) advance(i int, address string) {
	s.mu.Lock()
	s.registered[address] = false
	if len(s.addresses) > 0 {
		s.next = (i + 1) % len(s.addresses)
	}
	s.mu.Unlock()
}

func (s *GrpcJobSource) ensureRegistered(ctx context.Context, address string, client pb.FalconQueueServiceClient) error {
	s.mu.Lock()
	ok := s.registered[address]
	s.mu.Unlock()
	if ok {
		return nil
	}
	resp, err := client.RegisterWorker(ctx, &pb.RegisterWorkerRequest{NodeId: s.workerID, Address: s.workerAddr, Capacity: 10, Tags: []string{"default"}})
	if err != nil {
		return err
	}
	if !resp.Success {
		return fmt.Errorf("worker registration rejected")
	}
	s.mu.Lock()
	s.registered[address] = true
	s.mu.Unlock()
	return nil
}

func (s *GrpcJobSource) Poll(ctx context.Context, maxJobs int) ([]*types.Job, error) {
	start, _ := s.begin()
	var lastErr error
	for n := 0; n < len(s.addresses); n++ {
		i := (start + n) % len(s.addresses)
		address := s.addresses[i]
		seedCtx, cancel := context.WithTimeout(ctx, seedRPCTimeout)
		client, err := s.client(address)
		if err == nil {
			err = s.ensureRegistered(seedCtx, address, client)
		}
		var resp *pb.PollJobsResponse
		if err == nil {
			resp, err = client.PollJobs(seedCtx, &pb.PollJobsRequest{WorkerId: s.workerID, MaxJobs: int32(maxJobs)})
		}
		if err != nil {
			cancel()
			lastErr = err
			s.advance(i, address)
			continue
		}
		cancel()
		s.mu.Lock()
		s.next = i
		s.mu.Unlock()
		jobs := make([]*types.Job, 0, len(resp.Jobs))
		for _, pbJob := range resp.Jobs {
			var payload map[string]interface{}
			if len(pbJob.Payload) > 0 {
				if err := json.Unmarshal(pbJob.Payload, &payload); err != nil {
					payload = make(map[string]interface{})
				}
			}
			job := &types.Job{ID: types.JobID(pbJob.Id), Payload: payload, Status: mapPbStatusToType(pbJob.Status), Attempt: int(pbJob.Attempt), Timeout: time.Duration(pbJob.TimeoutMs) * time.Millisecond, CreatedAt: pbJob.CreatedAt, UpdatedAt: pbJob.UpdatedAt, WorkerID: pbJob.WorkerId}
			if pbJob.DeadlineMs > 0 {
				deadline := pbJob.DeadlineMs
				job.Deadline = &deadline
			}
			jobs = append(jobs, job)
		}
		return jobs, nil
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no master seed addresses configured")
	}
	return nil, lastErr
}

func (s *GrpcJobSource) Acknowledge(ctx context.Context, jobID string, status types.JobStatus, result *Result) error {
	start, _ := s.begin()
	var lastErr error
	for n := 0; n < len(s.addresses); n++ {
		i := (start + n) % len(s.addresses)
		address := s.addresses[i]
		seedCtx, cancel := context.WithTimeout(ctx, seedRPCTimeout)
		client, err := s.client(address)
		if err == nil {
			err = s.ensureRegistered(seedCtx, address, client)
		}
		if err == nil {
			attempt := int32(0)
			if result != nil {
				attempt = int32(result.Attempt)
			}
			resp, callErr := client.AcknowledgeJob(seedCtx, &pb.AcknowledgeJobRequest{JobId: jobID, WorkerId: s.workerID, Status: mapStatusToPb(status), Attempt: attempt})
			err = callErr
			if err == nil && resp.Success {
				cancel()
				s.mu.Lock()
				s.next = i
				s.mu.Unlock()
				return nil
			}
			if err == nil {
				err = fmt.Errorf("master rejected acknowledgement")
			}
		}
		cancel()
		lastErr = err
		s.advance(i, address)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no master seed addresses configured")
	}
	return lastErr
}

func (s *GrpcJobSource) Heartbeat(ctx context.Context, nodeID string, load int) error {
	start, _ := s.begin()
	var lastErr error
	for n := 0; n < len(s.addresses); n++ {
		i := (start + n) % len(s.addresses)
		address := s.addresses[i]
		seedCtx, cancel := context.WithTimeout(ctx, seedRPCTimeout)
		client, err := s.client(address)
		if err == nil {
			err = s.ensureRegistered(seedCtx, address, client)
		}
		if err == nil {
			if nodeID == "" {
				nodeID = s.workerID
			}
			resp, callErr := client.SendHeartbeat(seedCtx, &pb.HeartbeatRequest{NodeId: nodeID, CurrentLoad: int32(load), Timestamp: time.Now().UnixMilli()})
			err = callErr
			if err == nil && resp.ReRegister {
				s.mu.Lock()
				s.registered[address] = false
				s.mu.Unlock()
				err = s.ensureRegistered(seedCtx, address, client)
			}
			if err == nil {
				cancel()
				s.mu.Lock()
				s.next = i
				s.mu.Unlock()
				return nil
			}
		}
		cancel()
		lastErr = err
		s.advance(i, address)
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no master seed addresses configured")
	}
	return lastErr
}

func (s *GrpcJobSource) Close() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	var first error
	for _, conn := range s.conns {
		if conn != nil {
			if err := conn.Close(); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

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
