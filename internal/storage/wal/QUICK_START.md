# WAL Quick Start

English | [Chinese version](QUICK_START.zh-CN.md)

This page’s full content is currently available in Chinese. An English translation is in progress.

Highlights covered in the Chinese version:

- A 5-day incremental plan to implement the WAL module
- Checksum calculation/verification, append, replay, rotate, and lifecycle
- Tests for correctness, recovery, and concurrency (with race detector)
- Integration with Controller and Snapshot-based recovery
- Optional optimizations like batch writer and validation tools

Suggested order of work:

1. Implement types, checksum, and basic errors; then New/Append in WAL.
2. Add Replay with checksum-before-decode verification and tests.
3. Implement Rotate, Close, GetLastSeq; verify with tests.
4. Add concurrency tests; integrate with Controller recovery.
5. Optionally add a batch writer and utility functions; run benchmarks.
