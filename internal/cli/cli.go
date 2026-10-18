// ============================================================================
// Beaver-Raft CLI - Command Line Interface
// ============================================================================
//
// Package: internal/cli
// File: cli.go
// Purpose: Provides user-friendly command line interface based on Cobra framework
//
// Command Structure:
//   beaver-raft                    # Root command
//   ├── run                        # Start queue system
//   │   └── --config, -c          # Specify config file
//   ├── enqueue                    # Submit jobs
//   │   └── --file, -f            # Specify job JSON file
//   ├── status                     # View system status
//   ├── --version                  # Display version information
//   └── --help                     # Display help information
//
// Configuration Management:
//   Uses YAML format config file (default: configs/default.yaml)
//   Configuration items include:
//   - worker: Worker count and timeout settings
//   - wal: WAL log configuration
//   - snapshot: Snapshot strategy configuration
//   - metrics: Prometheus monitoring configuration
//
// run Command:
//   Starts complete queue system, including:
//   1. Load config file
//   2. Create and start Controller
//   3. Start Metrics HTTP server (if enabled)
//   4. Listen for system signals (SIGINT, SIGTERM)
//   5. Gracefully shutdown system
//
//   Examples:
//     ./beaver-raft run
//     ./beaver-raft run -c custom-config.yaml
//
// enqueue Command:
//   Batch submit jobs from JSON file
//   JSON format:
//   [
//     {
//       "id": "job-1",
//       "payload": {"key": "value"},
//       "timeout_ms": 5000
//     }
//   ]
//
//   Examples:
//     ./beaver-raft enqueue -f jobs.json
//
// status Command:
//   Display system running status:
//   - Config file path
//   - WAL/Snapshot status
//   - Worker status
//
//   Examples:
//     ./beaver-raft status
//
// Signal Handling:
//   run command captures following signals and gracefully shuts down:
//   - SIGINT (Ctrl+C): User interrupt
//   - SIGTERM: System terminate request
//
//   Graceful shutdown flow:
//   1. Stop accepting new jobs
//   2. Wait for current jobs to complete
//   3. Create final snapshot
//   4. Close all resources
//
// Metrics Service:
//   If enabled in config, starts HTTP service in separate goroutine:
//   - Default port: 9090
//   - Path: /metrics
//   - Format: Prometheus format
//
// Error Handling:
//   - Config load failed: Return detailed error information
//   - Controller start failed: Clean up resources and return
//   - Job submission failed: Display error but don't interrupt system
//
// ============================================================================

package cli

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"net"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/internal/raft"
	"github.com/ChuLiYu/raft-recovery/internal/server"
	"github.com/ChuLiYu/raft-recovery/internal/worker"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/spf13/cobra"
	"google.golang.org/grpc"
	"google.golang.org/grpc/credentials/insecure"
	"gopkg.in/yaml.v3"
)

// Config represents the complete system configuration structure
// Maps config file fields through YAML tags
type Config struct {
	Worker struct {
		WorkerCount int           `yaml:"worker_count"`
		TaskTimeout time.Duration `yaml:"task_timeout"`
	} `yaml:"worker"`

	WAL struct {
		Dir              string `yaml:"dir"`
		MaxSegmentSize   int64  `yaml:"max_segment_size"`
		SyncInterval     int    `yaml:"sync_interval"`
		RetentionSeconds int    `yaml:"retention_seconds"`
		BufferSize       int    `yaml:"buffer_size"`
		FlushIntervalMs  int    `yaml:"flush_interval_ms"` // NEW: batch flush interval in ms
	} `yaml:"wal"`

	Snapshot struct {
		Dir             string `yaml:"dir"`
		IntervalSeconds int    `yaml:"interval_seconds"`
		RetentionCount  int    `yaml:"retention_count"`
	} `yaml:"snapshot"`

	Metrics struct {
		Enabled bool `yaml:"enabled"`
		Port    int  `yaml:"port"`
	} `yaml:"metrics"`
}

var (
	configFile string
	globalCtrl *controller.Controller
)

func BuildCLI() *cobra.Command {
	rootCmd := &cobra.Command{
		Use:   "beaver-raft",
		Short: "Beaver-Raft: A crash-recoverable job queue system",
		Long: `Beaver-Raft is a distributed job queue with:
- WAL-based durability
- Snapshot-based recovery
- Prometheus metrics
- Optional replicated master mode using Raft`,
		Version: "1.0.0",
	}

	rootCmd.PersistentFlags().StringVarP(&configFile, "config", "c", "configs/default.yaml", "config file path")

	rootCmd.AddCommand(buildRunCommand())
	rootCmd.AddCommand(buildEnqueueCommand())
	rootCmd.AddCommand(buildStatusCommand())

	return rootCmd
}

