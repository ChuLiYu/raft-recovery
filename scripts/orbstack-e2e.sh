#!/usr/bin/env bash
set -Eeuo pipefail

repo_dir="$(cd "$(dirname "${BASH_SOURCE[0]}")/.." && pwd)"
context="${RAFT_DOCKER_CONTEXT:-orbstack}"
project="${RAFT_E2E_PROJECT:-raft-recovery-e2e}"
main_project="${RAFT_PROJECT_NAME:-raft-recovery}"
compose_file="$repo_dir/compose.yaml"
compose=(docker --context "$context" compose -f "$compose_file" -p "$project")
masters="master1:50051,master2:50051,master3:50051"
run_id="$(date -u +%Y%m%dT%H%M%SZ)-$$"
worker_job="orb-e2e-worker-$run_id"
failover_job="orb-e2e-failover-$run_id"
durable_job_a="orb-e2e-durable-a-$run_id"
durable_job_b="orb-e2e-durable-b-$run_id"
recovery_job="orb-e2e-quorum-$run_id"
minority_job="orb-e2e-minority-$run_id"
cleanup_started=0

probe() {
  "${compose[@]}" run --rm -T --no-deps --entrypoint /usr/local/bin/e2e-probe cli "$@"
}

if [[ "$project" == "$main_project" ]]; then
  echo "Refusing to run E2E with project '$project': it matches RAFT_PROJECT_NAME/default project." >&2
  exit 1
fi
existing_containers="$("${compose[@]}" ps --all --quiet)"
existing_volumes="$(docker --context "$context" volume ls --quiet \
  --filter "label=com.docker.compose.project=$project")"
existing_networks="$(docker --context "$context" network ls --quiet \
  --filter "label=com.docker.compose.project=$project")"
if [[ -n "$existing_containers" || -n "$existing_volumes" || -n "$existing_networks" ]]; then
  echo "Refusing E2E: project '$project' already has containers, volumes, or networks. Choose a fresh RAFT_E2E_PROJECT or remove that disposable project first." >&2
  exit 1
fi

wait_running() {
  local service="$1"
  local deadline=$((SECONDS + 30))
  until [[ -n "$("${compose[@]}" ps -q --status running "$service")" ]]; do
    if (( SECONDS >= deadline )); then
      echo "Service $service did not enter running state within 30 seconds." >&2
      return 1
    fi
    sleep 1
  done
}

diagnostics() {
  echo "--- E2E containers ($project) ---"
  "${compose[@]}" ps -a || true
  echo "--- E2E logs ($project) ---"
  "${compose[@]}" logs --no-color --timestamps || true
}

cleanup() {
  local rc=$?
  trap - EXIT INT TERM
  if (( rc != 0 )); then diagnostics; fi
  if (( cleanup_started )); then
    "${compose[@]}" down --volumes --remove-orphans || rc=1
  fi
  exit "$rc"
}
trap cleanup EXIT
trap 'exit 130' INT
trap 'exit 143' TERM

if [[ ! -f "$compose_file" ]]; then
  echo "Compose file not found: $compose_file" >&2
  exit 1
fi
"${compose[@]}" config -q
cleanup_started=1

echo "Starting isolated OrbStack E2E project '$project' on context '$context'."
"${compose[@]}" up -d master1 master2 master3 worker

echo "Waiting for a leader through RegisterWorker gRPC."
initial_leader="$(probe leader --masters "$masters")"
echo "Initial leader: $initial_leader"

echo "Checking the real worker with unique job $worker_job."
probe submit --masters "$masters" --id "$worker_job" --payload "{\"e2e_id\":\"$worker_job\",\"sleep_ms\":25}"
worker_deadline=$((SECONDS + 60))
until "${compose[@]}" logs --no-color worker | grep -Fq "worker acknowledged job $worker_job attempt 1 success=true"; do
  if (( SECONDS >= worker_deadline )); then
    echo "Real worker did not ACK exact job ID $worker_job within 60 seconds." >&2
    exit 1
  fi
  sleep 1
done
echo "Real worker ACKed $worker_job attempt 1."

current_leader="$(probe leader --masters "$masters")"
echo "Killing the currently serving leader $current_leader with SIGKILL."
"${compose[@]}" kill -s SIGKILL "$current_leader"
failover_leader="$(probe leader --masters "$masters")"
if [[ "$failover_leader" == "$current_leader" ]]; then
	echo "Leader discovery returned killed node $current_leader." >&2
  exit 1
