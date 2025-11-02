# WAL Module Overview

English | [Chinese version](MODULE_OVERVIEW.zh-CN.md)

This page’s full content is currently available in Chinese. An English translation is in progress.

What the Chinese version covers:

- Folder layout and required files (types.go, wal.go, checksum.go, errors.go)
- Phased implementation plan with tests (append, replay, rotate, concurrency)
- Public API surface and integration with Controller and Snapshot
- Design decisions (JSON format, CRC32, fsync strategy, rotation semantics)
- Common pitfalls and testing goals

Quick orientation:

1. Start with types/checksum/errors, then implement WAL New/Append/Replay/Rotate.
2. Verify with unit tests, then integrate with the Controller recovery path.
3. Use atomic writes and checksum verification before decode during replay.
4. Optionally enable a batch writer for throughput; keep correctness first.