func buildRunCommand() *cobra.Command {
	var mode string
	var port int
	var masterAddr string
	var nodeID string
	var peers string
	var dataDir string
	var listenAddr string

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the Beaver-Raft queue system",
		Long:  "Start the system in standalone, master, or worker mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystemWithRaft(mode, port, masterAddr, nodeID, peers, dataDir, listenAddr)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "standalone", "System mode: standalone, master, worker")
	cmd.Flags().IntVar(&port, "port", 50051, "Port to listen on (master mode)")
	cmd.Flags().StringVar(&masterAddr, "master", "", "Master address or comma-separated seed addresses (worker mode)")
	cmd.Flags().StringVar(&nodeID, "node-id", "", "Stable identity for this master (defaults to its listen address)")
	cmd.Flags().StringVar(&peers, "peers", "", "Comma-separated master addresses, including this node; empty means single-node master")
	cmd.Flags().StringVar(&dataDir, "data-dir", "", "Per-node durable Raft data directory")
	cmd.Flags().StringVar(&listenAddr, "listen", "", "Address to bind in master mode (defaults to :port)")

	return cmd
}

func runSystem(mode string, port int, masterAddr string) error {
	return runSystemWithRaft(mode, port, masterAddr, "", "", "", "")
}

func runSystemWithRaft(mode string, port int, masterAddr, nodeID, peerList, dataDir, listenAddr string) error {
	cfg, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Starting Beaver-Raft in %s mode\n", mode)

	if mode == "worker" {
		return runWorkerNode(cfg, masterAddr)
	}
	if mode != "master" && mode != "standalone" {
		return fmt.Errorf("unsupported mode %q (use standalone, master, or worker)", mode)
	}

	// Master or Standalone Mode
	return runControllerNodeWithRaft(cfg, mode, port, nodeID, peerList, dataDir, listenAddr)
}

func runWorkerNode(cfg *Config, masterAddr string) error {
	seeds := 0
	for _, address := range strings.Split(masterAddr, ",") {
		if strings.TrimSpace(address) != "" {
			seeds++
		}
	}
	if seeds == 0 {
		return fmt.Errorf("master address is required in worker mode")
	}

	log.Printf("Connecting to master seeds %s...\n", masterAddr)

	// Create Worker Pool
	pool := worker.NewPool(100)

	// Create JobSource (gRPC)
	workerID := fmt.Sprintf("worker-%d", time.Now().UnixNano())
	source := worker.NewSeedGrpcJobSource(masterAddr, workerID, "")
	defer source.Close()

	// Start Worker Pool with Pull Mode
	if cfg.Worker.WorkerCount <= 0 {
		cfg.Worker.WorkerCount = 1
	}
	log.Printf("Starting %d workers...\n", cfg.Worker.WorkerCount)
	if err := pool.Start(cfg.Worker.WorkerCount, source); err != nil {
		return fmt.Errorf("failed to start worker pool: %w", err)
	}

	// Wait for shutdown signal
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)
	<-sigChan

	log.Println("Stopping worker node...")
	pool.Stop()
	return nil
}

