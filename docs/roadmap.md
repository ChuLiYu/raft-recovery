# Beaver-Raft Program Roadmap

This roadmap sequences the Beaver-Raft initiative across three phases that gradually
increase resiliency, distribution, and research sophistication while keeping risk
low and deliverables demonstrable.

## Objectives

- Deliver a crash-tolerant job system that can be demonstrated locally in weeks.
- Expand to a multi-node deployment that exercises cloud design competencies.
- Finish with a research-grade Raft-based architecture that showcases deep systems expertise.

## Phase Overview

| Phase | Name                       | Core Themes                       | Primary Goal                                       |
|-------|---------------------------|-----------------------------------|----------------------------------------------------|
| 1     | Snapshot-Aware Job Queue  | Goroutines, snapshots, WAL        | Build a high-scoring, interview-ready foundation   |
| 2     | FalconQueue               | HTTP RPC, multi-node resilience   | Demonstrate cloud-scale engineering capabilities   |
| 3     | Beaver-Raft               | Raft consensus, partial snapshot  | Present research-level architecture for top roles  |

## Milestones and Budget

| Phase | Duration  | Budget (CAD) | Environment              |
|-------|-----------|--------------|--------------------------|
| 1     | 3 weeks   | 0–30         | Local development        |
| 2     | 10 weeks  | 50–80        | Cloud VMs + Grafana      |
| 3     | 12–16 weeks | 120–180    | 3-node cluster           |
| **Total** | **6–7 months** | **200–300** |                          |

## Shared Deliverables

- README with one-line value proposition, architecture diagram, and performance summary.
- `make demo` or `docker compose up` to showcase end-to-end flow.
- Prometheus metrics for observability.
- Unit, integration, and chaos testing suites.

## Validation Strategy

- Adopt `go test -race` as a pre-commit guard from Phase 1 onward.
- Execute fault-injection scenarios (process kill, I/O errors, network partitions).
- Track KPIs per phase and record them in the dashboard and documentation.

