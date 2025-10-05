package jobmanager

import (
	"errors"
	"fmt"
	"sync"
	"testing"
	"time"

	"github.com/ChuLiYu/beaver-raft/pkg/types"
)

// ============================================================================
// 測試輔助函數
// ============================================================================

// newTestJobManager 建立測試用的 JobManager
func newTestJobManager() *JobManager {
	return NewJobManager()
}

// newTestJob 建立測試用的 Job
func newTestJob(id string) types.Job {
	return types.Job{
		ID:      types.JobID(id),
		Payload: map[string]interface{}{"test": "data"},
		Attempt: 0,
	}
}

// assertNoError 斷言沒有錯誤
func assertNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

// assertError 斷言有特定錯誤
func assertError(t *testing.T, err error, want error) {
	t.Helper()
	if err == nil {
		t.Errorf("expected error %v, got nil", want)
		return
	}
	if !errors.Is(err, want) {
		t.Errorf("expected error %v, got %v", want, err)
	}
}

// assertEqual 斷言兩個值相等
func assertEqual(t *testing.T, got, want interface{}) {
	t.Helper()
	if got != want {
		t.Errorf("got %v, want %v", got, want)
	}
}

// assertJobStatus 斷言任務狀態
func assertJobStatus(t *testing.T, jm *JobManager, jobID types.JobID, want types.JobStatus) {
	t.Helper()
	job, exists := jm.jobs[jobID]
	if !exists {
		t.Errorf("job %s not found", jobID)
		return
	}
	if job.Status != want {
		t.Errorf("job %s status: got %s, want %s", jobID, job.Status, want)
	}
}

// ============================================================================
// 單元測試
// ============================================================================

func TestNewJobManager(t *testing.T) {
	jm := NewJobManager()

	// 驗證所有欄位都已初始化
	if jm.jobs == nil {
		t.Error("jobs map not initialized")
	}
	if jm.queue == nil {
		t.Error("queue slice not initialized")
	}
	if jm.inFlight == nil {
		t.Error("inFlight map not initialized")
	}
	if jm.completed == nil {
		t.Error("completed map not initialized")
	}
	if jm.dead == nil {
		t.Error("dead map not initialized")
	}

	// 驗證初始狀態
	stats := jm.Stats()
	expectedStats := map[string]int{
		"pending":   0,
		"in_flight": 0,
		"completed": 0,
		"dead":      0,
	}
	for key, value := range expectedStats {
		if stats[key] != value {
			t.Errorf("stats[%s]: got %d, want %d", key, stats[key], value)
		}
	}
}

func TestEnqueue(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		job     types.Job
		wantErr error
	}{
		{
			name:    "正常加入單個任務",
			setup:   func(jm *JobManager) {},
			job:     newTestJob("task-001"),
			wantErr: nil,
		},
		{
			name:    "加入多個任務",
			setup:   func(jm *JobManager) { jm.Enqueue(newTestJob("task-001")) },
			job:     newTestJob("task-002"),
			wantErr: nil,
		},
		{
			name:    "重複 ID 錯誤",
			setup:   func(jm *JobManager) { jm.Enqueue(newTestJob("task-001")) },
			job:     newTestJob("task-001"),
			wantErr: ErrDuplicateJob,
		},
		{
			name:    "空 ID 處理",
			setup:   func(jm *JobManager) {},
			job:     newTestJob(""),
			wantErr: nil,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.Enqueue(tt.job)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// 驗證任務已加入
				if _, exists := jm.jobs[tt.job.ID]; !exists {
					t.Errorf("job %s not found in jobs map", tt.job.ID)
				}
				// 驗證任務在 queue 中
				found := false
				for _, id := range jm.queue {
					if id == tt.job.ID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("job %s not found in queue", tt.job.ID)
				}
				// 驗證狀態
				assertJobStatus(t, jm, tt.job.ID, types.StatusPending)
			}
		})
	}
}

