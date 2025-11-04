# AI Notes

English | [Chinese version](ai-notes.zh-CN.md)

This page’s full content is currently available in Chinese. An English translation is in progress.

Purpose: capture AI-assisted design decisions, scaffolding, and implementation tips for Phase 1 (Snapshot-Aware Job Queue).

Quick outline (see Chinese version for details):

- Goals: single-node queue, WAL + snapshot persistence, crash recovery < 3s, ≥ 200 jobs/s, race-free.
- Architecture: CLI → Controller → JobManager, WAL, SnapshotManager, WorkerPool.
- Recovery: load snapshot, then replay WAL; checksum verified before decode.
- Testing: unit + integration + race; throughput benchmark and recovery time check.
- Operations: regular snapshots with atomic write (temp + rename), optional batching in WAL.

If you prefer, open the Chinese version linked above for the full step-by-step guide and examples.
