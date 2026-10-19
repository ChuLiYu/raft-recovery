package cli

import (
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	pb "github.com/ChuLiYu/raft-recovery/api/proto/v1"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type enqueueTestServer struct {
	pb.UnimplementedFalconQueueServiceServer
	responses map[string]bool
	failures  map[string]error
}

func (s *enqueueTestServer) SubmitJob(_ context.Context, req *pb.SubmitJobRequest) (*pb.SubmitJobResponse, error) {
	if err := s.failures[req.JobId]; err != nil {
		return nil, err
	}
	accepted := s.responses[req.JobId]
	response := &pb.SubmitJobResponse{Success: accepted}
	if !accepted {
		response.ErrorMessage = "rejected by test server"
	}
	return response, nil
}

func startEnqueueTestServer(t *testing.T, service *enqueueTestServer) string {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	require.NoError(t, err)
	server := grpc.NewServer()
	pb.RegisterFalconQueueServiceServer(server, service)
	go func() { _ = server.Serve(listener) }()
	t.Cleanup(func() { server.Stop() })
	return listener.Addr().String()
}

func TestBuildCLI(t *testing.T) {
	cmd := BuildCLI()

	assert.NotNil(t, cmd, "BuildCLI should return a non-nil command")
	assert.Equal(t, "beaver-raft", cmd.Use, "Root command should be 'beaver-raft'")
	assert.Equal(t, "1.0.0", cmd.Version, "Version should be 1.0.0")

	// Check subcommands
	commands := cmd.Commands()
	assert.Len(t, commands, 3, "Should have 3 subcommands")

	commandNames := make(map[string]bool)
	for _, c := range commands {
		commandNames[c.Use] = true
	}

	assert.True(t, commandNames["run"], "Should have 'run' command")
	assert.True(t, commandNames["enqueue"], "Should have 'enqueue' command")
	assert.True(t, commandNames["status"], "Should have 'status' command")

	// Check persistent flags
	configFlag := cmd.PersistentFlags().Lookup("config")
	assert.NotNil(t, configFlag, "Should have --config flag")
	assert.Equal(t, "configs/default.yaml", configFlag.DefValue, "Default config path should be configs/default.yaml")
}

func TestBuildRunCommand(t *testing.T) {
	cmd := buildRunCommand()

	assert.NotNil(t, cmd, "buildRunCommand should return a non-nil command")
	assert.Equal(t, "run", cmd.Use, "Command should be 'run'")
	assert.Contains(t, cmd.Short, "Start", "Short description should mention 'Start'")
	assert.NotNil(t, cmd.RunE, "RunE function should be set")
}

func TestBuildRunCommandClusterFlags(t *testing.T) {
	cmd := buildRunCommand()
	for _, name := range []string{"node-id", "peers", "data-dir", "listen", "port", "master"} {
		assert.NotNil(t, cmd.Flags().Lookup(name), "run should expose --%s", name)
	}
	assert.Equal(t, "", cmd.Flags().Lookup("peers").DefValue, "empty peers use the safe one-node default")
}

func TestResolveMasterMembers(t *testing.T) {
	t.Run("single node defaults to its listen address", func(t *testing.T) {
		members, err := resolveMasterMembers(":50051", "")
		require.NoError(t, err)
		assert.Equal(t, []string{":50051"}, members)
	})
	t.Run("advertised identity is a cluster member", func(t *testing.T) {
		members, err := resolveMasterMembers("127.0.0.1:50051", "127.0.0.1:50051,127.0.0.1:50052")
		require.NoError(t, err)
		assert.Equal(t, []string{"127.0.0.1:50051", "127.0.0.1:50052"}, members)
	})
	t.Run("local identity must be in peer list", func(t *testing.T) {
		_, err := resolveMasterMembers("127.0.0.1:50051", "127.0.0.1:50052,127.0.0.1:50053")
		assert.ErrorContains(t, err, "must appear in --peers")
	})
	t.Run("rejects wildcard identity for a cluster", func(t *testing.T) {
		_, err := resolveMasterMembers(":50051", ":50051,127.0.0.1:50052")
		assert.ErrorContains(t, err, "not routable")
	})
}

func TestBuildEnqueueCommand(t *testing.T) {
	cmd := buildEnqueueCommand()

	assert.NotNil(t, cmd, "buildEnqueueCommand should return a non-nil command")
	assert.Equal(t, "enqueue", cmd.Use, "Command should be 'enqueue'")

	// Check --file flag
	fileFlag := cmd.Flags().Lookup("file")
	assert.NotNil(t, fileFlag, "Should have --file flag")
	assert.Equal(t, "f", fileFlag.Shorthand, "Should have -f shorthand")

	// Check RunE function (do not execute, just check existence)
	assert.NotNil(t, cmd.RunE, "RunE function should be set")
}

func TestBuildStatusCommand(t *testing.T) {
	cmd := buildStatusCommand()

	assert.NotNil(t, cmd, "buildStatusCommand should return a non-nil command")
	assert.Equal(t, "status", cmd.Use, "Command should be 'status'")
	assert.Contains(t, cmd.Short, "status", "Short description should mention 'status'")
	assert.NotNil(t, cmd.RunE, "RunE function should be set")
}

func TestLoadConfig_ValidYAML(t *testing.T) {
	// Create temporary config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "test_config.yaml")

	configContent := `
worker:
  worker_count: 4
  task_timeout: 5s

wal:
  dir: "./test_wal"
  max_segment_size: 1048576
  sync_interval: 5
  retention_seconds: 3600
  buffer_size: 50

snapshot:
  dir: "./test_snapshot"
  interval_seconds: 15
  retention_count: 3

metrics:
  enabled: true
  port: 8080
`

	err := os.WriteFile(configPath, []byte(configContent), 0644)
	require.NoError(t, err, "Failed to write test config file")

	// Load config
	cfg, err := loadConfig(configPath)
	require.NoError(t, err, "loadConfig should not return an error")
	require.NotNil(t, cfg, "Config should not be nil")

	// verify Worker config
	assert.Equal(t, 4, cfg.Worker.WorkerCount, "Worker count should be 4")
	assert.Equal(t, 5*time.Second, cfg.Worker.TaskTimeout, "Task timeout should be 5s")

	// verify WAL config
	assert.Equal(t, "./test_wal", cfg.WAL.Dir, "WAL dir should be ./test_wal")
	assert.Equal(t, int64(1048576), cfg.WAL.MaxSegmentSize, "Max segment size should be 1048576")
	assert.Equal(t, 5, cfg.WAL.SyncInterval, "Sync interval should be 5")
	assert.Equal(t, 3600, cfg.WAL.RetentionSeconds, "Retention seconds should be 3600")
	assert.Equal(t, 50, cfg.WAL.BufferSize, "Buffer size should be 50")

	// verify Snapshot config
	assert.Equal(t, "./test_snapshot", cfg.Snapshot.Dir, "Snapshot dir should be ./test_snapshot")
	assert.Equal(t, 15, cfg.Snapshot.IntervalSeconds, "Interval seconds should be 15")
	assert.Equal(t, 3, cfg.Snapshot.RetentionCount, "Retention count should be 3")

	// verify Metrics config
	assert.True(t, cfg.Metrics.Enabled, "Metrics should be enabled")
	assert.Equal(t, 8080, cfg.Metrics.Port, "Metrics port should be 8080")
}

func TestLoadConfig_FileNotFound(t *testing.T) {
	cfg, err := loadConfig("/nonexistent/config.yaml")

	assert.Error(t, err, "loadConfig should return an error for nonexistent file")
	assert.Nil(t, cfg, "Config should be nil on error")
	assert.Contains(t, err.Error(), "failed to read config file", "Error should mention file reading failure")
}

func TestLoadConfig_InvalidYAML(t *testing.T) {
	// Create a temporary file containing invalid YAML
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "invalid.yaml")

	invalidYAML := `
worker:
  worker_count: "not a number"
  invalid yaml structure
    broken indentation
`

	err := os.WriteFile(configPath, []byte(invalidYAML), 0644)
	require.NoError(t, err, "Failed to write invalid YAML file")

	cfg, err := loadConfig(configPath)

	assert.Error(t, err, "loadConfig should return an error for invalid YAML")
	assert.Nil(t, cfg, "Config should be nil on parse error")
	assert.Contains(t, err.Error(), "failed to parse config YAML", "Error should mention YAML parsing failure")
}

