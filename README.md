# Beaver-Raft: Crash-Recoverable Job Queue System

**English** | **[中文](README.zh-CN.md)** | **[Language Guide](LANGUAGE.md)**

[![Go Version](https://img.shields.io/badge/Go-1.23-blue.svg)](https://golang.org/)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg)](https://github.com/ChuLiYu/raft-recovery)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

Production-ready, crash-recoverable job queue system with sub-3s recovery time and zero data loss.

> 📚 **[完整文檔導覽](DOCS_INDEX.md)** | 快速找到您需要的文檔

## ✨ Features

- ⚡ **Fast Recovery**: Sub-3s crash recovery with WAL + Snapshot
- 📊 **High Performance**: ≥200 jobs/s throughput
- 🔄 **Zero Data Loss**: Write-Ahead Log ensures durability
- 📈 **Observable**: Prometheus metrics and real-time monitoring
- 🎯 **Simple**: Easy-to-use CLI interface

## 🚀 Quick Start

```bash
# One command to see it in action
make demo

# Or start manually
make build
./bin/beaver-raft run --workers 8

# In another terminal
./bin/beaver-raft enqueue --file test/jobs.json
```

## 📖 Documentation

| Document | Description |
|----------|-------------|
| **[USAGE_GUIDE.md](USAGE_GUIDE.md)** | 🎯 快速使用指南（中文） |
| **[QUICKSTART.md](QUICKSTART.md)** | 📘 開發者入門（中文） |
| **[PHASE1_SUMMARY.md](PHASE1_SUMMARY.md)** | 📋 Phase 1 完整總結 |
| **[IMPLEMENTATION_ORDER.md](IMPLEMENTATION_ORDER.md)** | 🔢 模塊實作順序 |

### Architecture Docs

- 🏗️ [Phase 1 Architecture](docs/phase1-architecture.md) - System design
- 💡 [AI Notes](docs/ai-notes.md) - Design decisions
- 📊 [Phase 1 Details](docs/phase1-snapshot-aware-job-queue.md) - Technical deep dive

## 🏗️ Architecture

```text
┌─────────────────────────────────────────┐
│            Controller                    │
│  ┌──────────┐  ┌──────────┐  ┌────────┐│
│  │JobManager│  │Worker Pool│  │Metrics ││
│  └────┬─────┘  └─────┬────┘  └────────┘│
└───────┼──────────────┼─────────────────┘
        │              │
        ▼              ▼
  ┌──────────────────────────┐
  │    WAL + Snapshot         │
  │  (Persistent Storage)     │
  └──────────────────────────┘
```

### Core Components

- **Controller**: Orchestrates 4 core loops (dispatch, result, timeout, snapshot)
- **JobManager**: State machine managing job lifecycle
- **Worker Pool**: Concurrent job execution with timeout control
- **WAL**: Write-Ahead Log for operation durability
- **Snapshot Manager**: Periodic state snapshots for fast recovery

## ��️ Development

```bash
# Install dependencies
make install

# Build
make build

# Run tests
make test

# Run benchmarks
make bench

# Generate coverage report
make coverage

# Clean build artifacts
make clean
```

## 📊 Performance Metrics

| Metric | Target | Status |
|--------|--------|--------|
| Recovery Time | < 3s | ✅ |
| Throughput | ≥ 200 jobs/s | ✅ |
| Data Loss | Zero | ✅ (WAL) |
| Concurrency | Race-free | ✅ (tested) |

## 🎯 Use Cases

- Background job processing
- Task queue with crash recovery
- Distributed job scheduling (Phase 2+)
- Mission-critical task execution

## 🗺️ Roadmap

### Phase 1: Snapshot-Aware Job Queue ✅ (Current)

- Goroutine-based workers
- WAL + JSON snapshots
- Fast crash recovery
- Prometheus metrics

### Phase 2: FalconQueue (Planned)

- Multi-node deployment
- HTTP RPC communication
- Service registry & heartbeat
- Observability stack

### Phase 3: Beaver-Raft (Future)

- Raft consensus integration
- Distributed coordination
- Partial snapshots optimization
- Research-grade architecture

## 📝 Example Usage

### Create Jobs

```json
[
  {
    "id": "task-001",
    "payload": {"action": "process", "value": 42},
    "timeout_ms": 5000
  }
]
```

### Submit & Monitor

```bash
# Start server
./bin/beaver-raft run --workers 8

# Enqueue jobs
./bin/beaver-raft enqueue --file jobs.json

# Check status
./bin/beaver-raft status

# View metrics
curl http://localhost:9090/metrics
```

### Test Crash Recovery

```bash
# 1. Start server
./bin/beaver-raft run &
PID=$!

# 2. Submit jobs
./bin/beaver-raft enqueue --file test/jobs.json

# 3. Simulate crash
kill -9 $PID

# 4. Restart - it will recover automatically
./bin/beaver-raft run

# ✅ Unfinished jobs continue processing
```

## 🧪 Testing

```bash
# Unit tests
go test ./internal/...

# Integration tests
go test ./test/integration/...

# Race detection
go test -race ./...

# Specific module
go test -v ./internal/controller/
```

## 📂 Project Structure

```text
beaver-raft/
├── cmd/queue/          # CLI entry point
├── internal/
│   ├── controller/     # Core orchestration
│   ├── jobmanager/     # State management
│   ├── worker/         # Job execution
│   ├── storage/
│   │   ├── wal/       # Write-Ahead Log
│   │   └── snapshot/  # Snapshot management
│   ├── cli/           # Command-line interface
│   └── metrics/       # Prometheus metrics
├── test/
│   └── integration/   # Integration tests
├── docs/              # Documentation
└── scripts/           # Helper scripts
```

## 🤝 Contributing

1. Fork the repository
2. Create your feature branch
3. Add tests for your changes
4. Ensure `make test` passes
5. Submit a pull request

## 📄 License

MIT License - see [LICENSE](LICENSE) file

## 🙏 Acknowledgments

Inspired by distributed systems research and production queue systems:

- Raft consensus algorithm
- Redis queue patterns
- Kafka log design
- PostgreSQL WAL architecture

---

Built with ❤️ for reliable distributed systems

**Quick Links**: [使用指南](USAGE_GUIDE.md) | [開發指南](QUICKSTART.md) | [完整文檔](DOCS_INDEX.md)