func runControllerNodeWithRaft(cfg *Config, mode string, port int, nodeID, peerList, dataDir, listenAddr string) error {
	log.Printf("Starting Controller with config: %s\n", configFile)
	log.Printf("Workers: %d, Timeout: %s\n", cfg.Worker.WorkerCount, cfg.Worker.TaskTimeout)
	if cfg.Worker.WorkerCount <= 0 {
		cfg.Worker.WorkerCount = 1
	}
	if cfg.Snapshot.IntervalSeconds <= 0 {
		cfg.Snapshot.IntervalSeconds = 10
	}
	if listenAddr == "" {
		if nodeID != "" {
			listenAddr = nodeID
		} else {
			listenAddr = fmt.Sprintf(":%d", port)
		}
	}
	if mode == "master" && nodeID == "" {
		nodeID = listenAddr
	}
	if mode == "master" && dataDir == "" {
		dataDir = filepath.Join("data", "raft", fmt.Sprintf("node-%d", port))
	}
	var members []string
	if mode == "master" {
		var err error
		members, err = resolveMasterMembers(nodeID, peerList)
		if err != nil {
			return err
		}
	}

	// If running in distributed Master mode, disable internal dispatch loops to avoid stealing jobs from remote workers.
	// This is critical for correct distributed operation (see PHASE2_DEBUG_REPORT.md).
	ctrlConfig := controller.Config{
		WorkerCount:         cfg.Worker.WorkerCount,
		TaskTimeout:         cfg.Worker.TaskTimeout,
		SnapshotInterval:    time.Duration(cfg.Snapshot.IntervalSeconds) * time.Second,
		MaxRetry:            3,
		WALPath:             cfg.WAL.Dir,
		SnapshotPath:        cfg.Snapshot.Dir,
		WALBufferSize:       cfg.WAL.BufferSize,
		WALFlushInterval:    time.Duration(cfg.WAL.FlushIntervalMs) * time.Millisecond,
		DisableDispatchLoop: mode == "master",
	}
	if mode == "master" {
		ctrlConfig.WALPath = fmt.Sprintf("%s.%d", ctrlConfig.WALPath, port)
		ctrlConfig.SnapshotPath = fmt.Sprintf("%s.%d", ctrlConfig.SnapshotPath, port)
	}

	ctrl, err := controller.NewController(ctrlConfig)
	if err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	globalCtrl = ctrl

	var rf *raft.Raft
	var transport *raft.GrpcTransport
	var grpcServer *grpc.Server
	var lis net.Listener
	if mode == "master" {
		transport = raft.NewGrpcTransport()
		rf, err = raft.NewPersistentRaft(raft.Config{ID: nodeID, Peers: members}, dataDir, transport, ctrl.GetApplyCh())
		if err != nil {
			return fmt.Errorf("failed to create durable Raft node: %w", err)
		}
		ctrl.SetRaftNode(rf)
	}
	if err := ctrl.Start(); err != nil {
		if rf != nil {
			rf.Stop()
		}
		if transport != nil {
			_ = transport.Close()
		}
		return fmt.Errorf("failed to start controller: %w", err)
	}
	if rf != nil {
		rf.Start()
	}

	if cfg.Metrics.Enabled {
		go func() {
			mux := http.NewServeMux()
			mux.Handle("/metrics", promhttp.Handler())
			addr := fmt.Sprintf(":%d", cfg.Metrics.Port)
			log.Printf("Starting metrics server on %s\n", addr)
			if err := http.ListenAndServe(addr, mux); err != nil {
				log.Printf("Metrics server error: %v\n", err)
			}
		}()
	}
	if mode == "master" {
		lis, err = net.Listen("tcp", listenAddr)
		if err != nil {
			if rf != nil {
				rf.Stop()
			}
			ctrl.Stop()
			if transport != nil {
				_ = transport.Close()
			}
			return fmt.Errorf("failed to listen on %s: %w", listenAddr, err)
		}
		grpcServer = grpc.NewServer()
		srv := server.NewServer(ctrl, rf)
		pb.RegisterFalconQueueServiceServer(grpcServer, srv)
		log.Printf("gRPC/Raft node %s listening on %s; peers=%v data=%s\n", nodeID, listenAddr, members, dataDir)
		go func() {
			if err := grpcServer.Serve(lis); err != nil {
				log.Printf("gRPC server stopped: %v", err)
			}
		}()
	}

	log.Println("System started successfully")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("\nReceived shutdown signal, stopping gracefully...")

	if grpcServer != nil {
		grpcServer.GracefulStop()
	}
	if lis != nil {
		_ = lis.Close()
	}
	if rf != nil {
		rf.Stop()
	}
	ctrl.Stop()
	if transport != nil {
		_ = transport.Close()
	}

	log.Println("System stopped. Goodbye!")
	return nil
}

func resolveMasterMembers(nodeID, peerList string) ([]string, error) {
	var members []string
	for _, peer := range strings.Split(peerList, ",") {
		if peer = strings.TrimSpace(peer); peer != "" {
			members = append(members, peer)
		}
	}
	if len(members) == 0 {
		members = []string{nodeID}
	}
	localHost, localPort, addressErr := net.SplitHostPort(nodeID)
	if addressErr != nil || localPort == "" {
		return nil, fmt.Errorf("master --node-id must be a host:port address, got %q", nodeID)
	}
	if len(members) > 1 && (localHost == "" || localHost == "0.0.0.0" || localHost == "::") {
		return nil, fmt.Errorf("master --node-id %q is not routable in a multi-node cluster; use its advertised host:port", nodeID)
	}
	localMember := false
	for _, member := range members {
		peerHost, peerPort, err := net.SplitHostPort(member)
		if err != nil || peerPort == "" || (len(members) > 1 && (peerHost == "" || peerHost == "0.0.0.0" || peerHost == "::")) {
			return nil, fmt.Errorf("master peer %q must be a routable host:port address", member)
		}
		if member == nodeID {
			localMember = true
		}
	}
	if !localMember {
		return nil, fmt.Errorf("local node ID %q must appear in --peers; peers contain cluster node identities and addresses", nodeID)
	}
	return members, nil
}

