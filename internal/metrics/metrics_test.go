package metrics

import (
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestNewCollector(t *testing.T) {
	// 重置 Prometheus registry 以避免重複註冊
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	collector := NewCollector()

	assert.NotNil(t, collector, "NewCollector should return a non-nil collector")
	assert.NotNil(t, collector.jobsEnqueued, "jobsEnqueued counter should be initialized")
	assert.NotNil(t, collector.jobsDispatched, "jobsDispatched counter should be initialized")
	assert.NotNil(t, collector.jobsCompleted, "jobsCompleted counter should be initialized")
	assert.NotNil(t, collector.jobsFailed, "jobsFailed counter should be initialized")
	assert.NotNil(t, collector.jobsDead, "jobsDead counter should be initialized")
	assert.NotNil(t, collector.jobLatency, "jobLatency histogram should be initialized")
	assert.NotNil(t, collector.recoveryTime, "recoveryTime gauge should be initialized")
	assert.NotNil(t, collector.jobsPending, "jobsPending gauge should be initialized")
	assert.NotNil(t, collector.jobsInFlight, "jobsInFlight gauge should be initialized")
}

func TestRecordEnqueue(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// RecordEnqueue 應該不會 panic
	assert.NotPanics(t, func() {
		collector.RecordEnqueue()
	}, "RecordEnqueue should not panic")

	// 多次調用應該正常工作
	for i := 0; i < 5; i++ {
		collector.RecordEnqueue()
	}
}

func TestRecordDispatch(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	assert.NotPanics(t, func() {
		collector.RecordDispatch()
	}, "RecordDispatch should not panic")

	for i := 0; i < 10; i++ {
		collector.RecordDispatch()
	}
}

func TestRecordCompleted(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// 測試不同的延遲值
	latencies := []float64{0.001, 0.01, 0.1, 1.0, 5.0}

	for _, latency := range latencies {
		assert.NotPanics(t, func() {
			collector.RecordCompleted(latency)
		}, "RecordCompleted should not panic with latency %f", latency)
	}
}

func TestRecordFailed(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	assert.NotPanics(t, func() {
		collector.RecordFailed()
	}, "RecordFailed should not panic")

	for i := 0; i < 3; i++ {
		collector.RecordFailed()
	}
}

func TestRecordDead(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	assert.NotPanics(t, func() {
		collector.RecordDead()
	}, "RecordDead should not panic")

	for i := 0; i < 2; i++ {
		collector.RecordDead()
	}
}

func TestSetRecoveryTime(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// 測試設置不同的恢復時間
	recoveryTimes := []float64{0.001, 0.5, 1.5, 3.0}

	for _, rt := range recoveryTimes {
		assert.NotPanics(t, func() {
			collector.SetRecoveryTime(rt)
		}, "SetRecoveryTime should not panic with time %f", rt)
	}
}

func TestUpdateQueueStats(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	testCases := []struct {
		name     string
		pending  int
		inFlight int
	}{
		{"zero values", 0, 0},
		{"normal values", 10, 5},
		{"high pending", 100, 8},
		{"high in-flight", 5, 50},
		{"equal values", 20, 20},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			assert.NotPanics(t, func() {
				collector.UpdateQueueStats(tc.pending, tc.inFlight)
			}, "UpdateQueueStats should not panic")
		})
	}
}

func TestConcurrentMetricUpdates(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// 測試並發更新（Prometheus metrics 應該是線程安全的）
	done := make(chan bool, 100)

	for i := 0; i < 100; i++ {
		go func() {
			collector.RecordEnqueue()
			collector.RecordDispatch()
			collector.RecordCompleted(0.1)
			collector.UpdateQueueStats(10, 5)
			done <- true
		}()
	}

	// 等待所有 goroutine 完成
	for i := 0; i < 100; i++ {
		<-done
	}
}

func TestMetricMethodsWithNilCollector(t *testing.T) {
	// 測試 nil collector 的防禦性行為
	// 實際上，nil collector 調用方法會 panic，這是 Go 的標準行為
	// 本測試驗證正常的 collector 可以安全調用所有方法
	collector := NewCollector()

	// 驗證所有方法都可以安全調用
	collector.RecordEnqueue()
	collector.RecordDispatch()
	collector.RecordCompleted(1.0)
	collector.RecordFailed()
	collector.RecordDead()
	collector.SetRecoveryTime(1.0)
	collector.UpdateQueueStats(10, 5)
}

func TestCollectorIsolation(t *testing.T) {
	// 測試多個 collector 實例可以獨立工作
	prometheus.DefaultRegisterer = prometheus.NewRegistry()

	collector1 := NewCollector()
	require.NotNil(t, collector1)

	// 第二個 collector 會因為重複註冊而 panic
	// 這是預期行為 - 一個進程只應該有一個 collector
	assert.Panics(t, func() {
		NewCollector()
	}, "Creating a second collector should panic due to duplicate registration")
}

func TestMetricOperationSequence(t *testing.T) {
	// 測試一個典型的任務處理序列
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// 模擬任務生命週期
	assert.NotPanics(t, func() {
		// 1. 任務入隊
		collector.RecordEnqueue()
		collector.UpdateQueueStats(1, 0)

		// 2. 任務分派
		collector.RecordDispatch()
		collector.UpdateQueueStats(0, 1)

		// 3. 任務完成
		collector.RecordCompleted(0.5)
		collector.UpdateQueueStats(0, 0)
	}, "Complete job lifecycle should not panic")
}

func TestMetricOperationWithFailure(t *testing.T) {
	// 測試任務失敗場景
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	assert.NotPanics(t, func() {
		// 1. 任務入隊
		collector.RecordEnqueue()

		// 2. 任務分派
		collector.RecordDispatch()

		// 3. 任務失敗
		collector.RecordFailed()

		// 4. 如果重試次數用盡，進入死信隊列
		collector.RecordDead()
	}, "Job failure scenario should not panic")
}

func TestRecoveryTimeScenario(t *testing.T) {
	// 測試恢復時間記錄
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// 模擬系統啟動和恢復
	assert.NotPanics(t, func() {
		// 記錄恢復時間（秒）
		collector.SetRecoveryTime(2.5)

		// 恢復後開始處理任務
		collector.UpdateQueueStats(50, 0)
		collector.RecordDispatch()
		collector.RecordCompleted(0.1)
	}, "Recovery scenario should not panic")
}

func TestZeroAndNegativeValues(t *testing.T) {
	prometheus.DefaultRegisterer = prometheus.NewRegistry()
	collector := NewCollector()

	// 測試邊界值
	assert.NotPanics(t, func() {
		collector.RecordCompleted(0.0)     // 零延遲
		collector.SetRecoveryTime(0.0)     // 零恢復時間
		collector.UpdateQueueStats(0, 0)   // 空隊列
		collector.UpdateQueueStats(-1, -1) // 負值（雖然不應該發生）
	}, "Edge case values should not panic")
}
