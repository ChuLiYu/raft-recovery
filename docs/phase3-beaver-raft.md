# Phase 3 — Beaver-Raft (Consensus + Partial Snapshot)

## White Paper (High-Level Design)

### Value Proposition

- Combine Raft strong consistency with Beaver partial snapshots to highlight deep systems engineering.
- Suitable for a master-level project or advanced interview portfolio piece.

### Scope

- Implement Raft leader election, log replication, and commit indexing.
- Apply Beaver-style partial snapshots retaining only the necessary subset (e.g., in-flight jobs, cursor hashes).
- Support simulations across a three-node cluster.

### Key KPIs

| Metric          | Target                               |
|-----------------|--------------------------------------|
| Leader failover | < 3 s                                |
| Recovery time   | 30–50% faster than full snapshot     |
| Snapshot size   | ≥ 40% reduction versus full snapshot |

### Risks and Mitigations

- **Incorrect partial set definition** → Formal verification and replay testing.
- **Raft error handling gaps** → Start with proven library integration before custom implementation.

### Milestones (12+ Weeks)

1. Minimal viable Raft (leader election + log replication).
2. Map queue commands to Raft log entries.
3. Integrate Beaver partial snapshot mechanism and benchmark.
4. Simulate geo latency, document results, and draft final paper.

## Yellow Paper (Implementation Blueprint)

### State Machine

- Commands: `ENQ`, `ACK`, `TIMEOUT`, `DLQ`.
- State components: `Queue`, `InFlight`, `Cursor`, `DLQ`.

### Invariants

1. All committed commands apply in identical order on every node.
2. Partial snapshot + log replay must reproduce full state.
3. Idempotent ACK logic ensures safe replay after failures.

### Beaver Partial Snapshot Definition

| Component | Contents                                   |
|-----------|--------------------------------------------|
| Queue     | Cursor position plus hash fingerprint      |
| InFlight  | Complete representation of in-flight items |
| Completed | Recent N-window summary data               |

### Recovery Flow

```
LoadPartialSnapshot()
RebuildIndexes()
ReplayLog(from = snapshotIndex + 1)
Verify(fingerprints)
Serve()
```

### Test Plan

- Measure leader failover latency, log commit time, and snapshot compression ratio.
- Fault injection: leader crash, follower lag, network partitions.

