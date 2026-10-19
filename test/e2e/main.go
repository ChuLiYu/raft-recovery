// e2e-probe exercises the public gRPC contract from inside the Compose network.
package main

import (
	"context"
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"os"
	"strings"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

const defaultMasters = "master1:50051,master2:50051,master3:50051"

type endpoint struct {
	address string
	conn    *grpc.ClientConn
	client  pb.FalconQueueServiceClient
}

func main() {
	if len(os.Args) < 2 {
		fatalf("usage: e2e-probe <leader|submit|drain|no-delivery|minority> [flags]")
	}
	var err error
	switch os.Args[1] {
	case "leader":
		err = leaderCommand(os.Args[2:])
	case "submit":
		err = submitCommand(os.Args[2:])
	case "drain":
		err = drainCommand(os.Args[2:])
	case "no-delivery":
		err = noDeliveryCommand(os.Args[2:])
	case "minority":
		err = minorityCommand(os.Args[2:])
	default:
		err = fmt.Errorf("unknown command %q", os.Args[1])
	}
	if err != nil {
		fatalf("%v", err)
	}
}

func fatalf(format string, args ...any) {
	fmt.Fprintf(os.Stderr, "e2e-probe: "+format+"\n", args...)
	os.Exit(1)
}

func parseFlags(name string, args []string, extra func(*flag.FlagSet)) ([]*endpoint, *flag.FlagSet, error) {
	fs := flag.NewFlagSet(name, flag.ContinueOnError)
	fs.SetOutput(os.Stderr)
	masters := fs.String("masters", defaultMasters, "comma-separated master addresses")
	if extra != nil {
		extra(fs)
	}
	if err := fs.Parse(args); err != nil {
		return nil, fs, err
	}
	var eps []*endpoint
	for _, addr := range strings.Split(*masters, ",") {
		addr = strings.TrimSpace(addr)
		if addr == "" {
			continue
		}
		conn, err := grpc.NewClient(addr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return nil, fs, fmt.Errorf("connect setup for %s: %w", addr, err)
		}
		eps = append(eps, &endpoint{address: addr, conn: conn, client: pb.NewFalconQueueServiceClient(conn)})
	}
	if len(eps) == 0 {
		return nil, fs, errors.New("no master addresses provided")
	}
	return eps, fs, nil
}

func closeEndpoints(eps []*endpoint) {
	for _, ep := range eps {
		_ = ep.conn.Close()
	}
}

func leaderCommand(args []string) error {
	eps, _, err := parseFlags("leader", args, nil)
	if err != nil {
		return err
	}
	defer closeEndpoints(eps)
	deadline := time.Now().Add(60 * time.Second)
	for time.Now().Before(deadline) {
		for _, ep := range eps {
			ctx, cancel := context.WithTimeout(context.Background(), 1200*time.Millisecond)
			id := fmt.Sprintf("e2e-leader-check-%d", time.Now().UnixNano())
			resp, callErr := ep.client.RegisterWorker(ctx, &pb.RegisterWorkerRequest{NodeId: id, Capacity: 1})
			cancel()
			if callErr == nil && resp.GetSuccess() {
				fmt.Println(strings.TrimSuffix(ep.address, ":50051"))
				return nil
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return errors.New("no leader accepted RegisterWorker within 60 seconds")
}

func submitCommand(args []string) error {
	var id, payload string
	var timeout int64
	eps, _, err := parseFlags("submit", args, func(fs *flag.FlagSet) {
		fs.StringVar(&id, "id", "", "unique job ID")
		fs.StringVar(&payload, "payload", `{"source":"orb-e2e"}`, "JSON payload")
		fs.Int64Var(&timeout, "timeout-ms", 60000, "job timeout in milliseconds")
	})
	if err != nil {
		return err
	}
	defer closeEndpoints(eps)
	if id == "" {
		return errors.New("--id is required")
	}
	if !json.Valid([]byte(payload)) {
		return errors.New("--payload must be valid JSON")
	}
	deadline := time.Now().Add(45 * time.Second)
	var lastErr error
	for time.Now().Before(deadline) {
		for _, ep := range eps {
			ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
			resp, callErr := ep.client.SubmitJob(ctx, &pb.SubmitJobRequest{JobId: id, Payload: []byte(payload), TimeoutMs: timeout})
			cancel()
			if callErr == nil && resp.GetSuccess() && resp.GetJobId() == id {
				fmt.Printf("submitted %s via %s\n", id, ep.address)
				return nil
			}
			if callErr != nil {
				lastErr = callErr
			} else if resp.GetSuccess() {
				lastErr = fmt.Errorf("server returned unexpected job ID %q", resp.GetJobId())
			} else {
				lastErr = fmt.Errorf("submit rejected at %s: %s", ep.address, resp.GetErrorMessage())
			}
		}
		time.Sleep(250 * time.Millisecond)
	}
	return fmt.Errorf("job %s was not accepted within 45 seconds: %v", id, lastErr)
}

func drainCommand(args []string) error {
	var workerID, ids string
	eps, _, err := parseFlags("drain", args, func(fs *flag.FlagSet) {
		fs.StringVar(&workerID, "worker-id", "e2e-probe-worker", "worker identity used for gRPC poll and ACK")
		fs.StringVar(&ids, "ids", "", "comma-separated exact job IDs expected")
	})
	if err != nil {
		return err
	}
	defer closeEndpoints(eps)
	if workerID == "" || ids == "" {
		return errors.New("--worker-id and --ids are required")
	}
	want := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		id = strings.TrimSpace(id)
		if id != "" {
			want[id] = true
		}
	}
	if len(want) == 0 {
		return errors.New("--ids contains no IDs")
	}
	deadline := time.Now().Add(60 * time.Second)
	var active *endpoint
	for time.Now().Before(deadline) && active == nil {
		for _, ep := range eps {
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			resp, callErr := ep.client.RegisterWorker(ctx, &pb.RegisterWorkerRequest{NodeId: workerID, Capacity: 1})
			cancel()
			if callErr == nil && resp.GetSuccess() {
				active = ep
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if active == nil {
		return errors.New("probe worker could not register with a leader")
	}
	acked := map[string]bool{}
	for time.Now().Before(deadline) && len(acked) < len(want) {
		if active == nil {
			for _, ep := range eps {
				ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
				r, registerErr := ep.client.RegisterWorker(ctx, &pb.RegisterWorkerRequest{NodeId: workerID, Capacity: 1})
				cancel()
				if registerErr == nil && r.GetSuccess() {
					active = ep
					break
				}
			}
			if active == nil {
				time.Sleep(200 * time.Millisecond)
				continue
			}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
		resp, callErr := active.client.PollJobs(ctx, &pb.PollJobsRequest{WorkerId: workerID, MaxJobs: 10})
		cancel()
		if callErr != nil {
			active = nil
			time.Sleep(200 * time.Millisecond)
			continue
		}
		for _, job := range resp.GetJobs() {
			if !want[job.GetId()] {
				return fmt.Errorf("unexpected job delivered: %s", job.GetId())
			}
			if acked[job.GetId()] {
				return fmt.Errorf("job delivered more than once: %s", job.GetId())
			}
			if job.GetAttempt() != 1 {
				return fmt.Errorf("job %s attempt = %d, want 1", job.GetId(), job.GetAttempt())
			}
			var body map[string]any
			if err := json.Unmarshal(job.GetPayload(), &body); err != nil {
				return fmt.Errorf("job %s has invalid payload: %w", job.GetId(), err)
			}
			if body["e2e_id"] != job.GetId() {
				return fmt.Errorf("job %s payload e2e_id = %v", job.GetId(), body["e2e_id"])
			}
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			ack, ackErr := active.client.AcknowledgeJob(ctx, &pb.AcknowledgeJobRequest{JobId: job.GetId(), WorkerId: workerID, Status: pb.JobStatus_JOB_STATUS_COMPLETED, Attempt: job.GetAttempt()})
			cancel()
			if ackErr != nil {
				return fmt.Errorf("ACK %s attempt %d: %w", job.GetId(), job.GetAttempt(), ackErr)
			}
			if !ack.GetSuccess() {
				return fmt.Errorf("ACK rejected for %s attempt %d", job.GetId(), job.GetAttempt())
			}
			// The server must accept an idempotent repeat for this exact completed assignment.
			ctx, cancel = context.WithTimeout(context.Background(), 4*time.Second)
			dup, dupErr := active.client.AcknowledgeJob(ctx, &pb.AcknowledgeJobRequest{JobId: job.GetId(), WorkerId: workerID, Status: pb.JobStatus_JOB_STATUS_COMPLETED, Attempt: job.GetAttempt()})
			cancel()
			if dupErr != nil || !dup.GetSuccess() {
				return fmt.Errorf("duplicate ACK for completed job %s was not idempotent: response=%v err=%v", job.GetId(), dup, dupErr)
			}
			acked[job.GetId()] = true
			fmt.Printf("ACK %s attempt %d payload=%s\n", job.GetId(), job.GetAttempt(), string(job.GetPayload()))
		}
		time.Sleep(100 * time.Millisecond)
	}
	if len(acked) != len(want) {
		missing := make([]string, 0)
		for id := range want {
			if !acked[id] {
				missing = append(missing, id)
			}
		}
		return fmt.Errorf("timed out waiting for exact pending IDs to ACK: %s", strings.Join(missing, ","))
	}
	return nil
}

func noDeliveryCommand(args []string) error {
	var workerID, ids string
	var duration time.Duration
	eps, _, err := parseFlags("no-delivery", args, func(fs *flag.FlagSet) {
		fs.StringVar(&workerID, "worker-id", "e2e-dedup-worker", "worker identity")
		fs.StringVar(&ids, "ids", "", "completed IDs that must not reappear")
		fs.DurationVar(&duration, "duration", 3*time.Second, "observation window")
	})
	if err != nil {
		return err
	}
	defer closeEndpoints(eps)
	want := map[string]bool{}
	for _, id := range strings.Split(ids, ",") {
		if id = strings.TrimSpace(id); id != "" {
			want[id] = true
		}
	}
	if workerID == "" || len(want) == 0 {
		return errors.New("--worker-id and --ids are required")
	}
	deadline := time.Now().Add(15 * time.Second)
	var active *endpoint
	for time.Now().Before(deadline) && active == nil {
		for _, ep := range eps {
			ctx, cancel := context.WithTimeout(context.Background(), 1500*time.Millisecond)
			r, e := ep.client.RegisterWorker(ctx, &pb.RegisterWorkerRequest{NodeId: workerID, Capacity: 1})
			cancel()
			if e == nil && r.GetSuccess() {
				active = ep
				break
			}
		}
		time.Sleep(150 * time.Millisecond)
	}
	if active == nil {
		return errors.New("could not register no-delivery probe worker")
	}
	stop := time.Now().Add(duration)
	for time.Now().Before(stop) {
		ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
		resp, callErr := active.client.PollJobs(ctx, &pb.PollJobsRequest{WorkerId: workerID, MaxJobs: 10})
		cancel()
		if callErr != nil {
			if status.Code(callErr) != codes.FailedPrecondition {
				return fmt.Errorf("no-delivery poll: %w", callErr)
			}
			return fmt.Errorf("leader changed during no-delivery check; restart this check: %w", callErr)
		}
		for _, job := range resp.GetJobs() {
			return fmt.Errorf("unexpected job delivered during no-delivery check: %s attempt %d", job.GetId(), job.GetAttempt())
		}
		time.Sleep(100 * time.Millisecond)
	}
	fmt.Printf("no completed IDs redelivered during %s\n", duration)
	return nil
}

func minorityCommand(args []string) error {
	var id string
	eps, _, err := parseFlags("minority", args, func(fs *flag.FlagSet) {
		fs.StringVar(&id, "id", "", "unique job ID that must be rejected in minority")
	})
	if err != nil {
		return err
	}
	defer closeEndpoints(eps)
	if id == "" {
		return errors.New("--id is required")
	}
	for _, ep := range eps {
		ctx, cancel := context.WithTimeout(context.Background(), 1800*time.Millisecond)
		payload, _ := json.Marshal(map[string]string{"e2e_id": id, "marker": "minority-write"})
		resp, callErr := ep.client.SubmitJob(ctx, &pb.SubmitJobRequest{JobId: id, Payload: payload, TimeoutMs: 60000})
		cancel()
		if callErr == nil && resp.GetSuccess() {
			return fmt.Errorf("minority acknowledged write %s via %s", id, ep.address)
		}
	}
	fmt.Printf("minority did not acknowledge %s\n", id)
	return nil
}
