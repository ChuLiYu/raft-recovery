# Phase 3: Beaver-Raft Implementation Plan (Consensus & Partial Snapshots)

## 0. System Architecture & Terminology (Unified Branding)
The system is unified under the name **raft-recovery**, composed of three distinct layers:
1.  **Falcon Layer (Transport & Execution)**: Handles gRPC communication, Worker management, and job dispatch. (Formerly Phase 2)
2.  **Beaver Layer (Storage & Consensus)**: Manages Raft consensus, WAL persistence, and Partial Snapshots. (Formerly Phase 3)
3.  **Core Layer (State Machine)**: Orchestrates the `JobManager` state machine and Controller logic. (Formerly Phase 1)

## 1. Overview
This document outlines the implementation steps for **Phase 3 (Beaver-Raft)**.
The goal is to upgrade the Phase 2 "Master-Worker" system into a **Leader-Follower** cluster using the **Raft Consensus Algorithm**.
This phase also introduces the core research concept: **Partial Snapshots**, allowing faster recovery by persisting only critical "hot" state (In-Flight jobs, Cursors) while relying on log replay for "cold" data.

## 2. Architecture

### 2.1 Cluster Topology

```mermaid
graph TD
    Client[Client] -->|SubmitJob| Leader
    
    subgraph "Raft Cluster"
        Leader[Node A (Leader)]
        Follower1[Node B (Follower)]
        Follower2[Node C (Follower)]
    end
    
    Leader <-->|AppendEntries / RequestVote| Follower1
    Leader <-->|AppendEntries / RequestVote| Follower2
    
    subgraph "Internal Node Architecture"
        Raft[Raft Module]
        FSM[JobManager (FSM)]
        WAL[Raft Log (WAL)]
        Snap[Partial Snapshotter]
    end
    
    Raft -->|Apply Committed Entries| FSM
    Raft -->|Persist Entries| WAL
    FSM -->|Save State| Snap
```

### 2.2 Key Components

1.  **Raft Module**: Handles Leader Election, Log Replication, and Safety (Terms, Votes).
2.  **Raft Log (WAL)**: The existing WAL from Phase 1/2 will be adapted to store Raft Log Entries (`Term`, `Index`, `Command`) instead of raw job events.
3.  **FSM Adapter**: The `JobManager` acts as the Finite State Machine. It applies commands (`Enqueue`, `Ack`) only when the Raft module signals they are **committed**.
4.  **Partial Snapshotter**: A specialized snapshot mechanism that persists only:
    *   **In-Flight Map**: Active jobs.
    *   **Queue Cursors**: Pointers to the current position in the log.
    *   **Fingerprints**: Hashes for integrity verification.

## 3. Implementation Steps

### Step 1: Raft RPC Definitions & Transport
**Goal**: Extend gRPC to support Raft consensus primitives.
- [ ] Update `api/proto/v1/service.proto`:
  - Add `RequestVoteRequest` / `RequestVoteResponse`
  - Add `AppendEntriesRequest` / `AppendEntriesResponse` (Heartbeats & Data)
- [ ] Regenerate Go code (`make proto`).
- [ ] Implement `RaftServiceServer` (can be part of the same gRPC server or separate).

### Step 2: Raft Core (Leader Election)
**Goal**: Enable nodes to elect a leader.
- [ ] Create `internal/raft` package.
- [ ] Define `RaftNode` struct with state: `CurrentTerm`, `VotedFor`, `Role` (Follower, Candidate, Leader).
- [ ] Implement **Election Timer** (randomized timeout).
- [ ] Implement **RequestVote Handler**:
  - Check Term validity.
  - Grant vote if not voted yet in this term.
- [ ] Implement **Leader Heartbeat**:
  - Send empty `AppendEntries` to maintain authority.

### Step 3: Log Replication
**Goal**: Replicate commands from Leader to Followers.
- [ ] Adapt `internal/storage/wal` to support random access by Index (or efficient seeking).
- [ ] Implement `AppendEntries Handler`:
  - Consistency check (`PrevLogIndex`, `PrevLogTerm`).
  - Truncate conflicting entries.
  - Append new entries.
  - Update `CommitIndex`.
- [ ] Update `SubmitJob` flow:
  - Client -> Leader -> Append to Local Log -> Broadcast `AppendEntries` -> Wait for Quorum -> Apply to FSM -> Respond to Client.

### Step 4: State Machine Integration (JobManager)
**Goal**: Connect Raft Commit Index to JobManager execution.
- [ ] Create `ApplyChannel`: Raft sends committed entries here.
- [ ] Refactor `JobManager`:
  - Remove direct WAL writing (Raft handles logging now).
  - Add `Apply(command)` method.
  - Commands: `ENQUEUE`, `ACK_JOB`, `REGISTER_WORKER`.
- [ ] Ensure **Idempotency**: Applying the same command twice (e.g., during replay) must be safe.

### Step 5: Partial Snapshots (The "Beaver" Logic)
**Goal**: Implement the research-grade recovery mechanism.
- [ ] Define `PartialSnapshot` struct:
  - `Term`, `Index` (Raft metadata).
  - `InFlightJobs` (List).
  - `QueueCursors` (Map).
- [ ] Implement `Snapshot()` trigger:
  - When Log size > Threshold (e.g., 10MB).
  - Save `PartialSnapshot` to disk.
  - **Compaction**: Discard log entries covered by the snapshot (up to `Index`).
- [ ] Implement `Restore()`:
  - Load `PartialSnapshot`.
  - Reconstruct `JobManager` state.
  - Continue replaying from WAL starting at `Snapshot.Index + 1`.

### Step 6: Cluster Testing
**Goal**: Verify Fault Tolerance.
- [ ] **Leader Failure**: Kill Leader -> Verify new Leader elected < 3s.
- [ ] **Partition**: Isolate Leader -> Verify Split Vote or new Term.
- [ ] **Data Consistency**: Submit jobs, kill random nodes, restart, verify all jobs exist.

## 4. Migration from Phase 2
- Phase 2 "Master" becomes the first "Leader".
- Phase 2 "Workers" can run alongside Raft Nodes (Colocated) or be separate.
- **Recommendation**: For simplicity, make every Raft Node also a Worker Dispatcher (Leader) or purely a storage/consensus node, keeping Workers as lightweight clients (as in Phase 2).
  - **Architecture Decision**: The Raft Cluster replaces the "Single Master". Workers connect to *any* Raft Node (forwarded to Leader) or discover the Leader.

## 5. Success Metrics (KPIs)
- **Failover Time**: < 3 seconds.
- **Replication Lag**: < 100ms (LAN).
- **Snapshot Size**: 40% smaller than full state dump (due to partial nature).
