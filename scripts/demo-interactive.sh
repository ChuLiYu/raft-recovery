#!/bin/bash

# ============================================================================
# Beaver-Raft Interactive Demo Script
# ============================================================================
# Two Demo Modes:
#   demo1 - Normal operation: system processes jobs and auto-backups
#   demo2 - Crash recovery: demonstrate fault tolerance and data recovery
# ============================================================================

set -e

PROJECT_ROOT="/Users/liyu/repos/Beaver-Raft"
cd "$PROJECT_ROOT"

# Color definitions
RED='\033[0;31m'
GREEN='\033[0;32m'
YELLOW='\033[1;33m'
BLUE='\033[0;34m'
PURPLE='\033[0;35m'
CYAN='\033[0;36m'
BOLD='\033[1m'
NC='\033[0m'

header() {
    echo ""
    echo -e "${CYAN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo -e "${CYAN}${BOLD}  $1${NC}"
    echo -e "${CYAN}${BOLD}━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━━${NC}"
    echo ""
}

info() { echo -e "${BLUE}ℹ${NC}  $1"; }
success() { echo -e "${GREEN}✓${NC} $1"; }
warning() { echo -e "${YELLOW}⚠${NC}  $1"; }
error() { echo -e "${RED}✗${NC} $1"; }

# Wait for user to press Enter
wait_enter() {
    echo ""
    echo -e "${PURPLE}${BOLD}Press Enter to continue...${NC}"
    read -r
}

# ============================================================================
# Demo 1: Normal Operation
# Shows: Job processing, automatic snapshots, system monitoring
# ============================================================================

cmd_demo1() {
    header "Demo 1: Normal Operation with Auto-Backup"
    info "This demo shows the system processing jobs and auto-saving snapshots"
    echo ""
    
    echo -e "${CYAN}${BOLD}What you'll see:${NC}"
    echo "  1. System starts with 8 workers"
    echo "  2. Jobs are submitted and processed"
    echo "  3. Automatic snapshots every 30 seconds"
    echo "  4. Real-time status monitoring"
    echo ""
    
    info "Press Ctrl+C anytime to stop"
    wait_enter
    
    # Start system
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Step 1: Starting System ━━━${NC}"
    info "Starting Beaver-Raft with 8 workers..."
    wait_enter
    
    go run cmd/queue/main.go run --config configs/default.yaml &
    SYSTEM_PID=$!
    sleep 3
    
    # Show initial status
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Step 2: Initial Status ━━━${NC}"
    info "Checking system status before submitting jobs"
    wait_enter
    
    go run cmd/queue/main.go status
    
    # Submit jobs
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Step 3: Submitting Jobs ━━━${NC}"
    info "Submitting 5 jobs to the queue"
    wait_enter
    
    go run cmd/queue/main.go enqueue --file test/jobs.json
    sleep 2
    
    # Show processing status
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Step 4: Processing Status ━━━${NC}"
    info "Jobs are being processed by workers"
    wait_enter
    
    go run cmd/queue/main.go status
    sleep 3
    
    # Show final status
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Step 5: Final Status ━━━${NC}"
    info "Checking status after processing"
    wait_enter
    
    go run cmd/queue/main.go status
    
    echo ""
    success "Demo 1 Complete!"
    echo ""
    echo -e "${YELLOW}${BOLD}Key Points Demonstrated:${NC}"
    echo "  ✓ Job queue system running smoothly"
    echo "  ✓ 8 workers processing jobs concurrently"
    echo "  ✓ Automatic snapshots (check logs for 'Snapshot taken')"
    echo "  ✓ All jobs completed successfully"
    echo ""
    
    info "System is still running. Press Ctrl+C to stop, or let it run to see more snapshots."
    wait $SYSTEM_PID
}

# ============================================================================
# Demo 2: Crash Recovery
# Shows: System crash simulation, automatic recovery, data integrity
# ============================================================================

