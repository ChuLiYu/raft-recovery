package integration

import (
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/stretchr/testify/require"
)

func BenchmarkThroughput(b *testing.B) {
	config := controller.Config{
		WorkerCount:      8,
		TaskTimeout:      5 * time.Second,
		SnapshotInterval: 2 * time.Second,
		WALPath:          "/tmp/test-wal.log",
		SnapshotPath:     "/tmp/test-snapshot.json",
	}
	ctrl, err := controller.NewController(config)
	require.NoError(b, err)
	go ctrl.Start()
	defer ctrl.Stop()

	// 模擬高併發任務
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jobs := generateTestJobs(1000)
		err = ctrl.EnqueueJobs(jobs)
		require.NoError(b, err)
	}
	b.StopTimer()
}