func TestPopPending(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		wantJob *types.Job
		wantNil bool
	}{
		{
			name:    "空佇列回傳 nil",
			setup:   func(jm *JobManager) {},
			wantNil: true,
		},
		{
			name: "FIFO 順序正確",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.Enqueue(newTestJob("task-002"))
			},
			wantJob: &types.Job{ID: "task-001"},
		},
		{
			name: "連續 pop 直到空",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			wantJob: &types.Job{ID: "task-001"},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			job := jm.PopPending()

			if tt.wantNil {
				if job != nil {
					t.Errorf("expected nil, got %v", job)
				}
			} else {
				if job == nil {
					t.Error("expected job, got nil")
					return
				}
				if job.ID != tt.wantJob.ID {
					t.Errorf("got job ID %s, want %s", job.ID, tt.wantJob.ID)
				}
				// 驗證 queue 已更新
				if len(jm.queue) == 0 && tt.name == "FIFO 順序正確" {
					t.Error("queue should not be empty after first pop")
				}
			}
		})
	}
}

func TestMarkInFlight(t *testing.T) {
	tests := []struct {
		name     string
		setup    func(*JobManager)
		jobID    types.JobID
		deadline time.Time
		wantErr  error
	}{
		{
			name: "正常標記為執行中",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:    "task-001",
			deadline: time.Now().Add(time.Minute),
			wantErr:  nil,
		},
		{
			name:     "任務不存在錯誤",
			setup:    func(jm *JobManager) {},
			jobID:    "task-001",
			deadline: time.Now().Add(time.Minute),
			wantErr:  ErrJobNotFound,
		},
		{
			name: "任務狀態不是 Pending 錯誤",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:    "task-001",
			deadline: time.Now().Add(time.Minute),
			wantErr:  fmt.Errorf("job not in pending status"),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.MarkInFlight(tt.jobID, tt.deadline)

			if tt.wantErr != nil {
				if err == nil {
					t.Errorf("expected error %v, got nil", tt.wantErr)
				} else if err.Error() != tt.wantErr.Error() {
					t.Errorf("expected error %v, got %v", tt.wantErr, err)
				}
			} else {
				assertNoError(t, err)
				// 驗證 inFlight 集合正確更新
				if _, exists := jm.inFlight[tt.jobID]; !exists {
					t.Errorf("job %s not found in inFlight", tt.jobID)
				}
				// 驗證 Deadline 設定正確
				job := jm.jobs[tt.jobID]
				if job.Deadline == nil {
					t.Error("deadline not set")
				} else if *job.Deadline != tt.deadline.UnixMilli() {
					t.Errorf("deadline: got %d, want %d", *job.Deadline, tt.deadline.UnixMilli())
				}
				// 驗證狀態
				assertJobStatus(t, jm, tt.jobID, types.StatusInFlight)
			}
		})
	}
}

func TestMarkCompleted(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantErr error
	}{
		{
			name: "正常完成",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:   "task-001",
			wantErr: nil,
		},
		{
			name:    "任務不存在錯誤",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantErr: ErrJobNotFound,
		},
		{
			name: "任務不在執行中錯誤",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantErr: ErrNotInFlight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.MarkCompleted(tt.jobID)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// 驗證從 inFlight 移除
				if _, exists := jm.inFlight[tt.jobID]; exists {
					t.Errorf("job %s still in inFlight", tt.jobID)
				}
				// 驗證加入 completed
				if _, exists := jm.completed[tt.jobID]; !exists {
					t.Errorf("job %s not found in completed", tt.jobID)
				}
				// 驗證狀態
				assertJobStatus(t, jm, tt.jobID, types.StatusCompleted)
			}
		})
	}
}

func TestRequeue(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantErr error
	}{
		{
			name: "正常重新排隊",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:   "task-001",
			wantErr: nil,
		},
		{
			name:    "任務不存在錯誤",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantErr: ErrJobNotFound,
		},
		{
			name: "任務不在執行中錯誤",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantErr: ErrNotInFlight,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			originalAttempt := 0
			if job, exists := jm.jobs[tt.jobID]; exists {
				originalAttempt = job.Attempt
			}
			err := jm.Requeue(tt.jobID)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// 驗證 Attempt 正確增加
				newAttempt := jm.jobs[tt.jobID].Attempt
				if newAttempt != originalAttempt+1 {
					t.Errorf("attempt: got %d, want %d", newAttempt, originalAttempt+1)
				}
				// 驗證重新加入 queue 尾端
				if len(jm.queue) == 0 {
					t.Error("queue should not be empty after requeue")
				}
				lastJobID := jm.queue[len(jm.queue)-1]
				if lastJobID != tt.jobID {
					t.Errorf("last job in queue: got %s, want %s", lastJobID, tt.jobID)
				}
				// 驗證狀態
				assertJobStatus(t, jm, tt.jobID, types.StatusPending)
			}
		})
	}
}

