package integration

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/internal/raft"
	queueServer "github.com/ChuLiYu/raft-recovery/internal/server"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/credentials/insecure"
	"google.golang.org/grpc/status"
)

type clusterNode struct {
	address   string
	dir       string
	ctrl      *controller.Controller
	rf        *raft.Raft
	transport *raft.GrpcTransport
	server    *grpc.Server
	conn      *grpc.ClientConn
	client    pb.FalconQueueServiceClient
	up        bool
}

func hasRaftSnapshot(dir string) bool {
	b, err := os.ReadFile(filepath.Join(dir, "raft", "raft-state.json"))
	if err != nil {
		return false
	}
	var state struct {
		Index int64 `json:"snapshot_index"`
	}
	return json.Unmarshal(b, &state) == nil && state.Index > 0
}

func startClusterNode(t *testing.T, n *clusterNode, peers []string) {
	t.Helper()
	lis, err := net.Listen("tcp", n.address)
	if err != nil {
		t.Fatal(err)
	}
	n.ctrl, err = controller.NewController(controller.Config{WorkerCount: 1, TaskTimeout: time.Hour, SnapshotInterval: 100 * time.Millisecond, MaxRetry: 3, WALPath: filepath.Join(n.dir, "queue.wal"), SnapshotPath: filepath.Join(n.dir, "queue.snapshot"), DisableDispatchLoop: true})
	if err != nil {
		t.Fatal(err)
	}
	n.transport = raft.NewGrpcTransport()
	n.rf, err = raft.NewPersistentRaft(raft.Config{ID: n.address, Peers: peers, ElectionTimeout: 200 * time.Millisecond, HeartbeatInterval: 40 * time.Millisecond}, filepath.Join(n.dir, "raft"), n.transport, n.ctrl.GetApplyCh())
	if err != nil {
		t.Fatal(err)
	}
	n.ctrl.SetRaftNode(n.rf)
	if err := n.ctrl.Start(); err != nil {
		t.Fatal(err)
	}
	n.server = grpc.NewServer()
	pb.RegisterFalconQueueServiceServer(n.server, queueServer.NewServer(n.ctrl, n.rf))
	go n.server.Serve(lis)
	n.conn, err = grpc.NewClient(n.address, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		t.Fatal(err)
	}
	n.client = pb.NewFalconQueueServiceClient(n.conn)
	n.up = true
	n.rf.Start()
}

func stopClusterNode(n *clusterNode) {
	if !n.up {
		return
	}
	n.up = false
	n.rf.Stop()
	n.server.Stop()
	n.ctrl.Stop()
	n.conn.Close()
	n.transport.Close()
}

