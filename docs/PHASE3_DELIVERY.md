# Phase 3 implementation audit and delivery contract

[中文](PHASE3_DELIVERY.zh-CN.md) · [Interview guide](INTERVIEW_GUIDE.md) · [Run the cluster](../README.md)

Audit date: 2026-10-18. Baseline: `ChuLiYu/raft-recovery`, main commit `c6a8c283431e8cc9ec9aa9324e4344734fbcd9db`. This report compares the original roadmap and phase plans with executable code. Original planning documents remain historical records; unchecked boxes there are not a live progress tracker.

## Delivery boundary

The implementation delivers the functional core through Phase 3: durable standalone queue recovery, gRPC workers, and a fixed-membership cluster with a custom Raft election/replication implementation. Raft is implemented in this repository, rather than delegated to a consensus library.

The original roadmap also proposed research-grade Beaver snapshots, deployment exercises and performance targets. Those are **not all complete**. This delivery uses a queue checkpoint with completed-job tombstones, and an atomic Raft state image rather than the planned segmented WAL. The differences and remaining work are explicit below. Passing recovery tests does not establish a production SLA or a formal correctness proof.

## Original plan → implementation

| Area | Baseline gap | Delivered behavior | Evidence |
|---|---|---|---|
| Local durability | WAL lacked complete payload recovery; sequence/replay and write-error paths were unsafe | Full-job versioned records, payload checksums, synchronous write acknowledgements, restart sequence recovery, fail-closed corruption handling | `internal/storage/wal/durability_test.go`, WAL tests |
| Local checkpoints | Terminal deduplication and FIFO could be lost; rotation raced with mutations | FIFO, active jobs, complete DLQ and completed tombstones; serialized snapshot/rotation boundary | JobManager and controller tests |
| Remote execution | CLI integration was broken; failover reconnection and attempt identity were incomplete | Working CLI, multi-seed submission/polling, registration/heartbeat, worker+attempt ACK fencing and clean shutdown | CLI/worker tests; process-kill demo |
| Election | Voting omitted log freshness and durable state | Randomized timeout, persistent term/vote, last-term/index voting, higher-term step-down | `TestVoteRequiresFreshLogAndPersists` |
| Replication | In-memory log, incomplete commit/error handling | Durable append, prefix checks, conflict repair, per-peer serialized replication, current-term commit rule and leader no-op | `TestPartitionCommitConflictRepair`, `TestAppendEntriesCommitsOnlyMatchedPrefix` |
| Client completion | Proposal acceptance could be confused with success | Wait for majority commit, matching proposal identity, and ordered local FSM application | Raft proposal replacement test; controller apply tests |
| Queue consensus | Dispatch/timeout still mutated local state | ENQUEUE, DISPATCH, ACK and TIMEOUT use one replicated deterministic FSM | `internal/jobmanager/replicated_test.go` |
| Snapshots | No complete durable InstallSnapshot path | Persisted snapshot index/term plus compacted log; snapshot install and suffix catch-up | `TestSingleNodeDurableRestartSnapshot`, `TestSnapshotCatchup` |
| Cluster lifecycle | CLI did not assemble a durable Raft cluster | Explicit static peers, per-node directories, routable IDs, transport/server shutdown | Real gRPC integration and `scripts/demo-three-node-failover.sh` |
| Claims and onboarding | README contained unverified performance/exactly-once claims | Runnable three-node instructions, honest limits, architecture/interview notes, CI | README and paired delivery/interview documents |

## Acceptance contract

These rules define the implemented behavior and what a later change must preserve.

