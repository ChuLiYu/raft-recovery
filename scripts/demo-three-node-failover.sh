#!/usr/bin/env bash
set -euo pipefail

repo_root="$(cd "$(dirname "$0")/.." && pwd)"
cd "$repo_root"

export GOCACHE="${GOCACHE:-/private/tmp/raft-recovery-gocache}"
export GOMODCACHE="${GOMODCACHE:-/private/tmp/raft-recovery-gomodcache}"
tmp_dir="$(mktemp -d "${TMPDIR:-/tmp}/raft-recovery-demo.XXXXXX")"
ports=(55131 55132 55133)
addresses=("127.0.0.1:${ports[0]}" "127.0.0.1:${ports[1]}" "127.0.0.1:${ports[2]}")
peers="${addresses[*]}"
peers="${peers// /,}"
master_pids=()
worker_pid=""

cleanup() {
  if [[ -n "$worker_pid" ]]; then kill "$worker_pid" 2>/dev/null || true; wait "$worker_pid" 2>/dev/null || true; fi
  for pid in "${master_pids[@]}"; do kill "$pid" 2>/dev/null || true; done
  for pid in "${master_pids[@]}"; do wait "$pid" 2>/dev/null || true; done
  rm -rf "$tmp_dir"
}
trap cleanup EXIT INT TERM

echo "Building beaver-raft..."
go build -o "$tmp_dir/beaver-raft" ./cmd/queue
for i in 0 1 2; do
  "$tmp_dir/beaver-raft" --config configs/cluster.yaml run --mode master --port "${ports[$i]}" --listen "${addresses[$i]}" --node-id "${addresses[$i]}" --peers "$peers" --data-dir "$tmp_dir/node-${ports[$i]}" >"$tmp_dir/master-${ports[$i]}.log" 2>&1 &
  master_pids+=("$!")
done

echo "Waiting for a leader election..."
leader_port=""
for _ in $(seq 1 60); do
  for port in "${ports[@]}"; do
    if grep -q "elected leader" "$tmp_dir/master-${port}.log"; then leader_port="$port"; break; fi
  done
  [[ -n "$leader_port" ]] && break
  sleep 0.25
done
if [[ -z "$leader_port" ]]; then
  echo "No leader elected. Master logs follow:" >&2
  for port in "${ports[@]}"; do echo "--- master :$port ---" >&2; cat "$tmp_dir/master-${port}.log" >&2; done
  exit 1
fi

cat >"$tmp_dir/jobs.json" <<'EOF'
[
  {"id":"failover-1","payload":{"sleep_ms":500},"timeout_ms":20000},
  {"id":"failover-2","payload":{"sleep_ms":5000},"timeout_ms":20000},
  {"id":"failover-3","payload":{"sleep_ms":5000},"timeout_ms":20000},
  {"id":"failover-4","payload":{"sleep_ms":5000},"timeout_ms":20000},
  {"id":"failover-5","payload":{"sleep_ms":5000},"timeout_ms":20000},
  {"id":"failover-6","payload":{"sleep_ms":5000},"timeout_ms":20000}
]
EOF

"$tmp_dir/beaver-raft" --config configs/cluster.yaml enqueue --file "$tmp_dir/jobs.json" --master "$peers"
"$tmp_dir/beaver-raft" --config configs/cluster.yaml run --mode worker --master "$peers" >"$tmp_dir/worker.log" 2>&1 &
worker_pid="$!"

echo "Waiting for work to start before stopping elected leader :$leader_port..."
for _ in $(seq 1 100); do
  if grep -q "worker acknowledged job" "$tmp_dir/worker.log"; then break; fi
  sleep 0.1
done
sleep 0.2
kill -KILL "${master_pids[$((leader_port - 55131))]}"
echo "Stopped leader at ${addresses[$((leader_port - 55131))]}"

ack_count=0
for _ in $(seq 1 300); do
  ack_count="$(grep -c "worker acknowledged job" "$tmp_dir/worker.log" || true)"
  if [[ "$ack_count" -ge 6 ]]; then break; fi
  sleep 0.1
done
cat "$tmp_dir/worker.log"
if [[ "$ack_count" -lt 6 ]]; then
  echo "Only $ack_count of 6 jobs were acknowledged after leader loss. Master logs are in $tmp_dir" >&2
  for port in "${ports[@]}"; do cat "$tmp_dir/master-${port}.log" >&2; done
  exit 1
fi
echo "Observed $ack_count successful worker acknowledgements after leader loss."