func TestMarkDead(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantErr error
	}{
		{
			name: "正常標記為死信",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantErr: nil,
		},
		{
			name:    "任務不存在錯誤",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantErr: ErrJobNotFound,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			err := jm.MarkDead(tt.jobID)

			if tt.wantErr != nil {
				assertError(t, err, tt.wantErr)
			} else {
				assertNoError(t, err)
				// 驗證加入 dead 集合
				if _, exists := jm.dead[tt.jobID]; !exists {
					t.Errorf("job %s not found in dead", tt.jobID)
				}
				// 驗證狀態更新正確
				assertJobStatus(t, jm, tt.jobID, types.StatusDead)
			}
		})
	}
}

func TestGetExpiredJobs(t *testing.T) {
	now := time.Now()
	past := now.Add(-time.Minute)
	future := now.Add(time.Minute)

	tests := []struct {
		name        string
		setup       func(*JobManager)
		now         time.Time
		wantExpired []types.JobID
	}{
		{
			name:        "無過期任務",
			setup:       func(jm *JobManager) {},
			now:         now,
			wantExpired: []types.JobID{},
		},
		{
			name: "單個過期任務",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", past)
			},
			now:         now,
			wantExpired: []types.JobID{"task-001"},
		},
		{
			name: "多個過期任務",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.PopPending()
				jm.MarkInFlight("task-001", past)
				jm.MarkInFlight("task-002", past)
			},
			now:         now,
			wantExpired: []types.JobID{"task-001", "task-002"},
		},
		{
			name: "混合過期和未過期",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.PopPending()
				jm.MarkInFlight("task-001", past)
				jm.MarkInFlight("task-002", future)
			},
			now:         now,
			wantExpired: []types.JobID{"task-001"},
		},
		{
			name: "邊界條件（deadline 剛好等於 now）",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", now)
			},
			now:         now,
			wantExpired: []types.JobID{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			expired := jm.GetExpiredJobs(tt.now)

			if len(expired) != len(tt.wantExpired) {
				t.Errorf("expired count: got %d, want %d", len(expired), len(tt.wantExpired))
				return
			}

			// 檢查是否包含所有期望的過期任務
			for _, wantJobID := range tt.wantExpired {
				found := false
				for _, gotJobID := range expired {
					if gotJobID == wantJobID {
						found = true
						break
					}
				}
				if !found {
					t.Errorf("expected expired job %s not found", wantJobID)
				}
			}
		})
	}
}

func TestStats(t *testing.T) {
	tests := []struct {
		name      string
		setup     func(*JobManager)
		wantStats map[string]int
	}{
		{
			name:      "空狀態統計",
			setup:     func(jm *JobManager) {},
			wantStats: map[string]int{"pending": 0, "in_flight": 0, "completed": 0, "dead": 0},
		},
		{
			name: "各狀態都有任務",
			setup: func(jm *JobManager) {
				// 建立一個更簡單的測試場景
				// InFlight
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))

				// Completed
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.MarkInFlight("task-002", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-002")

				// Dead
				jm.Enqueue(newTestJob("task-003"))
				jm.PopPending() // 先從 queue 移除
				jm.MarkDead("task-003")
			},
			wantStats: map[string]int{"pending": 0, "in_flight": 1, "completed": 1, "dead": 1},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			stats := jm.Stats()

			for key, wantValue := range tt.wantStats {
				if stats[key] != wantValue {
					t.Errorf("stats[%s]: got %d, want %d", key, stats[key], wantValue)
				}
			}
		})
	}
}

// ============================================================================
// 整合測試
// ============================================================================

func TestJobLifecycle(t *testing.T) {
	jm := newTestJobManager()

	// 測試完整生命週期：Enqueue -> PopPending -> MarkInFlight -> MarkCompleted
	job := newTestJob("task-001")

	// Enqueue
	err := jm.Enqueue(job)
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusPending)

	// PopPending
	poppedJob := jm.PopPending()
	if poppedJob == nil || poppedJob.ID != "task-001" {
		t.Error("PopPending failed")
	}

	// MarkInFlight
	deadline := time.Now().Add(time.Minute)
	err = jm.MarkInFlight("task-001", deadline)
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusInFlight)

	// MarkCompleted
	err = jm.MarkCompleted("task-001")
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusCompleted)

	// 驗證最終狀態
	stats := jm.Stats()
	if stats["completed"] != 1 {
		t.Errorf("expected 1 completed job, got %d", stats["completed"])
	}
}

