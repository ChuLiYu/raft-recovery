// ============================================================================
// Beaver-Raft Metrics - Prometheus Monitoring
// ============================================================================
//
// Package: internal/metrics
// File: metrics.go
// Purpose: Collect and expose system metrics for Prometheus monitoring
//
// Monitoring Philosophy:
//   Based on RED (Rate, Errors, Duration) and USE (Utilization, Saturation, Errors)
//   Provides comprehensive system observability
//
// Metric Categories:
//
//   1. Job Counters - Cumulative, monotonically increasing:
//      - jobs_enqueued_total: Total enqueued jobs
//      - jobs_dispatched_total: Total dispatched jobs
//      - jobs_completed_total: Total completed jobs
//      - jobs_failed_total: Total failed jobs
//      - jobs_dead_total: Total dead letter jobs
//
//   2. Performance Metrics (Histogram) - Distribution stats:
//      - job_latency_seconds: Job processing latency distribution
//        * Buckets: 0.005, 0.01, 0.025, 0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10
//        * For SLA monitoring and performance analysis
//
//   3. Status Metrics (Gauge) - Instantaneous values:
//      - recovery_time_seconds: Last recovery time
//      - jobs_pending: Current pending jobs
//      - jobs_in_flight: Current executing jobs
//
// Use Cases:
//
//   Alerting:
//   - job_latency_seconds > 5s  → Performance degradation
//   - jobs_failed_total rate increase → Error rate alert
//   - jobs_pending continuous growth → Insufficient capacity
//   - recovery_time_seconds > 3s → Recovery SLA breach
//
//   Capacity Planning:
//   - jobs_completed_total / time → Throughput trends
//   - jobs_in_flight / worker_count → Worker utilization
//   - jobs_pending peaks → Required worker count
//
//   Troubleshooting:
//   - jobs_dead_total spike → Check business logic
//   - job_latency anomaly → Check system load
//   - recovery_time increase → Check WAL/Snapshot performance
//
// Prometheus Query Examples:
//
//   # Jobs per minute
//   rate(jobs_completed_total[1m])
//
//   # 95th percentile latency
//   histogram_quantile(0.95, job_latency_seconds_bucket)
//
//   # Error rate
//   rate(jobs_failed_total[5m]) / rate(jobs_dispatched_total[5m])
//
//   # Job backlog
//   jobs_pending + jobs_in_flight
//
// HTTP Endpoint:
//   Exposed via /metrics endpoint, scraped by Prometheus
//   Default port: 9090
//   Format: OpenMetrics / Prometheus text format
//
// Performance:
//   - Counter/Gauge operations are atomic, thread-safe
//   - Histogram calculates multiple buckets with overhead
//   - All metrics protected by sync.Mutex
//
// Future Extensions:
//   Possible additional metrics:
//   - WAL write latency
//   - Snapshot size and creation time
//   - Worker pool saturation
//   - Memory usage
//
// ============================================================================
// Metrics Module
// Responsibility: Collect and expose Prometheus metrics
// ============================================================================

package metrics

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

// Collector collects Prometheus metrics
type Collector struct {
	// Job-related metrics
	jobsEnqueued   prometheus.Counter
	jobsDispatched prometheus.Counter
	jobsCompleted  prometheus.Counter
	jobsFailed     prometheus.Counter
	jobsDead       prometheus.Counter

	// Performance metrics
	jobLatency   prometheus.Histogram
	recoveryTime prometheus.Gauge

	// Status metrics
	jobsPending  prometheus.Gauge
	jobsInFlight prometheus.Gauge

	mu sync.Mutex
}

// NewCollector creates a new metrics collector
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

	// Register all metrics
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

// RecordEnqueue records job enqueue event
func (c *Collector) RecordEnqueue() {
	c.jobsEnqueued.Inc()
}

// RecordDispatch records job dispatch event
func (c *Collector) RecordDispatch() {
	c.jobsDispatched.Inc()
}

// RecordCompleted records job completion with latency
func (c *Collector) RecordCompleted(latencySeconds float64) {
	c.jobsCompleted.Inc()
	c.jobLatency.Observe(latencySeconds)
}

// RecordFailed records job failure event
func (c *Collector) RecordFailed() {
	c.jobsFailed.Inc()
}

// RecordDead records job moved to dead letter queue
func (c *Collector) RecordDead() {
	c.jobsDead.Inc()
}

// SetRecoveryTime sets recovery time metric
func (c *Collector) SetRecoveryTime(seconds float64) {
	c.recoveryTime.Set(seconds)
}

// UpdateQueueStats updates queue status statistics
func (c *Collector) UpdateQueueStats(pending, inFlight int) {
	c.jobsPending.Set(float64(pending))
	c.jobsInFlight.Set(float64(inFlight))
}

// StartServer starts Prometheus metrics HTTP server
//
// Parameters:
//   - port: HTTP server port
//
// Returns:
//   - error: Error on startup failure
func StartServer(port int) error {
	http.Handle("/metrics", promhttp.Handler())
	addr := fmt.Sprintf(":%d", port)
	return http.ListenAndServe(addr, nil)
}
