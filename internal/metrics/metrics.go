// ============================================================================
// Beaver-Raft Metrics - Prometheus 監控指標
// ============================================================================
//
// Package: internal/metrics
// 文件: metrics.go
// 功能: 收集和暴露系統運行指標，支持 Prometheus 監控
//
// 監控理念:
//   基於 RED 方法（Rate, Errors, Duration）和 USE 方法（Utilization, Saturation, Errors）
//   提供全面的系統可觀測性
//
// 指標分類:
//
//   1. 任務計數器 (Counter) - 累計值，只增不減：
//      - jobs_enqueued_total: 入隊任務總數
//      - jobs_dispatched_total: 已分派任務總數
//      - jobs_completed_total: 已完成任務總數
//      - jobs_failed_total: 失敗任務總數
//      - jobs_dead_total: 死信任務總數
//
//   2. 性能指標 (Histogram) - 分佈統計：
//      - job_latency_seconds: 任務處理延遲分佈
//        * 桶分佈: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
//        * 用於 SLA 監控和性能分析
//
//   3. 狀態指標 (Gauge) - 瞬時值：
//      - recovery_time_seconds: 最近一次恢復時間
//      - jobs_pending: 當前待處理任務數
//      - jobs_in_flight: 當前執行中任務數
//
// 使用場景:
//
//   監控告警:
//   - job_latency_seconds > 5s  → 性能下降告警
//   - jobs_failed_total 增長率 → 錯誤率告警
//   - jobs_pending 持續增長 → 處理能力不足
//   - recovery_time_seconds > 3s → 恢復時間超標
//
//   容量規劃:
//   - jobs_completed_total / time → 吞吐量趨勢
//   - jobs_in_flight / worker_count → Worker 利用率
//   - jobs_pending 峰值 → 需要的 Worker 數量
//
//   故障排查:
//   - jobs_dead_total 突增 → 檢查業務邏輯
//   - job_latency 異常 → 檢查系統負載
//   - recovery_time 增長 → 檢查 WAL/Snapshot 性能
//
// Prometheus 查詢示例:
//
//   # 每分鐘完成任務數
//   rate(jobs_completed_total[1m])
//
//   # 95 分位延遲
//   histogram_quantile(0.95, job_latency_seconds_bucket)
//
//   # 錯誤率
//   rate(jobs_failed_total[5m]) / rate(jobs_dispatched_total[5m])
//
//   # 任務積壓
//   jobs_pending + jobs_in_flight
//
// HTTP 端點:
//   通過 /metrics 端點暴露，由 Prometheus 定期抓取
//   默認端口: 9090
//   格式: OpenMetrics / Prometheus 文本格式
//
// 性能考慮:
//   - Counter/Gauge 操作是原子的，線程安全
//   - Histogram 會計算多個桶，有一定開銷
//   - 所有指標都使用 sync.Mutex 保護
//
// 擴展建議:
//   未來可添加的指標：
//   - WAL 寫入延遲
//   - Snapshot 大小和創建時間
//   - Worker 池飽和度
//   - 內存使用量
//
// ============================================================================
// Metrics 監控模組
// 職責：收集並暴露 Prometheus 指標
// ============================================================================

package metrics

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector Prometheus 指標收集器
type Collector struct {
	// 任務相關指標
	jobsEnqueued   prometheus.Counter
	jobsDispatched prometheus.Counter
	jobsCompleted  prometheus.Counter
	jobsFailed     prometheus.Counter
	jobsDead       prometheus.Counter

	// 效能指標
	jobLatency   prometheus.Histogram
	recoveryTime prometheus.Gauge

	// 狀態指標
	jobsPending  prometheus.Gauge
	jobsInFlight prometheus.Gauge

	mu sync.Mutex
}

// NewCollector 創建新的指標收集器
func NewCollector() *Collector {
	c := &Collector{
		jobsEnqueued: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_jobs_enqueued_total",
			Help: "Total number of jobs enqueued",
		}),
		jobsDispatched: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_jobs_dispatched_total",
			Help: "Total number of jobs dispatched to workers",
		}),
		jobsCompleted: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_jobs_completed_total",
			Help: "Total number of jobs completed successfully",
		}),
		jobsFailed: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_jobs_failed_total",
			Help: "Total number of jobs failed",
		}),
		jobsDead: prometheus.NewCounter(prometheus.CounterOpts{
			Name: "queue_jobs_dead_total",
			Help: "Total number of jobs moved to dead letter queue",
		}),
		jobLatency: prometheus.NewHistogram(prometheus.HistogramOpts{
			Name:    "queue_job_latency_seconds",
			Help:    "Job processing latency in seconds",
			Buckets: prometheus.DefBuckets,
		}),
		recoveryTime: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "queue_recovery_time_seconds",
			Help: "Time taken to recover from crash in seconds",
		}),
		jobsPending: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "queue_jobs_pending",
			Help: "Current number of pending jobs",
		}),
		jobsInFlight: prometheus.NewGauge(prometheus.GaugeOpts{
			Name: "queue_jobs_in_flight",
			Help: "Current number of in-flight jobs",
		}),
	}

	// 註冊所有指標
	prometheus.MustRegister(c.jobsEnqueued)
	prometheus.MustRegister(c.jobsDispatched)
	prometheus.MustRegister(c.jobsCompleted)
	prometheus.MustRegister(c.jobsFailed)
	prometheus.MustRegister(c.jobsDead)
	prometheus.MustRegister(c.jobLatency)
	prometheus.MustRegister(c.recoveryTime)
	prometheus.MustRegister(c.jobsPending)
	prometheus.MustRegister(c.jobsInFlight)

	return c
}

// RecordEnqueue 記錄任務加入佇列
func (c *Collector) RecordEnqueue() {
	c.jobsEnqueued.Inc()
}

// RecordDispatch 記錄任務分派
func (c *Collector) RecordDispatch() {
	c.jobsDispatched.Inc()
}

// RecordCompleted 記錄任務完成
func (c *Collector) RecordCompleted(latencySeconds float64) {
	c.jobsCompleted.Inc()
	c.jobLatency.Observe(latencySeconds)
}

// RecordFailed 記錄任務失敗
func (c *Collector) RecordFailed() {
	c.jobsFailed.Inc()
}

// RecordDead 記錄任務進入死信隊列
func (c *Collector) RecordDead() {
	c.jobsDead.Inc()
}

// SetRecoveryTime 設置恢復時間
func (c *Collector) SetRecoveryTime(seconds float64) {
	c.recoveryTime.Set(seconds)
}

// UpdateQueueStats 更新佇列狀態統計
func (c *Collector) UpdateQueueStats(pending, inFlight int) {
	c.jobsPending.Set(float64(pending))
	c.jobsInFlight.Set(float64(inFlight))
}

// StartServer 啟動 Prometheus metrics HTTP 伺服器
//
// 參數：
//   - port: HTTP 伺服器端口
//
// 返回值：
//   - error: 啟動失敗的錯誤
func StartServer(port int) error {
	http.Handle("/metrics", promhttp.Handler())
	addr := fmt.Sprintf(":%d", port)
	return http.ListenAndServe(addr, nil)
}
