# Beaver-Raft Phase 2 (Distributed Queue) Status Report

## 1. Project Overview
We are implementing **Phase 2: FalconQueue**, which transforms the single-node job queue into a **distributed Master-Worker architecture**.
- **Master**: Runs the `Controller`, manages `JobManager` (state), WAL, and Snapshots. Exposes a gRPC `FalconQueueService`.
- **Worker**: Runs `WorkerPool` with a `GrpcJobSource` to pull jobs from Master via gRPC.

## 2. Current Implementation Status

### 2.1 Completed Components
- **Proto Definition**: `api/proto/v1/service.proto` defines `SubmitJob`, `PollJobs`, `AcknowledgeJob`, `Heartbeat`.
- **Master Server**: `internal/server` implements the gRPC server, forwarding requests to `Controller`.
- **Worker Client**: `internal/worker/grpc_source.go` implements `JobSource` interface, connecting to Master.
- **Worker Pool**: Refactored to support `JobSource` (Pull mode) alongside the original Push mode.
- **CLI**: Updated to support `--mode master` and `--mode worker`.
- **Controller**: Implements `JobSource` interface (`Poll`, `Acknowledge`) to serve as the backend logic.

### 2.2 Integration Test (`test/demo-distributed.sh`)
We have a shell script that:
1. Starts a Master node (background).
2. Starts a Worker node (background).
3. Submits 5 jobs via `beaver-raft enqueue --master ...`.
4. Waits 5 seconds.
5. Dumps logs and WAL content.

## 3. The Issue: "Missing ACKs and Mysterious Dispatching"

### 3.1 Symptoms
- **Job Submission**: Successful. 5 jobs enqueued.
- **WAL Analysis**:
  - `ENQUEUE` events: 5 (Correct).
  - `DISPATCH` events: 5 (Correct). All jobs transition to `In-Flight`.
  - `ACK` events: **Only 0 or 1** (Incorrect). Most jobs remain `In-Flight` until timeout.
- **Worker Log**:
  - Shows `Poller Loop Started`.
  - **Rarely** shows `Polled jobs count: 1` or `Worker received task`.
  - Often shows nothing between "Started" and "Stopping", despite Master WAL showing Dispatches.

### 3.2 Investigation Findings
1. **Zombie Processes**: We suspected zombie worker processes were stealing jobs. Added `pkill -f beaver-raft` to the script.
   - **Result**: `DISPATCH` events in WAL are still spaced very closely (~10ms), even when we increased Worker poll interval to **1000ms**.
   - **Implication**: There is still a high-frequency poller active.

2. **Master's Self-Polling?**:
   - The `Controller` starts 4 internal `dispatchLoop` goroutines by default (`controller.go:Start()`).
   - These loops fetch pending jobs and try to `pool.Submit()` them to the **local** worker pool.
   - In Master mode, `config.Worker.WorkerCount` is set to 0.
   - **Hypothesis**: The Master's *internal* dispatch loops are dequeuing the jobs (writing `DISPATCH` to WAL) but failing to execute them because the local worker pool has 0 workers (or they are stuck).
   - **Evidence**:
     - WAL `DISPATCH` events have `job_id` but the checksums match.
     - The timestamps are very close (10ms), matching the `5 * time.Millisecond` sleep in `dispatchLoop`.
     - The gRPC Worker pulls nothing because the Master's internal loop steals the jobs first!

## 4. Root Cause Hypothesis
The **Master Node** is running in a mixed mode where it shouldn't:
1. It starts the gRPC Server (correct).
2. **BUT** it also starts the default `dispatchLoop`s in `Controller.Start()` (incorrect for a dedicated Master).
3. These internal loops race with the gRPC `Poll` requests.
4. Since internal loops are faster (no network), they "win" and mark jobs as `In-Flight`.
5. However, since `WorkerCount` is 0 on Master, these jobs sit in the local `taskCh` (or get stuck trying to submit if channel is unbuffered/full) and are never executed.
6. The remote Worker gets nothing (`Poll` returns empty).

## 5. Recommended Fix
Modify `internal/controller/controller.go` to **disable internal dispatch loops** when running in "Master/Distributed" mode, or purely rely on configuration:
- If `WorkerCount` is 0, `dispatchLoop` should probably not run, or `Controller` needs a "Passive Mode" flag where it only reacts to `Poll` requests and doesn't actively dispatch.

## 6. Artifacts for Next AI
- `internal/controller/controller.go`: Check `Start()` and `dispatchLoop`.
- `test/demo-distributed.sh`: The reproduction script.
- `data/master/wal`: Evidence of Dispatches without Acks.