func TestLoadConfig_EmptyFile(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "empty.yaml")

	err := os.WriteFile(configPath, []byte(""), 0644)
	require.NoError(t, err, "Failed to write empty file")

	// Empty file should parse, but fields will have zero values
	cfg, err := loadConfig(configPath)
	assert.NoError(t, err, "Empty YAML file should parse without error")
	assert.NotNil(t, cfg, "Config should not be nil for empty file")
	assert.Equal(t, 0, cfg.Worker.WorkerCount, "Empty config should have zero values")
}

func TestLoadConfig_PartialConfig(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "partial.yaml")

	// Only partial config
	partialConfig := `
worker:
  worker_count: 2
`

	err := os.WriteFile(configPath, []byte(partialConfig), 0644)
	require.NoError(t, err, "Failed to write partial config")

	cfg, err := loadConfig(configPath)
	require.NoError(t, err, "Partial config should parse successfully")
	assert.Equal(t, 2, cfg.Worker.WorkerCount, "Worker count should be set")
	assert.Empty(t, cfg.WAL.Dir, "Unset fields should have zero values")
}

func TestEnqueueJobs_InvalidFile(t *testing.T) {
	err := enqueueJobs("/nonexistent/jobs.json")

	assert.Error(t, err, "enqueueJobs should return error for nonexistent file")
	assert.Contains(t, err.Error(), "failed to read job file", "Error should mention file reading failure")
}

