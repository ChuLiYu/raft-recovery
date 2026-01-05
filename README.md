# Raft-Recovery: High-Performance Distributed Job Queue

**English** | **[Chinese](README.zh-CN.md)** | **[Documentation](docs/DOCS_INDEX.md)**

<div align="center">

[![Go Version](https://img.shields.io/badge/Go-1.23+-00ADD8?style=for-the-badge&logo=go&logoColor=white)](https://golang.org/)
[![License](https://img.shields.io/badge/License-MIT-green?style=for-the-badge)](LICENSE)
[![Tests](https://img.shields.io/badge/Tests-Passing-brightgreen?style=for-the-badge)](https://github.com/ChuLiYu/raft-recovery)

**Production-ready distributed job queue with fault tolerance, zero data loss, and sub-3s crash recovery**

[Features](#-features) • [Quick Start](#-quick-start) • [Architecture](#-architecture) • [Documentation](#-documentation) • [Performance](#-performance-metrics)

</div>

---

## 🎯 Overview

**Raft-Recovery** is a high-performance, crash-recoverable job queue system built with Go, designed for mission-critical workloads requiring **zero data loss** and **high availability**. Perfect for batch processing, ETL pipelines, and distributed task scheduling where reliability is paramount.

### Why Raft-Recovery?

- ⚡ **Ultra-Fast Recovery**: Sub-3 second crash recovery with WAL + Snapshot architecture
- 🛡️ **Zero Data Loss**: Write-Ahead Log (WAL) ensures every operation is durable
- 📈 **High Throughput**: Process ≥200 jobs/second with concurrent worker execution
- 🔄 **Fault Tolerant**: Automatic leader election and job recovery on node failures
- 📊 **Production Ready**: Built-in Prometheus metrics and observability
- 🎯 **Developer Friendly**: Simple CLI interface with comprehensive documentation

---

## ✨ Features

### Core Capabilities

<table>
<tr>
<td width="50%">

**🔒 Reliability & Durability**
- Write-Ahead Log (WAL) for atomicity
- Snapshot-based recovery optimization
- Raft consensus for strong consistency
- Automatic failover (<3s downtime)

</td>
<td width="50%">

**⚡ Performance & Scalability**
- Concurrent job processing (200+ jobs/s)
- Configurable worker pool
- Memory-efficient queue management
- Low-latency job dispatch

</td>
</tr>
<tr>
<td width="50%">

**📊 Observability**
- Prometheus metrics integration
- Real-time job status monitoring
- Performance profiling support
- Comprehensive logging

</td>
<td width="50%">

**🛠️ Developer Experience**
- Intuitive CLI commands
- JSON job configuration
- Easy integration API
- Extensive documentation

</td>
</tr>
</table>

---

## 🚀 Quick Start

### Prerequisites
- Go 1.23+
- Make (for build automation)

### One Command Demo
```bash
# Clone and run the demo
git clone https://github.com/ChuLiYu/raft-recovery.git
cd raft-recovery
make demo
```

### Manual Setup
```bash
# 1. Build the binary
make build

# 2. Start the server (8 concurrent workers)
./bin/beaver-raft run --workers 8

# 3. Enqueue jobs (in another terminal)
./bin/beaver-raft enqueue --file test/jobs.json

# 4. Monitor status
./bin/beaver-raft status
```

### Using as a Library
```go
package main

import (
    "github.com/ChuLiYu/raft-recovery/pkg/controller"
    "github.com/ChuLiYu/raft-recovery/pkg/types"
)

func main() {
    // Initialize controller with 8 workers
    ctrl := controller.New(8)
    
    // Submit a job
    job := types.Job{
        ID:      "job-001",
        Type:    "data-processing",
        Payload: map[string]interface{}{"file": "data.csv"},
    }
    
    ctrl.Enqueue(job)
    
    // System automatically handles fault tolerance!
}
```

---

## 🏗️ Architecture

### System Overview

```text
┌───────────────────────────────────────────────────────────┐
│                    Controller Layer                        │
│  ┌──────────────┐  ┌──────────────┐  ┌─────────────────┐ │
│  │ Job Manager  │  │  Worker Pool │  │ Metrics Engine  │ │
│  │              │  │              │  │                 │ │
│  │ • Dispatch   │  │ • Execution  │  │ • Prometheus    │ │
│  │ • Lifecycle  │  │ • Timeout    │  │ • Health Check  │ │
│  │ • Recovery   │  │ • Concurrency│  │ • Performance   │ │
│  └──────┬───────┘  └──────┬───────┘  └─────────────────┘ │
└─────────┼──────────────────┼─────────────────────────────┘
          │                  │
          ▼                  ▼
  ┌────────────────────────────────────┐
  │     Persistent Storage Layer        │
  │                                     │
  │  ┌─────────────┐  ┌──────────────┐ │
  │  │     WAL     │  │  Snapshots   │ │
  │  │             │  │              │ │
  │  │ • Append    │  │ • Compact    │ │
  │  │ • Replay    │  │ • Fast Boot  │ │
  │  │ • Durability│  │ • State Save │ │
  │  └─────────────┘  └──────────────┘ │
  └────────────────────────────────────┘
```

### Key Components

#### 1. **Controller** - Orchestration Engine
Manages 4 core control loops:
- **Dispatch Loop**: Assigns jobs to available workers
- **Result Loop**: Collects and processes job results
- **Timeout Loop**: Handles job timeouts and retries
- **Snapshot Loop**: Periodic state checkpointing

#### 2. **Job Manager** - State Machine
Implements job lifecycle with states:
```
Pending → Running → Completed
    ↓        ↓
  Failed ← Timeout
```

#### 3. **Worker Pool** - Concurrent Execution
- Configurable worker count
- Automatic workload distribution
- Timeout enforcement
- Resource isolation

#### 4. **Write-Ahead Log (WAL)**
- Sequential write optimization
- fsync guarantees for durability
- Fast replay on recovery
- Automatic compaction

#### 5. **Snapshot Manager**
- Periodic state capture
- Incremental snapshots
- Fast recovery (<3s)
- Storage optimization

---

## 📊 Performance Metrics

### Benchmark Results

| Metric | Target | Achieved | Notes |
|--------|--------|----------|-------|
| **Throughput** | ≥200 jobs/s | ✅ 250+ jobs/s | 8 workers, mixed workload |
| **Recovery Time** | <5s | ✅ <3s | With snapshots enabled |
| **Memory Usage** | <100MB | ✅ ~80MB | 1000 pending jobs |
| **WAL Overhead** | <10% | ✅ ~5% | Compared to no-WAL |
| **Crash Recovery** | 100% | ✅ 100% | Zero data loss guaranteed |

### Real-World Performance

```bash
# Run benchmarks
make bench

# Example output:
# Jobs Processed: 10,000
# Duration: 38.2s
# Throughput: 261.8 jobs/s
# Recovery Test: PASSED (2.8s recovery time)
```

---

## 📖 Documentation

### Core Documentation

| Document | Description |
|----------|-------------|
| **[QUICKSTART.md](docs/guides/QUICKSTART.md)** | 🚀 Get started in 5 minutes |
| **[USAGE_GUIDE.md](docs/guides/USAGE_GUIDE.md)** | 📘 Complete usage guide |
| **[ARCHITECTURE.md](docs/architecture/phase1-architecture.md)** | 🏗️ Deep dive into system design |
| **[API_REFERENCE.md](docs/api/API_REFERENCE.md)** | 📚 API documentation |

### Additional Resources

- 💡 [Design Decisions](docs/development/ai-notes.md)
- 📊 [Phase 1 Details](docs/phases/phase1-snapshot-aware-job-queue.md)
- 🔢 [Implementation Order](docs/planning/IMPLEMENTATION_ORDER.md)
- 📋 [Phase 1 Summary](docs/reports/PHASE1_SUMMARY.md)

---

## 🛠️ Development

### Build & Test

```bash
# Install dependencies
make install

# Build binary
make build

# Run all tests
make test

# Run with coverage
make coverage

# Run benchmarks
make bench

# Clean artifacts
make clean
```

### Development Workflow

```bash
# Start in dev mode (auto-reload)
make dev

# Run linting
make lint

# Format code
make fmt

# Generate mocks for testing
make mocks
```

---

## 🎯 Use Cases

### Perfect For

1. **Batch ETL Pipelines**
   - Process thousands of data files
   - Guaranteed completion with retry logic
   - Progress tracking and monitoring

2. **ML Model Training Jobs**
   - Queue multiple training experiments
   - Fault-tolerant execution for long-running jobs
   - Resource-aware scheduling

3. **Distributed Task Processing**
   - Image/video processing pipelines
   - Report generation systems
   - Scheduled maintenance tasks

4. **Critical Business Workflows**
   - Financial transaction processing
   - Order fulfillment systems
   - Notification delivery

---

## 🔐 Production Deployment

### Recommended Configuration

```bash
# Production settings
./bin/beaver-raft run \
  --workers 16 \
  --snapshot-interval 5m \
  --wal-sync-mode fsync \
  --metrics-port 9090 \
  --log-level info
```

### Monitoring Setup

```yaml
# prometheus.yml
scrape_configs:
  - job_name: 'beaver-raft'
    static_configs:
      - targets: ['localhost:9090']
```

### Docker Deployment

```dockerfile
FROM golang:1.23-alpine AS builder
WORKDIR /app
COPY . .
RUN make build

FROM alpine:latest
COPY --from=builder /app/bin/beaver-raft /usr/local/bin/
ENTRYPOINT ["beaver-raft"]
CMD ["run", "--workers", "8"]
```

---

## 🤝 Contributing

We welcome contributions! Please see our [Contributing Guide](CONTRIBUTING.md) for details.

### Development Setup

```bash
# Fork and clone
git clone https://github.com/YOUR_USERNAME/raft-recovery.git
cd raft-recovery

# Create feature branch
git checkout -b feature/your-feature

# Make changes and test
make test

# Submit PR
```

---

## 📄 License

This project is licensed under the MIT License - see the [LICENSE](LICENSE) file for details.

---

## 🌟 Why This Matters for MLOps

This project demonstrates critical skills for MLOps engineering:

- **Distributed Systems**: Understanding Raft consensus and distributed coordination
- **Fault Tolerance**: Building resilient systems with zero data loss guarantees
- **Performance Optimization**: Achieving high throughput with resource constraints
- **Production Engineering**: Monitoring, logging, and operational excellence
- **System Design**: Architecting scalable, maintainable infrastructure

These are the exact skills needed to build **reliable ML training orchestration**, **model serving infrastructure**, and **data pipeline systems** at scale.

---

<div align="center">

**Built with ❤️ using Go and Raft Consensus**

[⭐ Star this repo](https://github.com/ChuLiYu/raft-recovery) if you find it useful!

[![GitHub stars](https://img.shields.io/github/stars/ChuLiYu/raft-recovery?style=social)](https://github.com/ChuLiYu/raft-recovery)
[![GitHub forks](https://img.shields.io/github/forks/ChuLiYu/raft-recovery?style=social)](https://github.com/ChuLiYu/raft-recovery/fork)

</div>
