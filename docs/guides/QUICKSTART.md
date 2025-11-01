# Quick Start Guide

English | [Chinese version](QUICKSTART.zh-CN.md)

> Understand the project structure and start contributing to Beaver-Raft quickly

## Prerequisites

- Go 1.23+
- macOS or Linux
- Make installed

## One-line demo

```bash
make demo
```

This builds the project, starts the server, enqueues jobs, simulates a crash, restarts, and verifies recovery.

## Manual run (developer flow)

```bash
# 1) Build
make build

# 2) Start the server (terminal 1)
./bin/beaver-raft run --workers 8

# 3) Enqueue jobs (terminal 2)
./bin/beaver-raft enqueue --file test/jobs.json

# 4) Check status and metrics
./bin/beaver-raft status
curl http://localhost:9090/metrics
```

## Project structure (high-level)

```text
beaver-raft/
├── cmd/queue/          # CLI entry point
├── internal/
│   ├── controller/     # Core orchestration (4 loops)
│   ├── jobmanager/     # Job state machine
│   ├── worker/         # Worker pool
│   ├── storage/
│   │   ├── wal/        # Write-Ahead Log
│   │   └── snapshot/   # Snapshot manager
│   ├── cli/            # Command-line interface
│   └── metrics/        # Prometheus metrics
├── docs/               # Documentation
└── scripts/            # Helper scripts
```

## Development workflow

```bash
# Create a feature branch
git checkout -b feature/my-change

# Run unit tests (all modules)
go test ./internal/...

# Run race detector
go test -race ./...

# Run integration tests
go test ./test/integration/...

# Benchmarks and coverage (optional)
go test -bench=. ./...
go test -cover ./...

# Commit and open a PR
git commit -m "feat: my change"
```

## Key modules overview

- JobManager (`internal/jobmanager/`)
  - Maintains job lifecycle: PENDING → IN_FLIGHT → COMPLETED/FAILED
  - Enqueue, dequeue, mark in-flight, mark completed/failed
  - Finds timeouts and enforces invariants

- Controller (`internal/controller/`)
  - Four loops: dispatch, result, timeout, snapshot
  - Orchestrates JobManager, Worker Pool, WAL, Snapshot Manager

- WAL (`internal/storage/wal/`)
  - Append-only operation log with CRC32 checksum and fsync
  - Replay to rebuild state on startup

- Snapshot Manager (`internal/snapshot/`)
  - Periodic full-state snapshots for fast recovery

- Worker Pool (`internal/worker/`)
  - Fixed-size goroutine pool, timeout with context, graceful shutdown

- Metrics (`internal/metrics/`)
  - Prometheus counters, gauges, histograms; HTTP endpoint at :9090

## Useful make targets

```bash
make help       # List available targets
make build      # Build binary
make test       # Run unit tests
make bench      # Run benchmarks
make coverage   # Generate coverage report
make clean      # Remove build artifacts
```

## Troubleshooting

- Port in use → choose a different `--metrics-port`
- Permission denied → `chmod +x ./bin/beaver-raft ./scripts/demo.sh`
- Jobs stuck → `./bin/beaver-raft status` to inspect queue/worker state

## Where to read next

- Usage Guide: `docs/guides/USAGE_GUIDE.md`
- Architecture: `docs/architecture/phase1-architecture.md`
- Implementation Order: `docs/planning/IMPLEMENTATION_ORDER.md`

---

Happy coding 🦫