1. **Acknowledged write durability.** A successful queue write in master mode has been committed through a majority of the configured peers and applied by the serving node. A minority cannot acknowledge a new commit. A timeout has an uncertain outcome; clients retry with the same logical JobID.
2. **Durable consensus identity.** Term, vote and log/snapshot state reach the persistent store before a positive vote/replication acknowledgement. Persistence errors stop positive progress. Proposals are identified by index and term, so a replacement entry cannot satisfy an old waiter.
3. **One ordered mutation path.** Only applied commands change replicated job state. The leader records job payload, timestamps, worker identity, attempt, deadline and retry policy as needed. Followers do not create independent transitions from their local clocks.
4. **Dispatch ownership.** DISPATCH chooses a pending FIFO head and increments its attempt. ACK/TIMEOUT must match the active assignment. A rejected stale transition does not change the next assignment. A retried ACK for an already completed matching assignment succeeds idempotently.
5. **Recovery equivalence.** Snapshot plus committed suffix reconstructs FIFO, active/in-flight state, DLQ, completion deduplication and last-applied boundary. An installed snapshot cannot be acknowledged without persistence. Startup restores a snapshot before applying its committed suffix.
6. **Client responsibility.** JobIDs identify logical operations and must remain stable across retries. Reusing an ID with a different payload does not replace the old job. Worker effects are at least once; business idempotency must be supplied by the executor/downstream system.
7. **Topology and disk ownership.** Every node receives the same complete static peer list, including itself, and has a distinct directory. Restart uses the same node ID, membership and directory. IDs are reachable peer addresses. One process owns each data directory; automatic enforcement is future work.

Follower responses contain leader hints; clients also rotate seeds. Worker leases are rebuildable local state. Expiry of a lease does not bypass the replicated timeout command for an assigned job.

## Important decisions and deviations

| Decision | Reason and effect | Cost / follow-up |
|---|---|---|
| Custom Raft, fixed membership | Satisfies the requested election/consensus implementation and keeps membership rules auditable | No joint consensus or automatic member replacement; more testing is needed before production use |
| Replicate dispatch and timeout as well as enqueue/ACK | Ownership and retry outcomes must survive leader change | More log entries per job; batch only after preserving the same commit/apply contract |
| Worker registration/heartbeat are soft state | Re-registration rebuilds connectivity; frequent heartbeats do not need durable history | Deviates from planned REGISTER_WORKER command; assignment identity remains replicated |
| Atomic JSON Raft image | One checksum and atomic file/rename boundary couples metadata, retained log and snapshot | O(retained log + snapshot bytes) per mutation; does not satisfy the planned efficient random-access/segmented Raft WAL |
| Preserve full pending/in-flight/DLQ plus completed tombstones | Cursor-only state cannot reconstruct missing jobs after prefix compaction; deleting all completed IDs breaks deduplication | No bounded tombstone policy; snapshot size depends on workload |
| Queue checkpoint rather than full Beaver protocol | Makes the actual recovery invariant explicit and testable | No coordinated external-traffic partial-snapshot protocol, replay fingerprint research or geo-latency study |
| Explicit failures for unimplemented WAL utility operations | Repair/truncate/compare must not silently return success | Administrative repair utilities and the unused batch-writer helper are not complete public features |

The Raft persistence flow is temporary-file write → file fsync → close → rename → directory fsync. Standalone recovery uses its own checksummed WAL and snapshot path; master mode restores its authoritative Raft image. Do not replay both paths into one FSM.

## Verification record

Verification was performed on macOS arm64 with Go 1.25.1; the module declares Go 1.23. Socket-based tests require permission to bind local ports in a restricted execution environment. CI provides repeatable Go-module checks on a runner with loopback networking.

| Check | Result |
|---|---|
| `go test ./... -timeout 120s` | Passed all packages; integration package completed in approximately 50 seconds |
| Focused Raft tests repeated (`-count=5`) and focused Raft race tests (`-race -count=3`) | Passed |
| CLI/worker/controller race checks | Passed before the final whole-repository run |
| `TestRaftClusterDurableFailover` over real TCP/gRPC | Passed; leader loss, replica restart, full restart, snapshot deduplication and minority refusal |
| `scripts/demo-three-node-failover.sh` | Passed; submitted six fixed-ID jobs, SIGKILLed the elected leader, observed ACKs for all six |
| `go test -race ./... -timeout 180s` | Passed all packages; integration package completed in 50.664 seconds |
| `go vet ./...` and `go build -o /private/tmp/raft-recovery-phase3-queue ./cmd/queue` | Passed |
| `git diff --check` and local documentation-link verification | Passed; README, indexes, delivery and interview-guide targets resolve |
| Final canceled-RPC result cleanup: controller/server tests and race tests | Passed; regression covers cancellation before late committed apply and successful result consumption |
| Final `go test -race ./test/integration -run '^TestRaftClusterDurableFailover$' -count=3 -timeout 120s` | Passed all three real-gRPC iterations in 10.174 seconds; vet, binary build, gofmt and diff checks also passed after the cleanup |

