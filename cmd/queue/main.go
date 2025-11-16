// ============================================================================
// Beaver-Raft Queue System - Main Entry Point
// ============================================================================
//
// File: cmd/queue/main.go
// Purpose: Application entry point and CLI initialization
//
// Responsibilities:
//   1. Version Management - Inject build info via ldflags
//   2. Panic Recovery - Catch unexpected panics gracefully
//   3. CLI Setup - Build and configure Cobra command interface
//   4. Error Handling - Unified command execution error handling
//
// Version Injection:
//   Variables injected at build time via -ldflags:
//   go build -ldflags "-X main.version=1.0.0 -X main.commit=abc123"
//
// Usage:
//   ./beaver-raft --help              # Show help
//   ./beaver-raft --version           # Show version
//   ./beaver-raft run                 # Start queue system
//   ./beaver-raft enqueue -f jobs.json # Submit jobs
//   ./beaver-raft status              # View system status
//
// ============================================================================

package main

import (
	"fmt"
	"os"

	"github.com/ChuLiYu/raft-recovery/internal/cli"
)

// Build-time version injection via ldflags
// Example: go build -ldflags "-X main.version=1.0.0"
var (
	version = "1.0.0"   // Semantic version
	commit  = "dev"     // Git commit hash
	date    = "unknown" // Build timestamp
)

// main is the program entry point
// Initializes CLI, handles panics, and executes commands
func main() {
	// Global panic recovery
	// Prevents uncaught panics from crashing the program
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: %v\n", r)
			os.Exit(1)
		}
	}()

	// Build CLI command tree
	// Includes run, enqueue, status subcommands
	rootCmd := cli.BuildCLI()

	// Set version info for --version flag
	// Format: "1.0.0 (commit: abc123, built: 2025-10-31)"
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)

	// Execute command parsing and business logic
	// Exit with error code if command fails
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1)
	}
}