func buildEnqueueCommand() *cobra.Command {
	var jobFile string
	var masterAddr string

	cmd := &cobra.Command{
		Use:   "enqueue",
		Short: "Enqueue jobs from a JSON file",
		Long:  "Read job definitions from a JSON file and enqueue them. Use --master to submit to a remote master.",
		RunE: func(cmd *cobra.Command, args []string) error {
			if jobFile == "" {
				return fmt.Errorf("job file is required (use --file or -f)")
			}
			return enqueueJobs(jobFile, masterAddr)
		},
	}

	cmd.Flags().StringVarP(&jobFile, "file", "f", "", "JSON file containing job definitions")
	cmd.Flags().StringVar(&masterAddr, "master", "", "Master address or comma-separated seed addresses for remote submission")
	cmd.MarkFlagRequired("file")

	return cmd
}

func enqueueJobs(filePath string, masterAddresses ...string) error {
	masterAddr := ""
	if len(masterAddresses) > 0 {
		masterAddr = masterAddresses[0]
	}
	data, err := os.ReadFile(filePath)
	if err != nil {
		return fmt.Errorf("failed to read job file: %w", err)
	}

	var jobsInput []struct {
		ID      string                 `json:"id"`
		Payload map[string]interface{} `json:"payload"`
		Timeout int64                  `json:"timeout_ms"`
	}

	if err := json.Unmarshal(data, &jobsInput); err != nil {
		return fmt.Errorf("failed to parse job file: %w", err)
	}

	// Mode 1: Remote Submission (gRPC)
	if masterAddr != "" {
		var seeds []string
		for _, seed := range strings.Split(masterAddr, ",") {
			if seed = strings.TrimSpace(seed); seed != "" {
				seeds = append(seeds, seed)
			}
		}
		if len(seeds) == 0 {
			return fmt.Errorf("no master addresses provided")
		}

		successCount := 0
		for _, j := range jobsInput {
			payloadBytes, _ := json.Marshal(j.Payload)
			var lastErr error
			for _, seed := range seeds {
				conn, dialErr := grpc.NewClient(seed, grpc.WithTransportCredentials(insecure.NewCredentials()))
				if dialErr != nil {
					lastErr = dialErr
					continue
				}
				ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
				resp, callErr := pb.NewFalconQueueServiceClient(conn).SubmitJob(ctx, &pb.SubmitJobRequest{JobId: j.ID, Payload: payloadBytes, TimeoutMs: j.Timeout})
				cancel()
				_ = conn.Close()
				if callErr != nil {
					lastErr = callErr
					continue
				}
				if resp.Success {
					successCount++
					lastErr = nil
					break
				}
				lastErr = fmt.Errorf("master rejected job: %s", resp.ErrorMessage)
			}
			if lastErr != nil {
				log.Printf("Failed to submit job %s: %v\n", j.ID, lastErr)
			}
		}
		log.Printf("Successfully submitted %d/%d jobs to %s\n", successCount, len(jobsInput), masterAddr)
		return nil
	}

	// Mode 2: Local Submission (Direct Controller)
	if globalCtrl == nil {
		cfg, err := loadConfig(configFile)
		if err != nil {
			return fmt.Errorf("failed to load config: %w", err)
		}

		ctrlConfig := controller.Config{
			WorkerCount:      cfg.Worker.WorkerCount,
			TaskTimeout:      cfg.Worker.TaskTimeout,
			SnapshotInterval: time.Duration(cfg.Snapshot.IntervalSeconds) * time.Second,
			MaxRetry:         3,
			WALPath:          cfg.WAL.Dir,
			SnapshotPath:     cfg.Snapshot.Dir,
			WALBufferSize:    cfg.WAL.BufferSize,
			WALFlushInterval: time.Duration(cfg.WAL.FlushIntervalMs) * time.Millisecond,
		}

		ctrl, err := controller.NewController(ctrlConfig)
		if err != nil {
			return fmt.Errorf("failed to create controller: %w", err)
		}

		globalCtrl = ctrl
		if err := ctrl.Start(); err != nil {
			return fmt.Errorf("failed to start controller: %w", err)
		}
	}

	var jobs []types.Job
	for _, j := range jobsInput {
		jobs = append(jobs, types.Job{
			ID:      types.JobID(j.ID),
			Payload: j.Payload,
			Timeout: time.Duration(j.Timeout) * time.Millisecond,
		})
	}

	log.Printf("Enqueuing %d jobs from %s locally\n", len(jobs), filePath)
	if err := globalCtrl.EnqueueJobs(jobs); err != nil {
		return fmt.Errorf("failed to enqueue jobs: %w", err)
	}

	log.Printf("Successfully enqueued %d jobs locally\n", len(jobs))
	return nil
}

