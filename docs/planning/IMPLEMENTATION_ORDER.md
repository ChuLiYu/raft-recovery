# Beaver-Raft Phase 1: Implementation Order

English | [Chinese version](IMPLEMENTATION_ORDER.zh-CN.md)

This guide provides a clear, test-first implementation path for Phase 1. It minimizes cross-module dependencies so you can deliver working increments and validate each step before moving on.

— 3 weeks, 11 steps, bottom-up approach.

## Overview

- Total duration: ~3 weeks
- Approach: Bottom-up (foundations first), test early and often
- Success criteria: all tests pass, no data loss, recovery < 3s, race detector clean

## Suggested timeline

- Week 1: Foundations (types, JobManager, WAL, Snapshot)
- Week 2: Execution (Worker Pool, Controller, Metrics)
- Week 3: Interfaces and polish (CLI, Integration tests, Docs, Demo & Performance)

## Implementation steps

### Step 1: Project setup and types (Day 1)

- Files
  - pkg/types/types.go – Core type definitions (Job, InFlightInfo, JobStatus, Config)
  - go.mod – Dependencies
  - Makefile – Build/test automation
- Deliverables
  - Types defined and imported where needed
  - Build succeeds
- Verification
  - go build ./...

### Step 2: JobManager (Days 2–3)

- File: internal/jobmanager/job_manager.go (+ unit tests)
- Responsibilities
  - Maintain queue, in_flight, completed, dead (each job lives in exactly one set)
  - State transitions: Enqueue → InFlight → Completed/Dead; Requeue; timeout handling
- Core functions
  - Enqueue(job), PopPending(), MarkInFlight(jobID, deadlineMs)
  - MarkCompleted(jobID), MarkDead(jobID), Requeue(job)
  - GetExpiredJobs(now), GetJob(jobID), Validate()
- Tests: invariants, FIFO ordering, concurrency, timeouts
- Verification
  - go test -v ./internal/jobmanager/
  - go test -race ./internal/jobmanager/

### Step 3: WAL (Days 4–6)

- Files
  - internal/storage/wal/types.go – Event structure
  - internal/storage/wal/checksum.go – CRC32 checksum helpers
  - internal/storage/wal/wal.go – WAL implementation
- Event: Event{Seq, Type, JobID, Timestamp, Checksum}
- API: NewWAL(path), Append(eventType, jobID), Replay(handler func(Event) error), Rotate(), Close()
- Tests: append/replay, corruption detection, rotation, concurrency
- Verification
  - go test -v ./internal/storage/wal/
  - go test -race ./internal/storage/wal/

### Step 4: Snapshot manager (Days 6–7)

- File: internal/snapshot/snapshot_manager.go (+ tests)
- Responsibilities
  - Save/Load atomic snapshots (temp file + rename)
  - Validate schema/version, maintain last sequence
  - Periodic scheduling
- Tests: write/load, atomicity (interrupted write), version mismatch
- Verification
  - go test -v ./internal/snapshot/

### Step 5: Worker pool (Days 7–9)

- Files: internal/worker/worker.go, internal/worker/worker_pool.go (+ tests)
- API: NewPool(size), Start(n), Submit(task), ReceiveResult(), Stop()
- Tests: concurrency, timeout, graceful shutdown

### Step 6: Controller (Days 9–12)

- File: internal/controller/controller.go (+ tests)
- Four loops
  1) Dispatch: Pop → WAL DISPATCH → send to workers
  2) Result: receive → WAL ACK/RETRY → update state
  3) Timeout: scan in_flight → retry or dead-letter
  4) Snapshot: periodic state persistence
- Startup flow: load snapshot → replay WAL (idempotent) → start loops
- Tests: crash recovery (< 3s), idempotency, throughput sanity

### Step 7: Metrics (Day 13)

- File: internal/metrics/metrics.go
- Expose Prometheus metrics (examples): jobs_enqueued_total, jobs_completed_total, jobs_failed_total, jobs_in_flight, recovery_time_seconds
- Tests: collection and endpoint registration (use isolated registries in tests)

### Step 8: CLI (Days 14–15)

- Files: internal/cli/cli.go, cmd/queue/main.go
- Commands: run, enqueue, status
- Tests: command parsing, basic integration

### Step 9: Integration tests (Day 16)

- Folder: test/integration/
- Scenarios: end-to-end processing, crash recovery, high load

### Step 10: Documentation (Day 17)

- Files: README.md, docs/guides/USAGE_GUIDE.md, architecture docs
- Update architecture section, quickstart, usage, and recovery guarantees

### Step 11: Demo & performance polish (Days 18–21)

- Scripts: scripts/demo.sh, Make targets (build/test/demo/clean)
- Tuning: WAL batch writes, lock contention, GC/allocs; targets: recovery < 3s, throughput ≥ 200 jobs/s

## Dependency graph (high level)

```text
Types → JobManager → WAL → Snapshot → Worker Pool → Controller → Metrics/CLI → Integration/Docs/Demo
```

## Success criteria

- All unit tests pass at each step before proceeding
- No race detector issues (go test -race)
- Recovery time < 3s in integration tests
- No data loss across crash-and-restart scenarios

## Common pitfalls

- WAL durability: ensure fsync/sync writes where required
- Controller: avoid goroutine leaks and lock inversion; keep replay idempotent
- Snapshot: always write via temp + atomic rename; validate schema/version
- Metrics tests: use per-test registries to avoid duplicate registration

## Tools and commands (optional)

```bash
# Run tests
go test ./...

# Race detector
go test -race ./...

# Coverage
go test -cover ./...
```

For a detailed Chinese explanation of each step, see IMPLEMENTATION_ORDER.zh-CN.md.

