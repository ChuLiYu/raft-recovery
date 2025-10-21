// ============================================================================
// Beaver-Raft 任務管理器 - 任務狀態機實現
// ============================================================================
//
// Package: internal/jobmanager
// 文件: job_manager.go
// 功能: 管理任務的完整生命週期和狀態轉換
//
// 設計理念:
//   採用混合式設計，兼顧性能和一致性：
//   1. jobs map - 統一的任務存儲，作為單一真實來源 (Single Source of Truth)
//   2. 狀態索引 - pending queue、inFlight/completed/dead maps 提供快速查詢
//   3. 兩者通過指針同步，確保狀態一致性
//
// 任務狀態轉換 (State Machine):
//   Pending (待處理)
//      ↓ PopPending() + MarkInFlight()
//   InFlight (執行中)
//      ↓ MarkCompleted() 或超時後 Requeue()
//   Completed (已完成) / Dead (死信)
//
// 狀態轉換規則:
//   - Pending → InFlight: 通過 PopPending() + MarkInFlight()
//   - InFlight → Completed: 通過 MarkCompleted()
//   - InFlight → Pending: 通過 Requeue() (失敗重試)
//   - InFlight → Dead: 通過 MarkDead() (超過最大重試次數)
//
// 數據結構設計:
//   jobs map[JobID]*Job - 主存儲，包含所有任務
//   ├─ Job.Status 字段標識當前狀態
//   └─ 通過狀態字段快速篩選不同狀態的任務
//
//   輔助索引（提升性能）:
//   - queue []JobID - pending 任務隊列，保證 FIFO
//   - inFlight map - 執行中任務索引
//   - completed map - 已完成任務索引
//   - dead map - 死信任務索引
//
// 並發安全:
//   - 使用 sync.RWMutex 保護所有數據結構
//   - 讀操作使用 RLock，寫操作使用 Lock
//   - 確保多 goroutine 並發訪問的安全性
//
// 快照支持:
//   - Snapshot() - 序列化當前所有任務狀態
//   - Restore() - 從快照恢復任務狀態
//   - 用於崩潰恢復和系統遷移
//
// 職責說明：
//   1. 定義系統的統一狀態管理結構（單一 jobs map）
//   2. 維護任務狀態轉換的完整性（Pending -> InFlight -> Completed/Dead）
//   3. 提供任務生命週期管理方法
//   4. 支援快照序列化與反序列化
//
// ============================================================================

package jobmanager

import (
	"errors"
	"sync"
	"time"

	"github.com/ChuLiYu/raft-recovery/pkg/types"
)

// ============================================================================
// 錯誤定義
// ============================================================================

var (
	// 任務 ID 重複錯誤
	ErrDuplicateJob = errors.New("job already exists")
	// 任務不在執行中狀態
	ErrNotInFlight = errors.New("job not in flight")
	// 任務不存在
	ErrJobNotFound = errors.New("job not found")
)

// 使用 pkg/types 中定義的狀態常數

// ============================================================================
// 資料結構定義（簡化版本）
// ============================================================================

// 使用 pkg/types 中定義的領域模型

// JobManager 代表任務管理器，使用混合設計確保效率
type JobManager struct {
	mu        sync.RWMutex
	jobs      map[types.JobID]*types.Job // 所有任務的統一儲存，透過 Status 欄位區分狀態
	queue     []types.JobID              // 待處理佇列
	inFlight  map[types.JobID]*types.Job // 執行中任務
	completed map[types.JobID]*types.Job // 已完成任務
	dead      map[types.JobID]*types.Job // 死信任務
}

// SnapShotData 快照資料，包含所有任務的完整狀態
type SnapShotData struct {
	Jobs      map[types.JobID]*types.Job `json:"jobs"`           // 所有任務的完整資料
	SchemaVer int                        `json:"schema_version"` // 版本號
}

// ============================================================================
// 核心方法偽代碼
// ============================================================================

// NewJobManager 建立新的任務管理器實例
//
// 返回值：
//   - *JobManager: 初始化完成的任務管理器
//
// 使用範例：
//
//	jm := NewJobManager()
//	job := Job{ID: "task-001", Payload: map[string]interface{}{"key": "value"}}
//	err := jm.Enqueue(job)
//
// 併發安全：返回的實例是執行緒安全的
func NewJobManager() *JobManager {
	return &JobManager{
		jobs:      make(map[types.JobID]*types.Job),
		queue:     make([]types.JobID, 0),
		inFlight:  make(map[types.JobID]*types.Job),
		completed: make(map[types.JobID]*types.Job),
		dead:      make(map[types.JobID]*types.Job),
	}
}

