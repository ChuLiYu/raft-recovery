# Beaver-Raft Usage Guide

> Quick guide to get started with Beaver-Raft crash-recoverable job queue system

**[中文版](USAGE_GUIDE.zh-CN.md)** | English

## 🚀 Quick Start

### One-Line Demo

```bash
make demo
```

This automatically: Build → Start → Submit jobs → Simulate crash → Auto recovery → Verify

### Manual Start (3 Steps)

```bash
# 1. Build
make build

# 2. Start server (Terminal 1)
./bin/beaver-raft run --workers 8

# 3. Submit jobs (Terminal 2)
./bin/beaver-raft enqueue --file test/jobs.json
```

## 📋 System Requirements

- Go 1.23+
- macOS / Linux
- 8GB+ RAM (recommended)

## 🎯 Core Features

| Feature | Command | Description |
|---------|---------|-------------|
| Start Server | `./bin/beaver-raft run` | Process jobs with 8 concurrent workers |
| Submit Jobs | `./bin/beaver-raft enqueue --file jobs.json` | Batch enqueue jobs |
| Check Status | `./bin/beaver-raft status` | Display system status |
| View Metrics | `curl http://localhost:9090/metrics` | Prometheus monitoring data |

## 📝 Create Job Files

Create `my-jobs.json`:

```json
[
  {
    "id": "task-001",
    "payload": {"action": "process", "data": 42},
    "timeout_ms": 5000
  },
  {
    "id": "task-002",
    "payload": {"action": "notify", "user": "admin"},
    "timeout_ms": 3000
  }
]
```

Submit:

```bash
./bin/beaver-raft enqueue --file my-jobs.json
```

## 🔧 Configuration Options

```bash
./bin/beaver-raft run \
  --workers 8 \                  # Number of workers
  --snapshot-interval 30s \      # Snapshot interval
  --task-timeout 30s \           # Task timeout
  --wal-path ./data/wal \        # WAL path
  --snapshot-path ./data/snapshot  # Snapshot path
```

Or use configuration file `config.yaml`:

```yaml
worker_count: 8
task_timeout: 30s
snapshot_interval: 30s
max_retry: 3
wal_path: ./data/wal
snapshot_path: ./data/snapshot
metrics_port: 9090
```

```bash
./bin/beaver-raft run --config config.yaml
```

## 🧪 Test Crash Recovery

```bash
# 1. Start and get PID
./bin/beaver-raft run &
PID=$!

# 2. Submit jobs
./bin/beaver-raft enqueue --file test/jobs.json

# 3. Wait for processing
sleep 2

# 4. Simulate crash
kill -9 $PID

# 5. Restart and recover
./bin/beaver-raft run

# ✅ System should recover within 3 seconds, unfinished jobs continue
```

## 📊 Monitoring Metrics

Access `http://localhost:9090/metrics` to view:

- `beaver_raft_jobs_enqueued_total` - Total enqueued jobs
- `beaver_raft_jobs_completed_total` - Total completed jobs
- `beaver_raft_jobs_failed_total` - Total failed jobs
- `beaver_raft_recovery_time_seconds` - Recovery time

## 🛠️ Development Commands

```bash
make help       # Show all commands
make build      # Build binary
make test       # Run tests
make bench      # Performance tests
make coverage   # Generate coverage report
make clean      # Clean build artifacts
```

## 🗂️ Data Storage

```text
data/
├── wal/              # Write-Ahead Log
│   └── wal-*.log    # Operation logs
└── snapshot/         # System snapshots
    └── snapshot.json # State snapshot
```

## ⚡ Performance Metrics

- **Recovery Time**: < 3 seconds
- **Throughput**: ≥ 200 jobs/s
- **Data Persistence**: Zero loss (WAL guaranteed)
- **Concurrency Safety**: Verified with race detector

## 🐛 Common Issues

**Q: Port already in use?**

```bash
# Check process using port
lsof -i :9090

# Use different port
./bin/beaver-raft run --metrics-port 9091
```

**Q: Permission denied?**

```bash
chmod +x ./bin/beaver-raft
chmod +x ./scripts/demo.sh
```

**Q: Jobs stuck in pending?**

Check if workers started properly:

```bash
./bin/beaver-raft status
```

## 📚 Advanced Documentation

| Document | Content |
|----------|---------|
| `QUICKSTART.md` | Implementation details & development guide |
| `docs/phase1-architecture.md` | Architecture design & principles |
| `IMPLEMENTATION_ORDER.md` | Module implementation order |
| `PHASE1_SUMMARY.md` | Phase 1 complete summary |

## 🎓 Learning Path

1. **Beginners**: `make demo` → Observe output → Understand flow
2. **Users**: Read this guide → Create custom jobs → Test recovery
3. **Developers**: `QUICKSTART.md` → Read source code → Run tests
4. **Architects**: `docs/phase1-architecture.md` → Understand design decisions

## 🚦 System Architecture (Simplified)

```text
                  ┌─────────────┐
                  │  Controller │
                  └──────┬──────┘
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
   ┌─────────┐    ┌───────────┐    ┌─────────┐
   │JobManager│    │Worker Pool│    │ Metrics │
   └────┬────┘    └─────┬─────┘    └─────────┘
        │               │
        ▼               ▼
   ┌─────────────────────────┐
   │    WAL + Snapshot       │
   └─────────────────────────┘
```

## 🎯 Get Started Now

```bash
# Clone project
git clone https://github.com/ChuLiYu/raft-recovery.git
cd raft-recovery

# Install dependencies
make install

# Run demo
make demo

# 🎉 Start using!
```

## 📞 Need Help?

- Check test cases: `internal/*/*_test.go`
- View complete docs: `docs/` directory
- Read implementation notes: `docs/ai-notes.md`

---

**Beaver-Raft** - Production-grade crash-recoverable job queue system 🦫