func buildStatusCommand() *cobra.Command {
	cmd := &cobra.Command{
		Use:   "status",
		Short: "Show system status",
		Long:  "Display job queue statistics and system health",
		RunE: func(cmd *cobra.Command, args []string) error {
			return showStatus()
		},
	}
	return cmd
}

func showStatus() error {
	cfg, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	fmt.Println("\n╔═══════════════════════════════════════════════════════════╗")
	fmt.Println("║           Beaver-Raft System Status                       ║")
	fmt.Println("╚═══════════════════════════════════════════════════════════╝")
	fmt.Println()

	// System Configuration
	fmt.Println("📋 Configuration:")
	fmt.Printf("  └─ Config File:     %s\n", configFile)
	fmt.Printf("  └─ Worker Count:    %d\n", cfg.Worker.WorkerCount)
	fmt.Printf("  └─ Task Timeout:    %s\n", cfg.Worker.TaskTimeout)
	fmt.Printf("  └─ Snapshot Every:  %ds\n", cfg.Snapshot.IntervalSeconds)
	fmt.Println()

	// Storage Configuration
	fmt.Println("💾 Storage:")
	fmt.Printf("  ├─ WAL Directory:       %s\n", cfg.WAL.Dir)
	fmt.Printf("  │  └─ Buffer Size:      %d entries\n", cfg.WAL.BufferSize)
	fmt.Printf("  │  └─ Max Segment Size: %.1f MB\n", float64(cfg.WAL.MaxSegmentSize)/(1024*1024))
	fmt.Printf("  └─ Snapshot Directory:  %s\n", cfg.Snapshot.Dir)
	fmt.Printf("     └─ Retention Count:  %d\n", cfg.Snapshot.RetentionCount)
	fmt.Println()

	// Job Queue Statistics (if controller is running)
	if globalCtrl != nil {
		stats := globalCtrl.GetStats()
		total := stats["pending"] + stats["in_flight"] + stats["completed"] + stats["dead"]

		fmt.Println("📊 Job Queue Statistics:")
		fmt.Printf("  ├─ Total Jobs:     %d\n", total)
		fmt.Printf("  ├─ ⏳ Pending:      %d\n", stats["pending"])
		fmt.Printf("  ├─ 🔄 In-Flight:    %d\n", stats["in_flight"])
		fmt.Printf("  ├─ ✅ Completed:    %d\n", stats["completed"])
		fmt.Printf("  └─ ❌ Dead:         %d\n", stats["dead"])
		fmt.Println()

		// Calculate success rate
		if total > 0 {
			successRate := float64(stats["completed"]) / float64(total) * 100
			fmt.Printf("📈 Success Rate: %.1f%%\n", successRate)
			fmt.Println()
		}
	} else {
		fmt.Println("📊 Job Queue Statistics:")
		fmt.Println("  └─ Controller not running (run 'beaver-raft run' to start)")
		fmt.Println()
	}

	// Metrics Status
	fmt.Println("📡 Metrics:")
	if cfg.Metrics.Enabled {
		fmt.Printf("  └─ Status: ✅ Enabled on http://localhost:%d/metrics\n", cfg.Metrics.Port)
	} else {
		fmt.Println("  └─ Status: ⚠️  Disabled")
	}
	fmt.Println()

	fmt.Println("═══════════════════════════════════════════════════════════")
	return nil
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("failed to read config file: %w", err)
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("failed to parse config YAML: %w", err)
	}

	return &cfg, nil
}
