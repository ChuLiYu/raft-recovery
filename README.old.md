# Beaver-Raft Initiative

Beaver-Raft is a staged journey from a resilient single-node job queue to a distributed, Raft-backed task system with partial snapshots. The program provides interview-ready demos early while paving the way toward research-grade architecture.

## Phase Highlights

- **Phase 1 – Snapshot-Aware Job Queue:** Goroutine-based workers, WAL + JSON snapshots, fast crash recovery.
- **Phase 2 – FalconQueue:** Multi-node deployment with HTTP RPC, registry/heartbeat, observability stack.
- **Phase 3 – Beaver-Raft:** Raft consensus integrated with Beaver-style partial snapshots for efficient recovery.

## Documentation

- Roadmap overview: [`docs/roadmap.md`](docs/roadmap.md)
- Phase 1 white & yellow papers: [`docs/phase1-snapshot-aware-job-queue.md`](docs/phase1-snapshot-aware-job-queue.md)
- Phase 2 white & yellow papers: [`docs/phase2-falconqueue.md`](docs/phase2-falconqueue.md)
- Phase 3 white & yellow papers: [`docs/phase3-beaver-raft.md`](docs/phase3-beaver-raft.md)

## Delivery Checklist

- [ ] Architecture diagrams and README visuals
- [ ] `make demo` or `docker compose up` showcasing end-to-end flow
- [ ] Prometheus metrics endpoints for each phase
- [ ] Unit, integration, and chaos tests (including `go test -race`)
- [ ] Load and recovery benchmarks tracked per phase

## Getting Started

1. Review the roadmap to understand scope and sequencing.
2. Implement the Phase 1 job queue skeleton and automate crash-recovery demos.
3. Layer in distributed coordination for FalconQueue, then progress to Beaver-Raft consensus.

## Status Tracking

- Record KPI results (throughput, recovery time, availability) per phase.
- Document fault injection outcomes and mitigations alongside code changes.
- Maintain English commit messages and tag milestones at phase completion.

