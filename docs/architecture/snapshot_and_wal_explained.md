# Snapshot and WAL: How They Work Together

English | [Chinese version](snapshot_and_wal_explained.zh-CN.md)

This page’s full content is currently available in Chinese. An English translation is in progress.

What you’ll find in the Chinese version:

- Conceptual model of WAL (append-only, replay) and Snapshot (periodic full state)
- Why they’re combined for fast recovery and bounded disk usage
- Recovery flow: load snapshot, then replay WAL beyond LastSeq
- Implementation notes: atomic snapshot write (temp + rename), WAL replay idempotency
- Practical examples with small event sequences and JSON snapshots

Quick outline (for reference):

1. WAL records state transitions in order and is durable and replayable.
2. Snapshots periodically persist full state to cap replay time and WAL growth.
3. On startup: load snapshot → replay WAL entries with seq > LastSeq.
4. After snapshot: prune WAL entries up to LastSeq to control size.
5. Ensure checksums and atomic writes; test crash recovery and throughput.