func awaitCluster(t *testing.T, description string, predicate func() bool) {
	t.Helper()
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if predicate() {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out: %s", description)
}

func clusterLeader(t *testing.T, nodes []*clusterNode) *clusterNode {
	t.Helper()
	var leader *clusterNode
	awaitCluster(t, "one active leader", func() bool {
		leader = nil
		count := 0
		for _, n := range nodes {
			if n.up {
				if _, ok := n.rf.GetState(); ok {
					leader = n
					count++
				}
			}
		}
		return count == 1
	})
	return leader
}

func submitCluster(t *testing.T, n *clusterNode, id string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	resp, err := n.client.SubmitJob(ctx, &pb.SubmitJobRequest{JobId: id, Payload: []byte(`{"value":"persisted"}`), TimeoutMs: 3600000})
	if err != nil || !resp.GetSuccess() {
		t.Fatalf("submit %s: %v / %v", id, resp, err)
	}
}

// TestRaftClusterDurableFailover exercises the public gRPC boundary, the durable
// Raft implementation and the queue FSM together, with actual loopback sockets.
func TestRaftClusterDurableFailover(t *testing.T) {
	root := t.TempDir()
	nodes := make([]*clusterNode, 3)
	peers := make([]string, 3)
	for i := range nodes {
		lis, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		peers[i] = lis.Addr().String()
		lis.Close()
		nodes[i] = &clusterNode{address: peers[i], dir: filepath.Join(root, fmt.Sprint(i))}
	}
	t.Cleanup(func() {
		for _, n := range nodes {
			stopClusterNode(n)
		}
	})
	for _, n := range nodes {
		startClusterNode(t, n, peers)
	}
	leader := clusterLeader(t, nodes)
	submitCluster(t, leader, "completed-once")
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	if _, err := leader.client.RegisterWorker(ctx, &pb.RegisterWorkerRequest{NodeId: "worker", Capacity: 1}); err != nil {
		t.Fatal(err)
	}
	polled, err := leader.client.PollJobs(ctx, &pb.PollJobsRequest{WorkerId: "worker", MaxJobs: 1})
	if err != nil || len(polled.GetJobs()) != 1 {
		t.Fatalf("poll: %v %v", polled, err)
	}
	job := polled.Jobs[0]
	stale, err := leader.client.AcknowledgeJob(ctx, &pb.AcknowledgeJobRequest{JobId: job.Id, WorkerId: "other-worker", Attempt: job.Attempt, Status: pb.JobStatus_JOB_STATUS_COMPLETED})
	if err != nil || stale.GetSuccess() {
		t.Fatalf("wrong worker ACK accepted: %v %v", stale, err)
	}
	acked, err := leader.client.AcknowledgeJob(ctx, &pb.AcknowledgeJobRequest{JobId: job.Id, WorkerId: "worker", Attempt: job.Attempt, Status: pb.JobStatus_JOB_STATUS_COMPLETED})
	if err != nil || !acked.GetSuccess() {
		t.Fatalf("ACK: %v %v", acked, err)
	}
	submitCluster(t, leader, "pending-after-failover")
	awaitCluster(t, "all replicas apply durable queue", func() bool {
		for _, n := range nodes {
			s := n.ctrl.GetStats()
			if s["completed"] != 1 || s["pending"] != 1 {
				return false
			}
		}
		return true
	})
	for _, n := range nodes {
		if n != leader {
			_, err := n.client.SubmitJob(ctx, &pb.SubmitJobRequest{JobId: "follower-write"})
			if status.Code(err) != codes.FailedPrecondition {
				t.Fatalf("follower write: %v", err)
			}
			break
		}
	}
	// Snapshot interval is intentionally short; verify compaction actually reached disk.
	awaitCluster(t, "snapshot persisted", func() bool { return hasRaftSnapshot(leader.dir) })
	oldLeader := leader
	stopClusterNode(oldLeader)
	leader = clusterLeader(t, nodes)
	submitCluster(t, leader, "new-leader-job")
	startClusterNode(t, oldLeader, peers)
	awaitCluster(t, "restarted former leader catches up", func() bool { s := oldLeader.ctrl.GetStats(); return s["completed"] == 1 && s["pending"] == 2 })
	// Restart every replica from disk, then retry an already completed ID.
	for _, n := range nodes {
		stopClusterNode(n)
	}
	for _, n := range nodes {
		startClusterNode(t, n, peers)
	}
	leader = clusterLeader(t, nodes)
	submitCluster(t, leader, "completed-once")
	awaitCluster(t, "snapshot tombstone prevents duplicate enqueue", func() bool {
		for _, n := range nodes {
			s := n.ctrl.GetStats()
			if s["completed"] != 1 || s["pending"] != 2 {
				return false
			}
		}
		return true
	})
	// A remaining leader may retain its role, but cannot report a committed write.
	for _, n := range nodes {
		if n != leader {
			stopClusterNode(n)
		}
	}
	minorityCtx, minorityCancel := context.WithTimeout(context.Background(), 400*time.Millisecond)
	defer minorityCancel()
	resp, err := leader.client.SubmitJob(minorityCtx, &pb.SubmitJobRequest{JobId: "must-not-commit"})
	if err == nil && resp.GetSuccess() {
		t.Fatal("minority reported successful commit")
	}
	if leader.ctrl.GetStats()["pending"] != 2 {
		t.Fatal("minority proposal mutated FSM")
	}
}