func TestJobLifecycleWithRetry(t *testing.T) {
	jm := newTestJobManager()

	// 測試重試生命週期：Enqueue -> PopPending -> MarkInFlight -> Requeue -> MarkInFlight -> MarkDead
	job := newTestJob("task-001")

	// Enqueue
	err := jm.Enqueue(job)
	assertNoError(t, err)

	// PopPending
	poppedJob := jm.PopPending()
	if poppedJob == nil || poppedJob.ID != "task-001" {
		t.Error("PopPending failed")
	}

	// MarkInFlight
	deadline := time.Now().Add(time.Minute)
	err = jm.MarkInFlight("task-001", deadline)
	assertNoError(t, err)

	// Requeue (模擬失敗重試)
	err = jm.Requeue("task-001")
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusPending)

	// 再次 MarkInFlight
	err = jm.MarkInFlight("task-001", deadline)
	assertNoError(t, err)

	// MarkDead (模擬超過重試次數)
	err = jm.MarkDead("task-001")
	assertNoError(t, err)
	assertJobStatus(t, jm, "task-001", types.StatusDead)

	// 驗證最終狀態
	stats := jm.Stats()
	if stats["dead"] != 1 {
		t.Errorf("expected 1 dead job, got %d", stats["dead"])
	}
}

func TestStateInvariants(t *testing.T) {
	jm := newTestJobManager()

	// 添加多個任務並進行各種操作
	jm.Enqueue(newTestJob("task-001"))
	jm.Enqueue(newTestJob("task-002"))
	jm.Enqueue(newTestJob("task-003"))

	jm.PopPending()
	jm.MarkInFlight("task-001", time.Now().Add(time.Minute))

	jm.PopPending()
	jm.MarkInFlight("task-002", time.Now().Add(time.Minute))
	jm.MarkCompleted("task-002")

	jm.MarkDead("task-003")

	// 驗證不變性：每個任務只存在於一個集合
	allJobIDs := make(map[types.JobID]bool)

	// 收集所有任務 ID
	for jobID := range jm.jobs {
		allJobIDs[jobID] = true
	}

	// 驗證 jobs map 包含所有任務
	if len(allJobIDs) != 3 {
		t.Errorf("expected 3 jobs in jobs map, got %d", len(allJobIDs))
	}

	// 驗證狀態轉換一致性
	// 注意：PopPending 會從 queue 中移除任務，但任務仍在 jobs map 中
	// 所以我們需要檢查實際的任務狀態
	actualTotal := 0
	for _, job := range jm.jobs {
		switch job.Status {
		case types.StatusPending:
			actualTotal++
		case types.StatusInFlight:
			actualTotal++
		case types.StatusCompleted:
			actualTotal++
		case types.StatusDead:
			actualTotal++
		}
	}
	if actualTotal != len(allJobIDs) {
		t.Errorf("actual total jobs (%d) != jobs map size (%d)", actualTotal, len(allJobIDs))
	}
}

// ============================================================================
// 併發測試
// ============================================================================

func TestConcurrentEnqueue(t *testing.T) {
	jm := newTestJobManager()

	const numGoroutines = 10
	const jobsPerGoroutine = 100

	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*jobsPerGoroutine)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < jobsPerGoroutine; j++ {
				jobID := types.JobID(fmt.Sprintf("task-%d-%d", goroutineID, j))
				job := types.Job{ID: jobID, Payload: map[string]interface{}{"goroutine": goroutineID}}
				if err := jm.Enqueue(job); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 檢查是否有錯誤
	for err := range errors {
		t.Errorf("concurrent enqueue error: %v", err)
	}

	// 驗證所有任務都已加入
	stats := jm.Stats()
	expectedTotal := numGoroutines * jobsPerGoroutine
	if stats["pending"] != expectedTotal {
		t.Errorf("expected %d pending jobs, got %d", expectedTotal, stats["pending"])
	}
}