fi
echo "New leader: $failover_leader"
echo "Checking worker progress after leader failure with unique job $failover_job."
probe submit --masters "$masters" --id "$failover_job" --payload "{\"e2e_id\":\"$failover_job\",\"sleep_ms\":25}"
worker_deadline=$((SECONDS + 60))
until "${compose[@]}" logs --no-color worker | grep -Fq "worker acknowledged job $failover_job attempt 1 success=true"; do
  if (( SECONDS >= worker_deadline )); then
    echo "Worker did not ACK post-failure job $failover_job." >&2
    exit 1
  fi
  sleep 1
done
echo "Restarting former leader $current_leader."
"${compose[@]}" start "$current_leader"
wait_running "$current_leader"
rejoined_leader="$(probe leader --masters "$masters")"
echo "Cluster leader after former leader restart: $rejoined_leader"

echo "Stopping the worker and submitting two unique jobs for durable pending recovery."
"${compose[@]}" stop worker
probe submit --masters "$masters" --id "$durable_job_a" --payload "{\"e2e_id\":\"$durable_job_a\",\"marker\":\"durable-a\"}"
probe submit --masters "$masters" --id "$durable_job_b" --payload "{\"e2e_id\":\"$durable_job_b\",\"marker\":\"durable-b\"}"

echo "Stopping all masters, then starting the same services with the same project volumes."
"${compose[@]}" stop master1 master2 master3
"${compose[@]}" start master1 master2 master3
wait_running master1
wait_running master2
wait_running master3
probe leader --masters "$masters"
probe drain --masters "$masters" --worker-id "e2e-recovery-$run_id" --ids "$durable_job_a,$durable_job_b"

echo "Resubmitting pre-restart worker ACK IDs to verify completed dedup survived the restart."
probe submit --masters "$masters" --id "$worker_job" --payload "{\"e2e_id\":\"$worker_job\",\"marker\":\"must-not-replace\"}"
probe submit --masters "$masters" --id "$failover_job" --payload "{\"e2e_id\":\"$failover_job\",\"marker\":\"must-not-replace\"}"
probe no-delivery --masters "$masters" --worker-id "e2e-dedup-worker-$run_id" --ids "$worker_job,$failover_job" --duration 4s

echo "Resubmitting durable completed IDs with changed payloads; the original jobs must remain completed."
probe submit --masters "$masters" --id "$durable_job_a" --payload "{\"e2e_id\":\"$durable_job_a\",\"marker\":\"must-not-replace\"}"
probe submit --masters "$masters" --id "$durable_job_b" --payload "{\"e2e_id\":\"$durable_job_b\",\"marker\":\"must-not-replace\"}"
probe no-delivery --masters "$masters" --worker-id "e2e-dedup-$run_id" --ids "$durable_job_a,$durable_job_b" --duration 4s

minority_node="$(probe leader --masters "$masters")"
echo "Isolating only current leader $minority_node to verify minority writes cannot be acknowledged."
for master in master1 master2 master3; do
  if [[ "$master" != "$minority_node" ]]; then "${compose[@]}" stop "$master"; fi
done
reachable_minority="$(probe leader --masters "$minority_node:50051")"
if [[ "$reachable_minority" != "$minority_node" ]]; then
  echo "Expected surviving minority node $minority_node to accept RegisterWorker, got $reachable_minority." >&2
  exit 1
fi
probe minority --masters "$masters" --id "$minority_job"
echo "Restoring all three masters and checking quorum writes recover."
"${compose[@]}" start master1 master2 master3
wait_running master1
wait_running master2
wait_running master3
probe leader --masters "$masters"
probe submit --masters "$masters" --id "$recovery_job" --payload "{\"e2e_id\":\"$recovery_job\",\"marker\":\"quorum-restored\"}"
probe submit --masters "$masters" --id "$minority_job" --payload "{\"e2e_id\":\"$minority_job\",\"marker\":\"minority-write-retried-after-quorum\"}"
probe drain --masters "$masters" --worker-id "e2e-quorum-$run_id" --ids "$recovery_job,$minority_job"

echo "OrbStack cross-container E2E passed for exact job IDs: $worker_job, $failover_job, $durable_job_a, $durable_job_b, $minority_job, $recovery_job"
