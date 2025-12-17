# STAR Report: Debugging Phase 2 "Missing ACKs" in FalconQueue

## Situation (情境)
In **Phase 2 (FalconQueue)**, we transitioned from a single-node job queue to a distributed Master-Worker architecture using gRPC.
- **Goal**: Master dispatches jobs via gRPC; Workers execute them and return ACKs.
- **Problem**: During integration testing (`demo-distributed.sh`), 5 jobs were successfully enqueued and dispatched (according to Master WAL), but **0 or 1 ACKs** were received. The jobs remained stuck in `In-Flight` status until timeout. Workers reported receiving **0 jobs**, despite Master claiming dispatch.

## Task (任務)
Identify the root cause of the "Phantom Dispatch" phenomenon where jobs appeared to be dispatched by the Master but were never received by the gRPC Workers, and implement a fix to ensure reliable distributed execution.

## Action (行動)
1.  **Hypothesis 1: Zombie Processes**: Suspected lingering worker processes were stealing jobs.
    - *Action*: Added `pkill` to the test script and increased worker poll interval to 1s.
    - *Result*: Dispatch interval in WAL remained ~10ms (too fast for remote workers), ruling out network workers. The thief was local.

2.  **Hypothesis 2: Master Self-Cannibalization**: Analyzed `Controller.Start()` and realized it unconditionally starts 4 internal `dispatchLoop` goroutines.
    - *Action*: Added rigorous debug logging to `pollerLoop` (gRPC handler) and `Controller`.
    - *Finding*: The internal `dispatchLoop`s were faster than the gRPC `Poll` request. They dequeued pending jobs from `JobManager`, wrote `DISPATCH` events to WAL, and submitted them to the **Master's local `WorkerPool`**.
    - *Critical Failure*: In Master mode, `WorkerCount` is configured to 0. The local `WorkerPool` had no workers to process the `taskCh`. The jobs were effectively sent to a "black hole" (buffered channel) with no consumers, while remote workers received empty responses because the queue was already empty.

3.  **Fix Implementation**:
    - Introduced `DisableDispatchLoop` flag in `Controller` configuration.
    - Modified `Controller.Start()` to skip starting internal dispatchers when this flag is set.
    - Updated CLI to set `DisableDispatchLoop = true` when running in `--mode master`.

## Result (結果)
- **Immediate Effect**: After the fix, the integration test passed successfully. WAL showed 5 `ENQUEUE` -> 5 `DISPATCH` -> 5 `ACK` events.
- **Verification**: Remote workers logs confirmed they received and executed the jobs.
- **Outcome**: The system now correctly supports distributed execution without the Master node accidentally hoarding tasks.