// Enqueue 將新任務加入系統，設定為待處理狀態
//
// 參數說明：
//   - job: 要加入的任務，必須包含唯一 ID
//
// 返回值：
//   - error: 如果任務 ID 重複則回傳 ErrDuplicateJob
//
// 錯誤處理：
//   - ErrDuplicateJob: 任務 ID 已存在於系統中
//
// 使用範例：
//
//	job := Job{ID: "task-001", Payload: map[string]interface{}{"key": "value"}}
//	err := jm.Enqueue(job)
//	if err != nil {
//	    log.Printf("加入任務失敗: %v", err)
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) Enqueue(job types.Job) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 檢查任務 ID 是否已存在
	if _, exists := jm.jobs[job.ID]; exists {
		return ErrDuplicateJob
	}

	// 設定任務狀態和時間戳
	now := time.Now().UnixMilli()
	job.Status = types.StatusPending
	job.CreatedAt = now
	job.UpdatedAt = now

	// 加入系統
	jm.jobs[job.ID] = &job
	jm.queue = append(jm.queue, job.ID)
	return nil
}

// PopPending 取出一個待處理的任務，但不改變其狀態
//
// 返回值：
//   - *Job: 第一個待處理任務的指標，如果沒有待處理任務則回傳 nil
//
// 使用範例：
//
//	job := state.PopPending()
//	if job != nil {
//	    log.Printf("取出任務: %s", job.ID)
//	    // 處理任務後需要呼叫 MarkInFlight 來改變狀態
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) PopPending() *types.Job {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	if len(jm.queue) == 0 {
		return nil
	}

	jobID := jm.queue[0]
	jm.queue = jm.queue[1:] // 移除第一個

	return jm.jobs[jobID] // O(1) 查找
}

// MarkInFlight 將任務標記為執行中狀態，設定截止時間
//
// 參數說明：
//   - jobID: 要標記的任務 ID
//   - deadline: 任務的截止時間
//
// 返回值：
//   - error: 如果任務不存在或狀態不正確則回傳錯誤
//
// 錯誤處理：
//   - ErrJobNotFound: 任務不存在於系統中
//   - 自定義錯誤: 任務狀態不是 StatusPending
//
// 使用範例：
//
//	deadline := time.Now().Add(30 * time.Second)
//	err := state.MarkInFlight("task-001", deadline)
//	if err != nil {
//	    log.Printf("標記執行中失敗: %v", err)
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) MarkInFlight(jobID types.JobID, deadline time.Time) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 檢查任務是否存在
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// 檢查任務狀態是否為待處理
	if job.Status != types.StatusPending {
		return errors.New("job not in pending status")
	}

	// 更新任務狀態
	deadlineMs := deadline.UnixMilli()
	job.Status = types.StatusInFlight
	job.Deadline = &deadlineMs
	job.UpdatedAt = time.Now().UnixMilli()

	// 加入 inFlight 集合
	jm.inFlight[jobID] = job

	return nil
}

// MarkCompleted 將任務標記為已完成狀態
//
// 參數說明：
//   - jobID: 要標記完成的任務 ID
//
// 返回值：
//   - error: 如果任務不存在或狀態不正確則回傳錯誤
//
// 錯誤處理：
//   - ErrJobNotFound: 任務不存在於系統中
//   - ErrNotInFlight: 任務不在執行中狀態
//
// 使用範例：
//
//	err := state.MarkCompleted("task-001")
//	if err != nil {
//	    log.Printf("標記完成失敗: %v", err)
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) MarkCompleted(jobID types.JobID) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 檢查任務是否存在
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// 檢查任務狀態是否為執行中
	if job.Status != types.StatusInFlight {
		return ErrNotInFlight
	}

	// 更新任務狀態
	job.Status = types.StatusCompleted
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = time.Now().UnixMilli()

	// 從 inFlight 移除，加入 completed
	delete(jm.inFlight, jobID)
	jm.completed[jobID] = job

	return nil
}

// Requeue 將執行中的任務重新排隊，增加重試次數
//
// 參數說明：
//   - jobID: 要重新排隊的任務 ID
//
// 返回值：
//   - error: 如果任務不存在或狀態不正確則回傳錯誤
//
// 錯誤處理：
//   - ErrJobNotFound: 任務不存在於系統中
//   - ErrNotInFlight: 任務不在執行中狀態
//
// 使用範例：
//
//	err := state.Requeue("task-001")
//	if err != nil {
//	    log.Printf("重新排隊失敗: %v", err)
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) Requeue(jobID types.JobID) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 檢查任務是否存在
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// 檢查任務狀態是否為執行中
	if job.Status != types.StatusInFlight {
		return ErrNotInFlight
	}

	// 增加重試次數並重新排隊
	job.Attempt++
	job.Status = types.StatusPending
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = time.Now().UnixMilli()

	// 從 inFlight 移除，重新加入 queue
	delete(jm.inFlight, jobID)
	jm.queue = append(jm.queue, jobID)

	return nil
}

// MarkDead 將任務標記為死信狀態（失敗超過重試次數）
//
// 參數說明：
//   - jobID: 要標記為死信的任務 ID
//
// 返回值：
//   - error: 如果任務不存在則回傳錯誤
//
// 使用範例：
//
//	err := jm.MarkDead("task-001")
//	if err != nil {
//	    log.Printf("標記死信失敗: %v", err)
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) MarkDead(jobID types.JobID) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 檢查任務是否存在
	job, exists := jm.jobs[jobID]
	if !exists {
		return ErrJobNotFound
	}

	// 更新任務狀態
	job.Status = types.StatusDead
	job.Deadline = nil
	job.WorkerID = ""
	job.UpdatedAt = time.Now().UnixMilli()

	// 從 inFlight 移除，加入 dead
	delete(jm.inFlight, jobID)
	jm.dead[jobID] = job

	return nil
}

