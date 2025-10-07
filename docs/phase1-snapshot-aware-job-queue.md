# Phase 1 — Snapshot-Aware Job Queue

## White Paper (High-Level Design)

### Value Proposition

- Demonstrate concurrent processing, crash recovery, and restartable snapshots on a single node.
- Provide a demo-friendly story: kill the process, restart, and have the queue restore itself automatically.

### Scope

- Single-node controller coordinating multiple worker goroutines.
- Job queue with retry, timeout, and deduplication semantics.
- Persistent state via JSON snapshot plus write-ahead log (WAL).
- No network or distributed communication concerns.

### Key KPIs

| Metric                     | Target           | Notes                                  |
|----------------------------|------------------|----------------------------------------|
| Crash-to-restart recovery  | < 3 s            | Accept ±1 s overhead during JSON load  |
| Throughput (1k jobs)       | ≥ 200 jobs/s     | Simulated CPU-bound workload           |
| Race detector              | `go test -race`  | Must pass before each submission       |

### Risks and Mitigations

- **Corrupted JSON snapshot** → Dual persistence (WAL + snapshot) with checksums.
- **Race conditions** → Guard shared maps with `sync.Mutex`.
- **`fsync` overhead** → Batch flushes or time-windowed `fsync`.

### Three-Week Milestones

| Week | Focus                                                | Deliverables                                 |
|------|------------------------------------------------------|----------------------------------------------|
| 1    | Architecture skeleton: controller, worker, WAL       | Sample `main.go`, minimal CLI                |
| 2    | Retry/timeout/idempotency + snapshot recovery        | WAL replay tests, initial metrics            |
| 3    | Demo automation, README visuals, load testing        | Demo script, README diagrams, performance doc|

## Yellow Paper (Implementation Blueprint)

### Core Modules

| Module             | Responsibility                         | Techniques                               |
|--------------------|----------------------------------------|-------------------------------------------|
| `Controller`       | Dispatch jobs, manage queue state      | Channels, mutexes                         |
| `Worker`           | Execute workloads, report completions  | Context, deadlines/timeouts               |
| `SnapshotManager`  | Persist and restore queue state        | JSON encoding, atomic file rename         |
| `WAL`              | Append event log, replay on crash      | Append-only file, checksums               |

### State Schema (JSON)

```json
{
  "queue": [{"id": "t-001", "payload": {}, "attempt": 0}],
  "in_flight": {"t-001": {"worker": "W1", "deadline_ms": 1730790000}},
  "completed": ["t-000"],
  "last_seq": 42,
  "schema_version": 1
}
```

### Invariants

1. Each job resides in exactly one of `queue`, `in_flight`, or `completed`.
2. WAL + snapshot must replay to a single legal state.
3. `attempt` counter is monotonically increasing; exceeding the limit sends jobs to the dead-letter queue.

### Core Algorithms (Pseudocode)

```
DispatchLoop:
  for t := pop(queue):
    lock()
      writeWAL("DISPATCH", t.id)
      in_flight[t.id] = deadline(now + timeout)
    unlock()
    sendToWorker(t)

AckHandler(t):
  lock()
    writeWAL("ACK", t.id)
    delete(in_flight, t.id)
    append(completed, t.id)
  unlock()

SnapshotTick:
  every Δs:
    lock()
      atomicWrite(snapshot)
      rotateWAL()
    unlock()
```

### CLI Surface

```
queue enqueue --file jobs.json
queue run --workers 8 --timeout 3s --snapshot 2s
queue status
```

### Test Matrix

- **Unit tests:** WAL append/replay, state invariants.
- **Race testing:** `go test -race`.
- **Fault injection:** Random process kill, I/O error simulation.
- **Performance:** Measure P95 latency and recovery time.