func TestEnqueueJobs_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	jobFile := filepath.Join(tmpDir, "invalid.json")

	invalidJSON := `{"invalid json structure`

	err := os.WriteFile(jobFile, []byte(invalidJSON), 0644)
	require.NoError(t, err, "Failed to write invalid JSON")

	err = enqueueJobs(jobFile)

	assert.Error(t, err, "enqueueJobs should return error for invalid JSON")
	assert.Contains(t, err.Error(), "failed to parse job file", "Error should mention JSON parsing failure")
}

func TestEnqueueJobs_RemoteSubmissionResults(t *testing.T) {
	tests := []struct {
		name      string
		service   *enqueueTestServer
		wantError string
	}{
		{
			name: "all jobs accepted",
			service: &enqueueTestServer{responses: map[string]bool{
				"job-1": true,
				"job-2": true,
			}},
		},
		{
			name: "partial rejection returns error",
			service: &enqueueTestServer{responses: map[string]bool{
				"job-1": true,
			}, failures: map[string]error{}},
			wantError: "submitted 1/2 jobs",
		},
		{
			name: "remote RPC failure returns error",
			service: &enqueueTestServer{responses: map[string]bool{}, failures: map[string]error{
				"job-1": status.Error(codes.Unavailable, "temporarily unavailable"),
				"job-2": status.Error(codes.Unavailable, "temporarily unavailable"),
			}},
			wantError: "submitted 0/2 jobs",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			address := startEnqueueTestServer(t, tt.service)
			jobFile := filepath.Join(t.TempDir(), "jobs.json")
			jobs := []map[string]interface{}{
				{"id": "job-1", "payload": map[string]interface{}{"value": "one"}},
				{"id": "job-2", "payload": map[string]interface{}{"value": "two"}},
			}
			data, err := json.Marshal(jobs)
			require.NoError(t, err)
			require.NoError(t, os.WriteFile(jobFile, data, 0600))

			err = enqueueJobs(jobFile, address)
			if tt.wantError == "" {
				require.NoError(t, err)
				return
			}
			require.Error(t, err)
			assert.ErrorContains(t, err, tt.wantError)
		})
	}
}

func TestShowStatus(t *testing.T) {
	oldConfigFile := configFile
	configFile = filepath.Join("..", "..", "configs", "default.yaml")
	t.Cleanup(func() { configFile = oldConfigFile })

	// showStatus only prints output and should not return an error
	err := showStatus()
	assert.NoError(t, err, "showStatus should not return an error")
}

func TestConfigStructure(t *testing.T) {
	// ensure Config struct fields are correctly defined
	cfg := Config{}

	// check nested fields are accessible
	cfg.Worker.WorkerCount = 10
	cfg.Worker.TaskTimeout = 5 * time.Second
	cfg.WAL.Dir = "/test"
	cfg.WAL.BufferSize = 100
	cfg.Snapshot.Dir = "/snapshot"
	cfg.Snapshot.IntervalSeconds = 30
	cfg.Metrics.Enabled = true
	cfg.Metrics.Port = 9090

	assert.Equal(t, 10, cfg.Worker.WorkerCount)
	assert.Equal(t, 5*time.Second, cfg.Worker.TaskTimeout)
	assert.Equal(t, "/test", cfg.WAL.Dir)
	assert.Equal(t, 100, cfg.WAL.BufferSize)
	assert.Equal(t, "/snapshot", cfg.Snapshot.Dir)
	assert.Equal(t, 30, cfg.Snapshot.IntervalSeconds)
	assert.True(t, cfg.Metrics.Enabled)
	assert.Equal(t, 9090, cfg.Metrics.Port)
}
