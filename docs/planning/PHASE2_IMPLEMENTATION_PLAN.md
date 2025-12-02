# Phase 2: FalconQueue Implementation Plan (gRPC)

## 1. Overview
This document outlines the detailed implementation steps for **Phase 2 (FalconQueue)**. The primary objective is to evolve the existing single-node job queue into a **distributed system** using **gRPC** for inter-node communication.

The system will adopt a **Master-Worker** architecture (also referred to as Registry-Dispatcher-Worker in the high-level roadmap), where:
- **Master Node**: Manages the `JobManager`, persists state (WAL/Snapshot), and exposes a gRPC API.
- **Worker Nodes**: Connect to the Master via gRPC to pull jobs, execute them, and report status.

## 2. Architecture

### 2.1 High-Level Topology

```mermaid
graph TD
    Client[Client / CLI] -->|SubmitJob (gRPC/HTTP)| Master
    
    subgraph "Master Node"
        GRPC_Server[gRPC Server]
        JM[Job Manager]
        WAL[Write-Ahead Log]
        Registry[Worker Registry]
    end
    
    subgraph "Worker Node 1"
        W1_Client[gRPC Client Adapter]
        W1_Pool[Worker Pool]
    end
    
    subgraph "Worker Node 2"
        W2_Client[gRPC Client Adapter]
        W2_Pool[Worker Pool]
    end

    Client -.-> GRPC_Server
    GRPC_Server <--> JM
    JM <--> WAL
    GRPC_Server <--> Registry
    
    W1_Pool <-->|Pull / Ack| W1_Client
    W2_Pool <-->|Pull / Ack| W2_Client
    W1_Client <-->|gRPC| GRPC_Server
    W2_Client <-->|gRPC| GRPC_Server
```

### 2.2 Key Components

1.  **gRPC Service (`FalconQueueService`)**: Defined in `api/proto/v1/service.proto`. Handles job submission, worker registration, heartbeats, and job polling.
2.  **JobSource Abstraction**: A new interface in the `worker` package to decouple the execution logic from the job source. This allows workers to run in "Local Mode" (direct function calls) or "Remote Mode" (gRPC).
3.  **Master Server**: Wraps the existing `JobManager` and exposes it via gRPC. It also handles worker lifecycle (registration/expiry).

## 3. Implementation Steps

### Step 1: Infrastructure & Dependencies
**Goal**: Set up the build environment for Protocol Buffers.
- [ ] Install `protoc` and Go plugins (`protoc-gen-go`, `protoc-gen-go-grpc`) in the development environment.
- [ ] Finalize `api/proto/v1/service.proto`.
- [ ] Run `make proto` to generate Go stubs.
- [ ] Verify generated code compiles.

### Step 2: Core Refactoring (The `JobSource` Interface)
**Goal**: Decouple `worker` package from `jobmanager` implementation details.
- [ ] Define `JobSource` interface in `internal/worker`:
  ```go
  type JobSource interface {
      // Fetch new jobs to work on
      Poll(ctx context.Context, workerID string, count int) ([]*types.Job, error)
      
      // Report job completion or failure
      Acknowledge(ctx context.Context, jobID string, status types.JobStatus, result []byte) error
      
      // Send heartbeat to keep worker registration alive
      Heartbeat(ctx context.Context, nodeID string, load int) error
  }
  ```
- [ ] Implement `LocalJobSource` (adapter for existing `JobManager`) to ensure Phase 1 tests still pass.
- [ ] Update `WorkerPool` to use `JobSource` instead of direct `JobManager` reference.

### Step 3: Server-Side Implementation (The Master)
**Goal**: Implement the gRPC server logic.
- [ ] Create package `internal/server`.
- [ ] Implement `FalconQueueServiceServer` interface.
- [ ] Integrate with existing `JobManager`:
  - `SubmitJob` -> `JobManager.Enqueue`
  - `PollJobs` -> New method in `JobManager` to dequeue pending jobs for specific workers.
  - `AcknowledgeJob` -> `JobManager.UpdateStatus`.
- [ ] Implement `WorkerRegistry` (in-memory map with locks) to track active workers and handle Heartbeats.

### Step 4: Client-Side Implementation (The Worker)
**Goal**: Implement the gRPC client adapter.
- [ ] Create `internal/server/grpc_client.go` (or similar).
- [ ] Implement `GrpcJobSource` which fulfills the `JobSource` interface using generated gRPC clients.
- [ ] Implement retry logic (exponential backoff) for connection failures.

### Step 5: CLI & Entry Point Updates
**Goal**: Allow starting the application in different modes.
- [ ] Update `cmd/queue/main.go` and `internal/cli`.
- [ ] Add flags:
  - `--mode`: `standalone` (default), `master`, `worker`.
  - `--port`: Port for gRPC server (default: 50051).
  - `--master`: Address of master node (required for worker mode).
- [ ] Logic:
  - If `master`: Start `JobManager`, recover from WAL, start `gRPC Server`.
  - If `worker`: Start `WorkerPool`, initialize `GrpcJobSource`, connect to Master.

### Step 6: Verification & Testing
**Goal**: Ensure distributed logic works.
- [ ] **Unit Tests**: Test gRPC server methods with mocked `JobManager`.
- [ ] **Integration Test**:
  - Script to start 1 Master and 2 Workers locally.
  - Submit 100 jobs to Master.
  - Verify all jobs are processed by Workers.
  - Kill a Worker and verify system stability.

## 4. Migration Strategy
To ensure we don't break existing functionality:
1.  The `standalone` mode will remain the default for now.
2.  `JobSource` interface ensures we can switch between local and remote easily.
3.  Existing tests (`make test`) must pass throughout the refactoring.

## 5. Future Considerations (Phase 2.5+)
- **TLS/Auth**: Currently using insecure gRPC for simplicity.
- **Leader Election**: Currently a single Master. Phase 3 (Raft) will address high availability of the Master.
- **Binary Format**: Optimizing payload serialization if JSON is too slow.