func TestConcurrentOperations(t *testing.T) {
	jm := newTestJobManager()

	const numGoroutines = 5
	var wg sync.WaitGroup
	errors := make(chan error, numGoroutines*10)

	for i := 0; i < numGoroutines; i++ {
		wg.Add(1)
		go func(goroutineID int) {
			defer wg.Done()
			for j := 0; j < 10; j++ {
				jobID := types.JobID(fmt.Sprintf("task-%d-%d", goroutineID, j))
				job := types.Job{ID: jobID, Payload: map[string]interface{}{"goroutine": goroutineID}}

				// Enqueue
				if err := jm.Enqueue(job); err != nil {
					errors <- err
					continue
				}

				// PopPending
				poppedJob := jm.PopPending()
				if poppedJob == nil {
					errors <- fmt.Errorf("PopPending returned nil")
					continue
				}

				// MarkInFlight
				if err := jm.MarkInFlight(poppedJob.ID, time.Now().Add(time.Minute)); err != nil {
					errors <- err
					continue
				}

				// MarkCompleted
				if err := jm.MarkCompleted(poppedJob.ID); err != nil {
					errors <- err
				}
			}
		}(i)
	}

	wg.Wait()
	close(errors)

	// 檢查是否有錯誤
	for err := range errors {
		t.Errorf("concurrent operation error: %v", err)
	}

	// 驗證資料一致性
	stats := jm.Stats()
	if stats["pending"] != 0 {
		t.Errorf("expected 0 pending jobs, got %d", stats["pending"])
	}
	if stats["completed"] != numGoroutines*10 {
		t.Errorf("expected %d completed jobs, got %d", numGoroutines*10, stats["completed"])
	}
}

// ============================================================================
// 效能測試（Benchmarks）
// ============================================================================

func BenchmarkEnqueue(b *testing.B) {
	jm := NewJobManager()
	job := newTestJob("benchmark-job")

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		job.ID = types.JobID(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
	}
}

func BenchmarkPopPending(b *testing.B) {
	jm := NewJobManager()

	// 預先填充
	for i := 0; i < b.N; i++ {
		job := newTestJob(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jm.PopPending()
	}
}

func BenchmarkMarkInFlight(b *testing.B) {
	jm := NewJobManager()
	deadline := time.Now().Add(time.Minute)

	// 預先填充
	for i := 0; i < b.N; i++ {
		job := newTestJob(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
		jm.PopPending()
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jm.MarkInFlight(types.JobID(fmt.Sprintf("job-%d", i)), deadline)
	}
}

func BenchmarkConcurrentEnqueue(b *testing.B) {
	jm := NewJobManager()

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			job := newTestJob(fmt.Sprintf("job-%d", i))
			jm.Enqueue(job)
			i++
		}
	})
}

func BenchmarkConcurrentMixed(b *testing.B) {
	jm := NewJobManager()
	deadline := time.Now().Add(time.Minute)

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			jobID := types.JobID(fmt.Sprintf("job-%d", i))
			job := newTestJob(string(jobID))

			// 混合操作
			jm.Enqueue(job)
			if poppedJob := jm.PopPending(); poppedJob != nil {
				jm.MarkInFlight(poppedJob.ID, deadline)
				jm.MarkCompleted(poppedJob.ID)
			}
			i++
		}
	})
}

func BenchmarkStats(b *testing.B) {
	jm := NewJobManager()

	// 預先填充
	for i := 0; i < 1000; i++ {
		job := newTestJob(fmt.Sprintf("job-%d", i))
		jm.Enqueue(job)
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		jm.Stats()
	}
}

// ============================================================================
// 新增方法測試（Snapshot, Restore, IsCompleted, IsDead, GetJob）
// ============================================================================

func TestSnapshot(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*JobManager)
		want  func(types.SnapshotData) bool
	}{
		{
			name:  "空狀態快照",
			setup: func(jm *JobManager) {},
			want: func(data types.SnapshotData) bool {
				return len(data.Jobs) == 0 && data.SchemaVer == 1
			},
		},
		{
			name: "包含各種狀態的快照",
			setup: func(jm *JobManager) {
				// Pending
				jm.Enqueue(newTestJob("task-001"))

				// InFlight
				jm.Enqueue(newTestJob("task-002"))
				jm.PopPending()
				jm.MarkInFlight("task-002", time.Now().Add(time.Minute))

				// Completed
				jm.Enqueue(newTestJob("task-003"))
				jm.PopPending()
				jm.MarkInFlight("task-003", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-003")

				// Dead
				jm.Enqueue(newTestJob("task-004"))
				jm.MarkDead("task-004")
			},
			want: func(data types.SnapshotData) bool {
				if len(data.Jobs) != 4 {
					return false
				}
				if data.SchemaVer != 1 {
					return false
				}
				// 驗證每個任務的狀態
				if data.Jobs["task-001"].Status != types.StatusPending {
					return false
				}
				if data.Jobs["task-002"].Status != types.StatusInFlight {
					return false
				}
				if data.Jobs["task-003"].Status != types.StatusCompleted {
					return false
				}
				if data.Jobs["task-004"].Status != types.StatusDead {
					return false
				}
				return true
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			data := jm.Snapshot()

			if !tt.want(data) {
				t.Errorf("Snapshot validation failed")
			}
		})
	}
}

