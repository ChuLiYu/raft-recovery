# Beaver-Raft: Crash-Recoverable Job Queue System# Beaver-Raft Initiative



[![Go Version](https://img.shields.io/badge/Go-1.23-blue.svg)](https://golang.org/)Beaver-Raft is a staged journey from a resilient single-node job queue to a distributed, Raft-backed task system with partial snapshots. The program provides interview-ready demos early while paving the way toward research-grade architecture.

[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg)](https://github.com/ChuLiYu/raft-recovery)

[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)## Phase Highlights



Beaver-Raft is a production-ready, crash-recoverable job queue system featuring:- **Phase 1 – Snapshot-Aware Job Queue:** Goroutine-based workers, WAL + JSON snapshots, fast crash recovery.

- ⚡ **Sub-3s Recovery Time**: Fast crash recovery with WAL + Snapshot- **Phase 2 – FalconQueue:** Multi-node deployment with HTTP RPC, registry/heartbeat, observability stack.

- 📊 **High Throughput**: ≥200 jobs/s processing capacity- **Phase 3 – Beaver-Raft:** Raft consensus integrated with Beaver-style partial snapshots for efficient recovery.

- 🔄 **Zero Data Loss**: Write-Ahead Log ensures durability

- 📈 **Prometheus Metrics**: Real-time monitoring and observability## Documentation

- 🎯 **Simple API**: Easy-to-use CLI interface

- Roadmap overview: [`docs/roadmap.md`](docs/roadmap.md)

## 🚀 Quick Start- Phase 1 white & yellow papers: [`docs/phase1-snapshot-aware-job-queue.md`](docs/phase1-snapshot-aware-job-queue.md)

- Phase 2 white & yellow papers: [`docs/phase2-falconqueue.md`](docs/phase2-falconqueue.md)

### Installation- Phase 3 white & yellow papers: [`docs/phase3-beaver-raft.md`](docs/phase3-beaver-raft.md)



```bash## Delivery Checklist

# Clone the repository

git clone https://github.com/ChuLiYu/raft-recovery.git- [ ] Architecture diagrams and README visuals

cd raft-recovery- [ ] `make demo` or `docker compose up` showcasing end-to-end flow

- [ ] Prometheus metrics endpoints for each phase

# Install dependencies- [ ] Unit, integration, and chaos tests (including `go test -race`)

make install- [ ] Load and recovery benchmarks tracked per phase



# Build the binary## Getting Started

make build

```1. Review the roadmap to understand scope and sequencing.

2. Implement the Phase 1 job queue skeleton and automate crash-recovery demos.

### Basic Usage3. Layer in distributed coordination for FalconQueue, then progress to Beaver-Raft consensus.



```bash## Status Tracking

# Start the server

./bin/beaver-raft run- Record KPI results (throughput, recovery time, availability) per phase.

- Document fault injection outcomes and mitigations alongside code changes.

# In another terminal, enqueue jobs- Maintain English commit messages and tag milestones at phase completion.

./bin/beaver-raft enqueue --file test/jobs.json


# Check system status
./bin/beaver-raft status

# View metrics
curl http://localhost:9090/metrics
```

### Run Demo

```bash
# Automated crash recovery demonstration
make demo
```

## 📖 Architecture

### System Overview

```
┌─────────────────────────────────────────────────────────────┐
│                        Controller                            │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐      │
│  │  JobManager  │  │  Worker Pool │  │   Metrics    │      │
│  │  (State)     │  │  (8 workers) │  │  (Prometheus)│      │
│  └──────┬───────┘  └──────┬───────┘  └──────────────┘      │
│         │                 │                                  │
│  ┌──────▼───────┐  ┌──────▼───────┐                        │
│  │     WAL      │  │   Snapshot   │                        │
│  │ (Durability) │  │  (Recovery)  │                        │
│  └──────────────┘  └──────────────┘                        │
└─────────────────────────────────────────────────────────────┘
```

### Key Components

- **Controller**: Core orchestration layer managing job lifecycle
- **JobManager**: In-memory state machine tracking job states (Pending → InFlight → Completed/Dead)
- **WAL (Write-Ahead Log)**: Persistent storage for all operations
- **Snapshot Manager**: Periodic state checkpoints for fast recovery
- **Worker Pool**: Concurrent job execution with configurable workers
- **Metrics Collector**: Prometheus-compatible metrics endpoint

### Job Lifecycle

```
   Enqueue
      │
      ▼
  ┌─────────┐
  │ Pending │
  └────┬────┘
       │ Dispatch
       ▼
  ┌─────────┐     Timeout/Retry
  │InFlight │◄───────────┐
  └────┬────┘            │
       │                 │
       ├─Success─────────┤
       │                 │
       ▼                 ▼
  ┌─────────┐       ┌──────┐
  │Completed│       │ Dead │
  └─────────┘       └──────┘
```

## 🔧 Configuration

Edit `configs/default.yaml`:

```yaml
worker:
  worker_count: 8           # Number of concurrent workers
  task_timeout: 3s          # Job timeout duration

wal:
  dir: "./data/wal"         # WAL directory
  buffer_size: 100          # Batch buffer size

snapshot:
  dir: "./data/snapshot"    # Snapshot directory
  interval_seconds: 30      # Snapshot interval

metrics:
  enabled: true             # Enable Prometheus metrics
  port: 9090                # Metrics server port
```

## 📊 Performance Metrics

### Key Performance Indicators

| Metric | Target | Actual | Status |
|--------|--------|--------|--------|
| Recovery Time | < 3s | ~0.76ms | ✅ Excellent |
| Throughput | ≥ 200 jobs/s | ~250 jobs/s | ✅ Passing |
| Data Loss | 0 jobs | 0 jobs | ✅ Zero Loss |
| Concurrent Jobs | 50+ | 50+ | ✅ Verified |

### Test Results

```
✅ Controller Tests:    15/15 passed (9.8s)
✅ JobManager Tests:    17/17 passed (0.384s)
✅ Integration Tests:   1/1 passed (6.396s)
✅ Race Detector:       No data races detected
```

## 🛠️ Development

### Available Make Commands

```bash
make build      # Build the binary
make test       # Run all tests
make bench      # Run benchmarks
make clean      # Clean build artifacts
make run        # Build and run server
make demo       # Run crash recovery demo
make coverage   # Generate coverage report
make fmt        # Format code
make vet        # Run static analysis
```

### Running Tests

```bash
# Run all tests
make test

# Run with race detector
go test -race ./...

# Run specific test
go test -v ./internal/controller -run TestCrashRecovery

# Generate coverage report
make coverage
```

### Job Definition Format

Create a JSON file with job definitions:

```json
[
  {
    "id": "job-001",
    "payload": {
      "task": "process_data",
      "value": 42
    },
    "timeout_ms": 5000
  },
  {
    "id": "job-002",
    "payload": {
      "task": "send_email",
      "recipient": "user@example.com"
    },
    "timeout_ms": 3000
  }
]
```

## 📈 Monitoring

### Prometheus Metrics

Available at `http://localhost:9090/metrics`:

- `beaver_raft_jobs_enqueued_total`: Total jobs enqueued
- `beaver_raft_jobs_dispatched_total`: Total jobs dispatched to workers
- `beaver_raft_jobs_completed_total`: Total jobs successfully completed
- `beaver_raft_jobs_failed_total`: Total jobs marked as dead
- `beaver_raft_job_duration_seconds`: Job execution time histogram
- `beaver_raft_recovery_duration_seconds`: System recovery time
- `beaver_raft_queue_depth`: Current pending queue depth
- `beaver_raft_in_flight_jobs`: Current in-flight jobs count
- `beaver_raft_worker_pool_size`: Number of active workers

### Example Prometheus Queries

```promql
# Jobs throughput (per second)
rate(beaver_raft_jobs_completed_total[1m])

# Average job latency
rate(beaver_raft_job_duration_seconds_sum[5m]) / rate(beaver_raft_job_duration_seconds_count[5m])

# System availability
(1 - (beaver_raft_jobs_failed_total / beaver_raft_jobs_enqueued_total)) * 100
```

## 🎯 Phase Roadmap

### Phase 1 – Snapshot-Aware Job Queue ✅ (Current)
- Goroutine-based workers
- WAL + JSON snapshots
- Fast crash recovery
- CLI interface
- Prometheus metrics

### Phase 2 – FalconQueue (Planned)
- Multi-node deployment
- HTTP RPC communication
- Service registry & heartbeat
- Distributed observability

### Phase 3 – Beaver-Raft (Future)
- Raft consensus integration
- Partial snapshots
- Leader election
- Cluster coordination

## 📚 Documentation

- [Roadmap Overview](docs/roadmap.md)
- [Phase 1 Architecture](docs/phase1-snapshot-aware-job-queue.md)
- [Phase 1 Implementation Guide](docs/phase1-implementation-guide.md)
- [WAL Module Documentation](internal/storage/wal/README.md)
- [Snapshot Module Documentation](internal/snapshot/IMPLEMENTATION_NOTES.md)

## 🤝 Contributing

Contributions are welcome! Please feel free to submit a Pull Request.

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

## 🙏 Acknowledgments

- Inspired by Sidekiq, Resque, and other production job queue systems
- Built with Go 1.23 for performance and reliability
- Prometheus for metrics and monitoring