// GetExpiredJobs 取得已過期的執行中任務
//
// 參數說明：
//   - now: 當前時間
//
// 返回值：
//   - []JobID: 已過期的任務 ID 列表
//
// 使用範例：
//
//	expiredJobs := jm.GetExpiredJobs(time.Now())
//	for _, jobID := range expiredJobs {
//	    log.Printf("任務 %s 已過期", jobID)
//	}
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) GetExpiredJobs(now time.Time) []types.JobID {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	var expired []types.JobID
	nowMs := now.UnixMilli()

	for jobID, job := range jm.inFlight {
		if job.Deadline != nil && *job.Deadline < nowMs {
			expired = append(expired, jobID)
		}
	}

	return expired
}

// GetAllInFlightJobs 取得所有執行中的任務 ID
//
// 返回值：
//   - []types.JobID: 所有執行中任務的 ID 列表
//
// 用途：主要用於恢復時重新調度所有執行中的任務
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) GetAllInFlightJobs() []types.JobID {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	var inFlightJobs []types.JobID
	for jobID := range jm.inFlight {
		inFlightJobs = append(inFlightJobs, jobID)
	}

	return inFlightJobs
}

// Stats 取得各狀態任務的統計資訊
//
// 返回值：
//   - map[string]int: 各狀態的任務數量統計
//
// 使用範例：
//
//	stats := jm.Stats()
//	log.Printf("待處理: %d, 執行中: %d, 已完成: %d, 死信: %d",
//	    stats["pending"], stats["in_flight"], stats["completed"], stats["dead"])
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) Stats() map[string]int {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	return map[string]int{
		"pending":   len(jm.queue),
		"in_flight": len(jm.inFlight),
		"completed": len(jm.completed),
		"dead":      len(jm.dead),
	}
}

// ============================================================================
// 快照與恢復相關方法
// ============================================================================

// Restore 從快照恢復狀態
//
// 參數說明：
//   - data: 快照資料
//
// 返回值：
//   - error: 恢復失敗時的錯誤
//
// 使用範例：
//
//	data, _ := snapshot.Load()
//	err := jm.Restore(data)
//	if err != nil {
//	    log.Printf("恢復失敗: %v", err)
//	}
//
// 併發安全：使用互斥鎖保護
func (jm *JobManager) Restore(data types.SnapshotData) error {
	jm.mu.Lock()
	defer jm.mu.Unlock()

	// 清空現有狀態
	jm.jobs = make(map[types.JobID]*types.Job)
	jm.queue = make([]types.JobID, 0)
	jm.inFlight = make(map[types.JobID]*types.Job)
	jm.completed = make(map[types.JobID]*types.Job)
	jm.dead = make(map[types.JobID]*types.Job)

	// 恢復所有任務
	for jobID, job := range data.Jobs {
		jm.jobs[jobID] = job

		// 根據狀態分類
		switch job.Status {
		case types.StatusPending:
			jm.queue = append(jm.queue, jobID)
		case types.StatusInFlight:
			jm.inFlight[jobID] = job
		case types.StatusCompleted:
			jm.completed[jobID] = job
		case types.StatusDead:
			jm.dead[jobID] = job
		}
	}

	return nil
}

// Snapshot 生成快照資料
//
// 返回值：
//   - types.SnapshotData: 當前狀態的快照
//
// 使用範例：
//
//	data := jm.Snapshot()
//	snapshot.Write(data)
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) Snapshot() types.SnapshotData {
	jm.mu.RLock()
	defer jm.mu.RUnlock()

	// 深拷貝所有任務
	jobsCopy := make(map[types.JobID]*types.Job, len(jm.jobs))
	for id, job := range jm.jobs {
		jobCopy := *job
		jobsCopy[id] = &jobCopy
	}

	return types.SnapshotData{
		Jobs:      jobsCopy,
		SchemaVer: 1,
	}
}

// ============================================================================
// 查詢方法
// ============================================================================

// IsCompleted 檢查任務是否已完成
//
// 參數說明：
//   - jobID: 任務 ID
//
// 返回值：
//   - bool: 是否已完成
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) IsCompleted(jobID types.JobID) bool {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	_, exists := jm.completed[jobID]
	return exists
}

// IsDead 檢查任務是否已死亡
//
// 參數說明：
//   - jobID: 任務 ID
//
// 返回值：
//   - bool: 是否已死亡
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) IsDead(jobID types.JobID) bool {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	_, exists := jm.dead[jobID]
	return exists
}

// GetJob 取得任務
//
// 參數說明：
//   - jobID: 任務 ID
//
// 返回值：
//   - *types.Job: 任務指標，如果不存在則回傳 nil
//
// 併發安全：使用讀鎖保護
func (jm *JobManager) GetJob(jobID types.JobID) *types.Job {
	jm.mu.RLock()
	defer jm.mu.RUnlock()
	return jm.jobs[jobID]
}