func TestRestore(t *testing.T) {
	tests := []struct {
		name    string
		data    types.SnapshotData
		wantErr bool
		verify  func(*testing.T, *JobManager)
	}{
		{
			name: "恢復空快照",
			data: types.SnapshotData{
				Jobs:      make(map[types.JobID]*types.Job),
				SchemaVer: 1,
			},
			wantErr: false,
			verify: func(t *testing.T, jm *JobManager) {
				stats := jm.Stats()
				if stats["pending"] != 0 || stats["in_flight"] != 0 ||
					stats["completed"] != 0 || stats["dead"] != 0 {
					t.Errorf("expected empty state after restore, got %v", stats)
				}
			},
		},
		{
			name: "恢復包含各種狀態的快照",
			data: types.SnapshotData{
				Jobs: map[types.JobID]*types.Job{
					"task-001": {
						ID:      "task-001",
						Status:  types.StatusPending,
						Payload: map[string]interface{}{"test": "data"},
					},
					"task-002": {
						ID:      "task-002",
						Status:  types.StatusInFlight,
						Payload: map[string]interface{}{"test": "data"},
					},
					"task-003": {
						ID:      "task-003",
						Status:  types.StatusCompleted,
						Payload: map[string]interface{}{"test": "data"},
					},
					"task-004": {
						ID:      "task-004",
						Status:  types.StatusDead,
						Payload: map[string]interface{}{"test": "data"},
					},
				},
				SchemaVer: 1,
			},
			wantErr: false,
			verify: func(t *testing.T, jm *JobManager) {
				stats := jm.Stats()
				if stats["pending"] != 1 {
					t.Errorf("expected 1 pending job, got %d", stats["pending"])
				}
				if stats["in_flight"] != 1 {
					t.Errorf("expected 1 in_flight job, got %d", stats["in_flight"])
				}
				if stats["completed"] != 1 {
					t.Errorf("expected 1 completed job, got %d", stats["completed"])
				}
				if stats["dead"] != 1 {
					t.Errorf("expected 1 dead job, got %d", stats["dead"])
				}

				// 驗證每個任務的狀態
				if job := jm.GetJob("task-001"); job == nil || job.Status != types.StatusPending {
					t.Error("task-001 status incorrect")
				}
				if job := jm.GetJob("task-002"); job == nil || job.Status != types.StatusInFlight {
					t.Error("task-002 status incorrect")
				}
				if job := jm.GetJob("task-003"); job == nil || job.Status != types.StatusCompleted {
					t.Error("task-003 status incorrect")
				}
				if job := jm.GetJob("task-004"); job == nil || job.Status != types.StatusDead {
					t.Error("task-004 status incorrect")
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()

			err := jm.Restore(tt.data)

			if (err != nil) != tt.wantErr {
				t.Errorf("Restore() error = %v, wantErr %v", err, tt.wantErr)
				return
			}

			if tt.verify != nil {
				tt.verify(t, jm)
			}
		})
	}
}

func TestSnapshotAndRestore(t *testing.T) {
	// 創建第一個 JobManager 並添加數據
	jm1 := newTestJobManager()

	// 添加各種狀態的任務（確保狀態一致）
	// Pending
	jm1.Enqueue(newTestJob("task-001"))

	// InFlight
	jm1.Enqueue(newTestJob("task-002"))
	jm1.PopPending()
	jm1.MarkInFlight("task-002", time.Now().Add(time.Minute))

	// Completed
	jm1.Enqueue(newTestJob("task-003"))
	jm1.PopPending()
	jm1.MarkInFlight("task-003", time.Now().Add(time.Minute))
	jm1.MarkCompleted("task-003")

	// Dead
	jm1.Enqueue(newTestJob("task-004"))
	jm1.PopPending() // 先從 queue 移除
	jm1.MarkDead("task-004")

	// 生成快照
	snapshot := jm1.Snapshot()

	// 創建第二個 JobManager 並恢復
	jm2 := newTestJobManager()
	err := jm2.Restore(snapshot)
	assertNoError(t, err)

	// 比較兩個 JobManager 的狀態
	stats1 := jm1.Stats()
	stats2 := jm2.Stats()

	for key, value := range stats1 {
		if stats2[key] != value {
			t.Errorf("stats[%s]: jm1=%d, jm2=%d", key, value, stats2[key])
		}
	}

	// 驗證每個任務的詳細資訊
	for jobID, job1 := range jm1.jobs {
		job2 := jm2.GetJob(jobID)
		if job2 == nil {
			t.Errorf("job %s not found in jm2", jobID)
			continue
		}
		if job1.Status != job2.Status {
			t.Errorf("job %s status: jm1=%s, jm2=%s", jobID, job1.Status, job2.Status)
		}
		if job1.Attempt != job2.Attempt {
			t.Errorf("job %s attempt: jm1=%d, jm2=%d", jobID, job1.Attempt, job2.Attempt)
		}
	}
}

func TestIsCompleted(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*JobManager)
		jobID types.JobID
		want  bool
	}{
		{
			name:  "任務不存在",
			setup: func(jm *JobManager) {},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 Pending 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 InFlight 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 Completed 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-001")
			},
			jobID: "task-001",
			want:  true,
		},
		{
			name: "任務處於 Dead 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.MarkDead("task-001")
			},
			jobID: "task-001",
			want:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			got := jm.IsCompleted(tt.jobID)

			if got != tt.want {
				t.Errorf("IsCompleted() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestIsDead(t *testing.T) {
	tests := []struct {
		name  string
		setup func(*JobManager)
		jobID types.JobID
		want  bool
	}{
		{
			name:  "任務不存在",
			setup: func(jm *JobManager) {},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 Pending 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 InFlight 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 Completed 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-001")
			},
			jobID: "task-001",
			want:  false,
		},
		{
			name: "任務處於 Dead 狀態",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.MarkDead("task-001")
			},
			jobID: "task-001",
			want:  true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			got := jm.IsDead(tt.jobID)

			if got != tt.want {
				t.Errorf("IsDead() = %v, want %v", got, tt.want)
			}
		})
	}
}

