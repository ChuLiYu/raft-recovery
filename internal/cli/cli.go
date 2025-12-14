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
	"syscall"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/ChuLiYu/raft-recovery/internal/controller"
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
- Sub-3 second recovery time`,
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

	cmd := &cobra.Command{
		Use:   "run",
		Short: "Start the Beaver-Raft queue system",
		Long:  "Start the system in standalone, master, or worker mode",
		RunE: func(cmd *cobra.Command, args []string) error {
			return runSystem(mode, port, masterAddr)
		},
	}

	cmd.Flags().StringVar(&mode, "mode", "standalone", "System mode: standalone, master, worker")
	cmd.Flags().IntVar(&port, "port", 50051, "Port to listen on (master mode)")
	cmd.Flags().StringVar(&masterAddr, "master", "", "Master address (worker mode)")

	return cmd
}

func runSystem(mode string, port int, masterAddr string) error {
	cfg, err := loadConfig(configFile)
	if err != nil {
		return fmt.Errorf("failed to load config: %w", err)
	}

	log.Printf("Starting Beaver-Raft in %s mode\n", mode)

	if mode == "worker" {
		return runWorkerNode(cfg, masterAddr)
	}

	// Master or Standalone Mode
	return runControllerNode(cfg, mode, port)
}

func runWorkerNode(cfg *Config, masterAddr string) error {
	if masterAddr == "" {
		return fmt.Errorf("master address is required in worker mode")
	}

	log.Printf("Connecting to master at %s...\n", masterAddr)
	
	conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
	if err != nil {
		return fmt.Errorf("failed to connect to master: %w", err)
	}
	defer conn.Close()

	// Create Worker Pool
	pool := worker.NewPool(100)
	
	// Create JobSource (gRPC)
	workerID := fmt.Sprintf("worker-%d", time.Now().UnixNano())
	source := worker.NewGrpcJobSource(conn, workerID, "") // Address is optional for now

	// Start Worker Pool with Pull Mode
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

func runControllerNode(cfg *Config, mode string, port int) error {
	log.Printf("Starting Controller with config: %s\n", configFile)
	log.Printf("Workers: %d, Timeout: %s\n", cfg.Worker.WorkerCount, cfg.Worker.TaskTimeout)

	// If running in distributed Master mode, disable internal dispatch loops to avoid stealing jobs from remote workers.
	// This is critical for correct distributed operation (see PHASE2_DEBUG_REPORT.md).
	ctrlConfig := controller.Config{
		WorkerCount:      cfg.Worker.WorkerCount,
		TaskTimeout:      cfg.Worker.TaskTimeout,
		SnapshotInterval: time.Duration(cfg.Snapshot.IntervalSeconds) * time.Second,
		MaxRetry:         3,
		WALPath:          cfg.WAL.Dir,
		SnapshotPath:     cfg.Snapshot.Dir,
		WALBufferSize:    cfg.WAL.BufferSize,
		WALFlushInterval: time.Duration(cfg.WAL.FlushIntervalMs) * time.Millisecond,
		DisableDispatchLoop: mode == "master", // <-- Key fix: disables local dispatchers in Master mode
	}

	ctrl, err := controller.NewController(ctrlConfig)
	if err != nil {
		return fmt.Errorf("failed to create controller: %w", err)
	}

	globalCtrl = ctrl

	// Start Metrics
	if cfg.Metrics.Enabled {
		go func() {
			http.Handle("/metrics", promhttp.Handler())
			addr := fmt.Sprintf(":%d", cfg.Metrics.Port)
			log.Printf("Starting metrics server on %s\n", addr)
			if err := http.ListenAndServe(addr, nil); err != nil {
				log.Printf("Metrics server error: %v\n", err)
			}
		}()
	}

	// Start Controller
	if err := ctrl.Start(); err != nil {
		return fmt.Errorf("failed to start controller: %w", err)
	}

	// If Master mode, start gRPC server
	if mode == "master" {
		lis, err := net.Listen("tcp", fmt.Sprintf(":%d", port))
		if err != nil {
			return fmt.Errorf("failed to listen on port %d: %w", port, err)
		}
		
		grpcServer := grpc.NewServer()
		srv := server.NewServer(ctrl)
		pb.RegisterFalconQueueServiceServer(grpcServer, srv)
		
		log.Printf("gRPC Server listening on :%d\n", port)
		
		go func() {
			if err := grpcServer.Serve(lis); err != nil {
				log.Fatalf("gRPC server failed: %v", err)
			}
		}()
	}

	log.Println("System started successfully")

	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	<-sigChan
	log.Println("\nReceived shutdown signal, stopping gracefully...")

	ctrl.Stop()

	log.Println("System stopped. Goodbye!")
	return nil
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
	cmd.Flags().StringVar(&masterAddr, "master", "", "Master address (e.g. localhost:50051) for remote submission")
	cmd.MarkFlagRequired("file")

	return cmd
}

func enqueueJobs(filePath string, masterAddr string) error {
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
		conn, err := grpc.NewClient(masterAddr, grpc.WithTransportCredentials(insecure.NewCredentials()))
		if err != nil {
			return fmt.Errorf("failed to connect to master: %w", err)
		}
		defer conn.Close()

		client := pb.NewFalconQueueServiceClient(conn)
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()

		successCount := 0
		for _, j := range jobsInput {
			payloadBytes, _ := json.Marshal(j.Payload)
			req := &pb.SubmitJobRequest{
				JobId:     j.ID,
				Payload:   payloadBytes,
				TimeoutMs: j.Timeout,
			}
			
			resp, err := client.SubmitJob(ctx, req)
			if err != nil {
				log.Printf("Failed to submit job %s: %v\n", j.ID, err)
				continue
			}
			if !resp.Success {
				log.Printf("Master rejected job %s: %s\n", j.ID, resp.ErrorMessage)
				continue
			}
			successCount++
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
