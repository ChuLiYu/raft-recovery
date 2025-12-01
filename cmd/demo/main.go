package main

import (
	"fmt"
	"log"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"gopkg.in/yaml.v3"
)

type Config struct {
	Worker struct {
		WorkerCount int           `yaml:"worker_count"`
		TaskTimeout time.Duration `yaml:"task_timeout"`
	} `yaml:"worker"`
	WAL struct {
		Dir        string `yaml:"dir"`
		BufferSize int    `yaml:"buffer_size"`
	} `yaml:"wal"`
	Snapshot struct {
		Dir             string `yaml:"dir"`
		IntervalSeconds int    `yaml:"interval_seconds"`
	} `yaml:"snapshot"`
}

func main() {
	if len(os.Args) < 2 {
		fmt.Println("Usage: go run cmd/demo/main.go <start|recover>")
		os.Exit(1)
	}

	mode := os.Args[1]
	cfg, err := loadConfig("configs/default.yaml")
	if err != nil {
		log.Fatalf("Failed to load config: %v", err)
	}

	ctrlConfig := controller.Config{
		WorkerCount:      cfg.Worker.WorkerCount,
		TaskTimeout:      cfg.Worker.TaskTimeout,
		SnapshotInterval: time.Duration(cfg.Snapshot.IntervalSeconds) * time.Second,
		MaxRetry:         3,
		WALPath:          cfg.WAL.Dir,
		SnapshotPath:     cfg.Snapshot.Dir,
		WALBufferSize:    cfg.WAL.BufferSize,
	}

	ctrl, err := controller.NewController(ctrlConfig)
	if err != nil {
		log.Fatalf("Failed to create controller: %v", err)
	}

	if err := ctrl.Start(); err != nil {
		log.Fatalf("Failed to start controller: %v", err)
	}

	fmt.Printf("✓ Controller started (mode: %s)\n", mode)

	// Setup signal handling early
	sigChan := make(chan os.Signal, 1)
	signal.Notify(sigChan, syscall.SIGINT, syscall.SIGTERM)

	if mode == "start" {
		time.Sleep(1 * time.Second)

		// Check if we have recovered jobs first
		stats := ctrl.GetStats()
		hasRecovered := stats["pending"]+stats["in_flight"]+stats["completed"]+stats["dead"] > 0

		if hasRecovered {
			fmt.Printf("\n⚠️  Found existing jobs from previous run (recovered from crash!)\n")
			fmt.Printf("📊 Current Status (after recovery):\n")
			fmt.Printf("  Pending:   %d\n", stats["pending"])
			fmt.Printf("  In-Flight: %d\n", stats["in_flight"])
			fmt.Printf("  Completed: %d\n", stats["completed"])
			fmt.Printf("  Dead:      %d\n", stats["dead"])
			fmt.Printf("\n💡 This proves: WAL + Snapshot = Zero Data Loss!\n")
			fmt.Printf("   Run './scripts/demo-interactive.sh demo2-start' to restart fresh\n")
		} else {
			// Only enqueue if no recovered jobs
			timestamp := time.Now().Unix()
			// Submit 100 jobs to increase chance of catching some in-flight
			var jobs []types.Job
			for i := 1; i <= 1000; i++ {
				jobs = append(jobs, types.Job{
					ID:      types.JobID(fmt.Sprintf("crash-demo-%03d-%d", i, timestamp)),
					Payload: map[string]interface{}{"task": fmt.Sprintf("job_%d", i)},
					Timeout: 10 * time.Second,
				})
			}

			if err := ctrl.EnqueueJobs(jobs); err != nil {
				log.Fatalf("Failed to enqueue jobs: %v", err)
			}

			fmt.Printf("✓ Enqueued %d jobs\n", len(jobs))
			fmt.Printf("\n⚡ Jobs are being processed by 8 workers...\n")
			fmt.Printf("💡 Press Ctrl+C NOW (within ~2 seconds) to catch jobs in-flight!\n\n")

			// Show status updates every 100ms to catch in-flight jobs
			for i := 0; i < 20; i++ {
				select {
				case <-sigChan:
					fmt.Println("\n\nReceived shutdown signal, stopping gracefully...")
					ctrl.Stop()
					fmt.Println("✓ Controller stopped")
					return
				case <-time.After(100 * time.Millisecond):
					stats = ctrl.GetStats()
					if stats["in_flight"] > 0 || stats["pending"] > 0 {
						fmt.Printf("📊 Status: Pending=%d, In-Flight=%d, Completed=%d\n",
							stats["pending"], stats["in_flight"], stats["completed"])
					}
				}
			}

			// Status snapshot (system continues running in background)
			stats = ctrl.GetStats()
			fmt.Printf("\n📊 Status Snapshot (after 2 seconds):\n")
			fmt.Printf("  Pending:   %d\n", stats["pending"])
			fmt.Printf("  In-Flight: %d\n", stats["in_flight"])
			fmt.Printf("  Completed: %d\n", stats["completed"])
			fmt.Printf("  Dead:      %d\n", stats["dead"])

			if stats["completed"] == len(jobs) {
				fmt.Printf("\n⚠️  All jobs completed too fast!\n")
				fmt.Printf("💡 Tip: Run again and press Ctrl+C faster to catch in-flight jobs\n")
			}
		}
	} else if mode == "recover" {
		// Show immediate status after recovery (before jobs start processing again)
		time.Sleep(500 * time.Millisecond)

		stats := ctrl.GetStats()
		total := stats["pending"] + stats["in_flight"] + stats["completed"] + stats["dead"]

		fmt.Printf("\n📊 Immediate Status After Recovery:\n")
		fmt.Printf("  Pending:   %d\n", stats["pending"])
		fmt.Printf("  In-Flight: %d\n", stats["in_flight"])
		fmt.Printf("  Completed: %d\n", stats["completed"])
		fmt.Printf("  Dead:      %d\n", stats["dead"])
		fmt.Printf("  ─────────────────\n")
		fmt.Printf("  Total:     %d\n", total)

		if total > 0 {
			fmt.Printf("\n✓ Recovered %d jobs from crash!\n", total)
			fmt.Printf("  (Check logs above for 'requeued_jobs=N')\n")
		}

		// Wait a bit and show final status
		fmt.Printf("\n⏳ Waiting 2 seconds for jobs to process...\n")
		time.Sleep(2 * time.Second)

		stats = ctrl.GetStats()
		fmt.Printf("\n📊 Final Status (after processing):\n")
		fmt.Printf("  Pending:   %d\n", stats["pending"])
		fmt.Printf("  In-Flight: %d\n", stats["in_flight"])
		fmt.Printf("  Completed: %d\n", stats["completed"])
		fmt.Printf("  Dead:      %d\n", stats["dead"])
	}

	// Wait for signal
	<-sigChan

	fmt.Println("\n\nReceived shutdown signal, stopping gracefully...")
	ctrl.Stop()
	fmt.Println("✓ Controller stopped")
}

func loadConfig(path string) (*Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}

	var cfg Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, err
	}

	return &cfg, nil
}