func TestGetJob(t *testing.T) {
	tests := []struct {
		name    string
		setup   func(*JobManager)
		jobID   types.JobID
		wantNil bool
		verify  func(*testing.T, *types.Job)
	}{
		{
			name:    "任務不存在",
			setup:   func(jm *JobManager) {},
			jobID:   "task-001",
			wantNil: true,
		},
		{
			name: "取得 Pending 任務",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusPending {
					t.Errorf("expected status Pending, got %s", job.Status)
				}
			},
		},
		{
			name: "取得 InFlight 任務",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusInFlight {
					t.Errorf("expected status InFlight, got %s", job.Status)
				}
				if job.Deadline == nil {
					t.Error("expected Deadline to be set")
				}
			},
		},
		{
			name: "取得 Completed 任務",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.PopPending()
				jm.MarkInFlight("task-001", time.Now().Add(time.Minute))
				jm.MarkCompleted("task-001")
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusCompleted {
					t.Errorf("expected status Completed, got %s", job.Status)
				}
			},
		},
		{
			name: "取得 Dead 任務",
			setup: func(jm *JobManager) {
				jm.Enqueue(newTestJob("task-001"))
				jm.MarkDead("task-001")
			},
			jobID:   "task-001",
			wantNil: false,
			verify: func(t *testing.T, job *types.Job) {
				if job.Status != types.StatusDead {
					t.Errorf("expected status Dead, got %s", job.Status)
				}
			},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			jm := newTestJobManager()
			tt.setup(jm)

			job := jm.GetJob(tt.jobID)

			if tt.wantNil {
				if job != nil {
					t.Errorf("expected nil, got %v", job)
				}
			} else {
				if job == nil {
					t.Error("expected job, got nil")
					return
				}
				if tt.verify != nil {
					tt.verify(t, job)
				}
			}
		})
	}
}
