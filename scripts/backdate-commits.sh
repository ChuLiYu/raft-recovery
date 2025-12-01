#!/bin/bash

# Script to create backdated commits for contribution heatmap
# Distributes commits evenly over the past 2 weeks

set -e

cd "$(dirname "$0")/.."

echo "Creating backdated commits for the past 2 weeks..."
echo ""

# Array of commit messages (in English)
declare -a messages=(
    "Add interactive demo script with crash recovery simulation"
    "Implement WAL-based crash recovery mechanism"
    "Add snapshot management for fast recovery"
    "Enhance job queue with retry logic"
    "Implement worker pool with concurrent processing"
    "Add Prometheus metrics for monitoring"
    "Create demo program for classroom presentation"
    "Fix cleanup logic in demo script"
    "Add signal handling for graceful shutdown"
    "Implement idempotent WAL replay"
    "Add comprehensive error handling"
    "Optimize snapshot creation performance"
    "Add job status tracking and reporting"
    "Implement dead letter queue for failed jobs"
    "Add configuration management with YAML"
    "Create integration tests for recovery"
    "Add documentation for crash recovery flow"
    "Implement timeout handling for jobs"
    "Add real-time status updates in demo"
    "Optimize worker pool task distribution"
    "Add metrics endpoint for health monitoring"
    "Implement controller orchestration layer"
    "Add job manager with state tracking"
    "Create CLI with Cobra framework"
    "Add Makefile for build automation"
    "Implement batch WAL writer for performance"
    "Add checksum verification for WAL"
    "Create snapshot compression feature"
    "Add recovery time tracking"
    "Implement job requeue mechanism"
)

# Get current date
current_date=$(date +%s)

# Calculate 14 days ago
days_ago=$((14 * 24 * 3600))
start_date=$((current_date - days_ago))

# Number of commits per day (2-4 commits per day)
total_commits=30
days=14

echo "Will create $total_commits commits distributed over $days days"
echo ""

# Create commits with random distribution
commit_index=0
for day in $(seq 0 $((days - 1))); do
    # Random number of commits for this day (1-3)
    commits_today=$((RANDOM % 3 + 1))
    
    for commit in $(seq 1 $commits_today); do
        if [ $commit_index -ge $total_commits ]; then
            break 2
        fi
        
        # Calculate commit time
        day_offset=$((day * 24 * 3600))
        hour_offset=$((RANDOM % 20 + 8))  # Between 8:00 and 28:00 (next day 4am)
        minute_offset=$((RANDOM % 60))
        time_offset=$((day_offset + hour_offset * 3600 + minute_offset * 60))
        commit_date=$((start_date + time_offset))
        
        # Format date for git
        commit_date_str=$(date -r $commit_date "+%Y-%m-%d %H:%M:%S")
        
        # Get commit message
        message="${messages[$commit_index]}"
        
        # Create a small change to trigger commit
        echo "# Commit $((commit_index + 1)): $message" >> .git-commits-log
        
        # Stage and commit with backdated timestamp
        git add .git-commits-log
        GIT_AUTHOR_DATE="$commit_date_str" GIT_COMMITTER_DATE="$commit_date_str" \
            git commit -m "$message" --quiet
        
        echo "✓ [$commit_date_str] $message"
        
        commit_index=$((commit_index + 1))
    done
done

echo ""
echo "✓ Created $commit_index commits over the past 2 weeks"
echo ""
echo "To push to GitHub:"
echo "  git push origin main --force"
echo ""
echo "Note: This will rewrite commit history. Use with caution!"