Independent code review identified a response-result retention issue after canceled RPCs. Request-owned response IDs now register before proposal; error paths discard ownership, successful RPCs consume results, and late application drops unclaimed output. A focused second review found no lifecycle defect. The full suites above ran before this final cleanup; the final code received targeted controller/server normal and race checks, with cluster-path verification recorded separately.

The first whole-repository race attempt was blocked by sandbox loopback-bind restrictions; the same suite passed with socket permission. macOS linker warnings about LC_DYSYMTAB did not fail compilation or tests. Tests exercise explicit safety examples; this is not an exhaustive model check. The demo and elapsed test durations are not benchmarks of recovery latency or throughput. The original <3 s failover, <100 ms replication lag, 30–50% recovery improvement and ≥40% snapshot reduction remain unvalidated targets.

## Remaining functionality, in priority order

| Priority | Missing capability | Why it matters | Suggested acceptance criteria |
|---|---|---|---|
| P1 | Bounded queue, historical job records and tombstones | History and memory/disk can grow without bound | Defined capacity/backpressure and deduplication window; expire records without breaking retries inside that window |
| P1 | Data-directory lock and persisted cluster identity | Misconfiguration can reuse a directory or change membership unsafely | Second process rejected; node/cluster/membership mismatch fails startup with a clear error |
| P1 | Live leader/term/lag/commit/apply metrics and read API | Existing metrics and offline status do not describe the live Raft cluster completely | Observable failover and commit/apply lag; document read consistency; ReadIndex/barrier for linearizable reads |
| P1 before network deployment | TLS, authentication, authorization and backup/restore procedure | Demo currently assumes a trusted network and preserved quorum disks | Unauthorized peer/client rejected; certificate rotation and restore drill; no accidental split cluster |
| P2 | Segmented Raft WAL, group commit, indexed storage and snapshot thresholds | Full-image persistence amplifies writes; snapshots are interval-driven rather than the planned log-byte threshold | Crash injection across append/rotation/compaction; bounded write amplification and measured p95 latency |
| P2 | Reproducible benchmarks and dashboard | Original KPI targets have no comparable baseline | Record hardware/runtime, payload/active ratios, seeds, repetitions, p50/p95/p99; compare complete versus compact checkpoints under the same recovery contract |
| P2 | DLQ inspect/requeue and richer administration | Dead letters are preserved but operational remediation is limited | Auditable leader-gated requeue command with explicit retry/ID semantics; no direct local FSM mutation |
| P2 | Disk fault injection, long-running chaos and model checking | Current tests cover representative failures rather than all schedules | ENOSPC/fsync/rename failure tests, concurrent poll/ACK/failover soak, state-machine/consensus invariant checking |
| P3 | Dynamic membership, pre-vote/check-quorum, optimized snapshot transfer | Improves operation and availability beyond a fixed local cluster | Safe joint-consensus transitions, learner catch-up, cancellation/backpressure and chunked snapshot transport |
| Research extension | Beaver reproduction and geo-latency study | Required before claiming the original research milestone complete | Define external traffic assumptions, consistent partial cut/replay proof, comparable baselines and repeatable experiments |

Do not treat performance targets, research extensions or production hardening as completed because the Phase 3 functional core works. The [interview guide](INTERVIEW_GUIDE.md) provides a precise pitch and explains the tradeoffs without those claims.

## References and reading order

1. [README](../README.md): build, cluster commands and failure demonstration.
2. [Interview guide](INTERVIEW_GUIDE.md): architecture, request lifecycle, safety rules and interview questions.
3. `internal/raft/raft.go`, `rpc.go`, `persistent.go`; `internal/jobmanager/replicated.go`; `internal/controller/controller.go`.
4. [Original Phase 3 plan](planning/PHASE3_IMPLEMENTATION_PLAN.md), [phase specification](phases/phase3-beaver-raft.md), [roadmap](../roadmap.md).
5. [Raft paper](https://raft.github.io/raft.pdf) and [Beaver, OSDI 2024](https://www.usenix.org/conference/osdi24/presentation/yu).
