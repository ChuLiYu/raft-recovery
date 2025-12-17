#!/bin/bash
set -e

# Cleanup function
cleanup() {
    echo "Stopping processes..."
    kill $MASTER_PID 2>/dev/null || true
    kill $WORKER_PID 2>/dev/null || true
    rm -rf data/master data/worker
}
trap cleanup EXIT

# Setup
echo "Setting up..."
rm -rf data/master data/worker
mkdir -p data/master data/worker

# Create config for master
cat > data/master/config.yaml <<EOF
worker:
  worker_count: 0  # Master doesn't run local workers (optional)
  task_timeout: 5s
wal:
  dir: "data/master/wal"
  buffer_size: 100
  flush_interval_ms: 10
snapshot:
  dir: "data/master/snapshot"
  interval_seconds: 10
  retention_count: 3
metrics:
  enabled: true
  port: 9091
EOF

# Create config for worker
cat > data/worker/config.yaml <<EOF
worker:
  worker_count: 4
  task_timeout: 5s
wal:
  dir: "data/worker/wal" # Workers might not need WAL if stateless, but config requires it
  buffer_size: 100
  flush_interval_ms: 10
snapshot:
  dir: "data/worker/snapshot"
  interval_seconds: 10
  retention_count: 3
metrics:
  enabled: true
  port: 9092
EOF

# Create jobs file
cat > data/jobs.json <<EOF
[
  {"id": "job-1", "payload": {"sleep_ms": 100}, "timeout_ms": 5000},
  {"id": "job-2", "payload": {"sleep_ms": 100}, "timeout_ms": 5000},
  {"id": "job-3", "payload": {"sleep_ms": 100}, "timeout_ms": 5000},
  {"id": "job-4", "payload": {"sleep_ms": 100}, "timeout_ms": 5000},
  {"id": "job-5", "payload": {"sleep_ms": 100}, "timeout_ms": 5000}
]
EOF

# Start Master
echo "Starting Master..."
./bin/beaver-raft run --mode master --port 50051 --config data/master/config.yaml > data/master.log 2>&1 &
MASTER_PID=$!
sleep 2 # Wait for startup

# Start Worker
echo "Starting Worker..."
./bin/beaver-raft run --mode worker --master localhost:50051 --config data/worker/config.yaml > data/worker.log 2>&1 &
WORKER_PID=$!
sleep 2

# Submit Jobs
echo "Submitting jobs..."
./bin/beaver-raft enqueue --file data/jobs.json --master localhost:50051

# Wait for processing
echo "Waiting for processing..."
sleep 5

# Check Status (query Master)
echo "Checking Master Status..."
./bin/beaver-raft status --config data/master/config.yaml

echo "Done."