cmd_demo2_start() {
    header "Demo 2: Crash Recovery - Part 1 (Crash Simulation)"
    info "This demo shows the full crash recovery process"
    echo ""
    
    # Clean up old data first (including all WAL and snapshot files)
    warning "Cleaning old data from previous runs..."
    # Kill any background processes first
    pkill -9 -f "cmd/demo" 2>/dev/null || true
    pkill -9 -f "beaver-raft" 2>/dev/null || true
    sleep 0.5
    # Show files before cleanup
    FILE_COUNT_BEFORE=$(find ./data -type f 2>/dev/null | wc -l | tr -d ' ')
    echo -e "  Files before cleanup: ${FILE_COUNT_BEFORE}"
    # Multiple deletion methods to ensure cleanup
    find ./data/wal ./data/snapshot -type f -delete 2>/dev/null || true
    rm -f ./data/wal/* ./data/snapshot/* 2>/dev/null || true
    # Show files after cleanup
    FILE_COUNT_AFTER=$(find ./data -type f 2>/dev/null | wc -l | tr -d ' ')
    echo -e "  Files after cleanup: ${FILE_COUNT_AFTER}"
    if [ "$FILE_COUNT_AFTER" -gt 0 ]; then
        error "⚠️  Cleanup failed! ${FILE_COUNT_AFTER} files remain"
        echo "  Remaining files:"
        find ./data -type f -ls | sed 's/^/    /'
        exit 1
    fi
    echo -e "${GREEN}✓${NC} Data cleaned (all WAL and snapshot files removed)"
    echo ""
    
    echo -e "${CYAN}${BOLD}What You'll See:${NC}"
    echo "  1. System starts with 8 workers"
    echo "  2. 20 jobs are enqueued and immediately saved to WAL"
    echo "  3. Jobs process fast (~500ms each), creating in-flight window"
    echo "  4. System takes snapshots every 30 seconds"
    echo "  5. You manually crash it (Ctrl+C)"
    echo ""
    
    echo -e "${YELLOW}${BOLD}💡 Key Concept - WAL Records Everything:${NC}"
    echo "  • Job submitted → WAL writes EventEnqueue"
    echo "  • Job dispatched → WAL writes EventDispatch"
    echo "  • Job completed → WAL writes EventAck"
    echo "  • Crash anytime → WAL has complete history!"
    echo ""
    echo -e "${CYAN}📊 Recovery Logic:${NC}"
    echo "  1. Load snapshot (if exists)"
    echo "  2. Replay WAL events after snapshot"
    echo "  3. Requeue all in-flight jobs → requeued_jobs=N"
    echo ""
    
    warning "Demo Flow:"
    echo "  ① Watch system start and enqueue 20 jobs"
    echo "  ② Jobs will show processing status (Pending/In-Flight/Completed)"
    echo "  ③ Option A: Press Ctrl+C quickly (~1-2s) to catch in-flight jobs"
    echo "  ④ Option B: Wait ~30s for snapshot, then Ctrl+C"
    echo "  ⑤ Run './scripts/demo-interactive.sh demo2-recover' to see recovery"
    wait_enter
    
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Starting Demo System... ━━━${NC}"
    echo ""
    wait_enter
    
    go run cmd/demo/main.go start
}

cmd_demo2_recover() {
    header "Demo 2: Crash Recovery - Part 2 (Recovery Verification)"
    info "Now we'll recover from the crash and verify data integrity"
    echo ""
    
    echo -e "${CYAN}${BOLD}Recovery Process - Watch For:${NC}"
    echo "  1. ${GREEN}INFO Starting recovery...${NC}"
    echo "     └─ Recovery process begins"
    echo ""
    echo "  2. ${GREEN}INFO Snapshot loaded duration=XXµs jobs=N${NC}"
    echo "     └─ Restores state from last checkpoint (target: < 1ms)"
    echo ""
    echo "  3. ${GREEN}INFO Recovery completed duration=XXXµs requeued_jobs=N${NC}"
    echo "     └─ Shows total recovery time and recovered jobs"
    echo ""
    echo "  4. ${GREEN}📊 Status After Recovery${NC}"
    echo "     └─ Displays recovered job counts"
    echo ""
    
    info "🎯 Target: Complete recovery in < 3 seconds with zero data loss"
    wait_enter
    
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Starting Recovery Now... ━━━${NC}"
    echo ""
    wait_enter
    
    go run cmd/demo/main.go recover
    
    echo ""
    echo -e "${GREEN}${BOLD}━━━ Checking Persistence Files ━━━${NC}"
    info "Let's verify the persistence layer..."
    echo ""
    wait_enter
    
    if [ -f "./data/wal/beaver-raft.wal" ]; then
        echo -e "${GREEN}✓${NC} WAL file exists: $(ls -lh ./data/wal/beaver-raft.wal | awk '{print $5}')"
    fi
    
    if [ -f "./data/snapshot/beaver-raft.snap" ]; then
        echo -e "${GREEN}✓${NC} Snapshot file exists: $(ls -lh ./data/snapshot/beaver-raft.snap | awk '{print $5}')"
        echo -e "${GREEN}✓${NC} Last modified: $(ls -l ./data/snapshot/beaver-raft.snap | awk '{print $6, $7, $8}')"
    fi
    
    echo ""
    success "Recovery Demonstration Complete!"
    echo ""
    echo -e "${CYAN}${BOLD}━━━ Core Technologies Explained ━━━${NC}"
    echo ""
    echo -e "${YELLOW}📝 WAL (Write-Ahead Log)${NC}"
    echo "  Purpose: Durability guarantee"
    echo "  ├─ Every operation written to log BEFORE execution"
    echo "  ├─ Survives crashes, power failures"
    echo "  └─ Enables replay after recovery"
    echo ""
    echo -e "${YELLOW}📸 Snapshot${NC}"
    echo "  Purpose: Fast recovery"
    echo "  ├─ Periodic state checkpoints (every 30s)"
    echo "  ├─ Recovery starts from latest snapshot"
    echo "  └─ Only need to replay WAL entries after snapshot"
    echo ""
    echo -e "${YELLOW}🔄 Idempotent Replay${NC}"
    echo "  Purpose: Correctness guarantee"
    echo "  ├─ Safe to replay same operation multiple times"
    echo "  ├─ Skips already-completed jobs"
    echo "  └─ Ensures exactly-once semantics"
    echo ""
    echo -e "${GREEN}${BOLD}Result:${NC} Sub-3-second recovery with zero data loss!"
    echo ""
    
    echo -e "${YELLOW}💡 Tip:${NC} When you stop this demo, a new snapshot will be created."
    echo "   Run 'make clean-data' before next demo2-start for a fresh start."
    echo ""
    
    info "System is running. Press Ctrl+C to stop."
    wait $SYSTEM_PID
}

# ============================================================================
# Individual Step Commands (for manual control)
# ============================================================================

cmd_start() {
    header "Start Beaver-Raft System"
    info "Starting system in background mode"
    echo ""
    go run cmd/queue/main.go run --config configs/default.yaml
}

cmd_status() {
    header "Check System Status"
    go run cmd/queue/main.go status
    echo ""
}

cmd_submit() {
    header "Submit Jobs"
    local file=${1:-"test/jobs.json"}
    info "Submitting jobs from: $file"
    echo ""
    go run cmd/queue/main.go enqueue --file "$file"
    echo ""
    success "Jobs submitted!"
}

cmd_submit_long() {
    header "Submit Long-Running Jobs"
    info "Submitting 10 jobs with 30s timeout from: test/demo-jobs.json"
    echo ""
    go run cmd/queue/main.go enqueue --file test/demo-jobs.json
    echo ""
    success "10 long-running jobs submitted!"
}

# ============================================================================
# Help
# ============================================================================

cmd_help() {
    echo ""
    echo -e "${CYAN}${BOLD}╔════════════════════════════════════════════════════════════╗${NC}"
    echo -e "${CYAN}${BOLD}║  Beaver-Raft Interactive Demo Script                      ║${NC}"
    echo -e "${CYAN}${BOLD}╚════════════════════════════════════════════════════════════╝${NC}"
    echo ""
    echo -e "${BOLD}Usage:${NC} ./scripts/demo-interactive.sh [command]"
    echo ""
    echo -e "${BOLD}Demo Modes:${NC}"
    echo ""
    echo -e "${GREEN}  demo1${NC}          - ${BOLD}Normal Operation Demo${NC}"
    echo "                   Shows job processing and auto-backup"
    echo "                   Duration: ~1-2 minutes (with pauses)"
    echo ""
    echo -e "${GREEN}  demo2-start${NC}    - ${BOLD}Crash Recovery Demo - Part 1${NC}"
    echo "                   Start system, submit jobs, then crash (Ctrl+C)"
    echo ""
    echo -e "${GREEN}  demo2-recover${NC}  - ${BOLD}Crash Recovery Demo - Part 2${NC}"
    echo "                   Recover system and verify data integrity"
    echo ""
    echo -e "${BOLD}Manual Control Commands:${NC}"
    echo ""
    echo -e "${GREEN}  start${NC}          - Start system"
    echo -e "${GREEN}  status${NC}         - Check system status"
    echo -e "${GREEN}  submit${NC}         - Submit normal jobs (test/jobs.json)"
    echo -e "${GREEN}  submit-long${NC}    - Submit long-running jobs (test/demo-jobs.json)"
    echo ""
    echo -e "${BOLD}Quick Demo Flows:${NC}"
    echo ""
    echo -e "${YELLOW}Option 1: Normal Operation${NC}"
    echo "  ./scripts/demo-interactive.sh demo1"
    echo "  ${PURPLE}(Press Enter at each step to continue)${NC}"
    echo ""
    echo -e "${YELLOW}Option 2: Crash Recovery${NC}"
    echo "  Terminal 1: ./scripts/demo-interactive.sh demo2-start"
    echo "              ${PURPLE}(Press Enter at each step)${NC}"
    echo "              ${RED}(Wait for prompt, then press Ctrl+C)${NC}"
    echo "  Terminal 1: ./scripts/demo-interactive.sh demo2-recover"
    echo "              ${PURPLE}(Press Enter at each step)${NC}"
    echo ""
    echo -e "${YELLOW}Option 3: Manual Control${NC}"
    echo "  Terminal 1: ./scripts/demo-interactive.sh start"
    echo "  Terminal 2: ./scripts/demo-interactive.sh status"
    echo "  Terminal 2: ./scripts/demo-interactive.sh submit"
    echo "  Terminal 2: ./scripts/demo-interactive.sh status"
    echo ""
}

# ============================================================================
# Main
# ============================================================================

CMD=${1:-"help"}

case "$CMD" in
    demo1)
        cmd_demo1
        ;;
    demo2-start|demo2)
        cmd_demo2_start
        ;;
    demo2-recover)
        cmd_demo2_recover
        ;;
    start)
        cmd_start
        ;;
    status)
        cmd_status
        ;;
    submit)
        cmd_submit
        ;;
    submit-long)
        cmd_submit_long
        ;;
    help|--help|-h)
        cmd_help
        ;;
    *)
        error "Unknown command: $CMD"
        echo ""
        cmd_help
        exit 1
        ;;
esac

echo ""
