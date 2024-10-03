// Package types 定義了 beaver-raft 系統中使用的核心領域模型
package types

import (
	"time"
)

// JobID 任務唯一識別碼
type JobID string

// JobStatus 任務狀態
type JobStatus string

// 定義任務狀態常數
const (
	StatusPending   JobStatus = "pending"   // 待處理狀態：任務已建立但尚未開始執行
	StatusInFlight  JobStatus = "in_flight" // 執行中狀態：任務正在被 worker 處理
	StatusCompleted JobStatus = "completed" // 完成狀態：任務已成功執行完畢
	StatusDead      JobStatus = "dead"      // 死亡狀態：任務執行失敗或超時
)

// Job 任務結構，代表系統中的一個工作單元
// 整合了 pkg/types 和 internal/jobmanager 兩版本的欄位
type Job struct {
	// 識別與資料
	ID      JobID                  `json:"id"`      // 任務唯一識別碼
	Payload map[string]interface{} `json:"payload"` // 任務執行所需的資料載荷

	// 狀態追蹤
	Status  JobStatus `json:"status"`  // 任務當前狀態
	Attempt int       `json:"attempt"` // 重試次數

	// 時間管理（使用 Unix 毫秒時間戳，符合原始設計）
	Timeout   time.Duration `json:"timeout"`               // 任務執行超時時間
	Deadline  *int64        `json:"deadline_ms,omitempty"` // 任務截止時間（Unix 毫秒）
	CreatedAt int64         `json:"created_at"`            // 任務建立時間（Unix 毫秒）
	UpdatedAt int64         `json:"updated_at"`            // 任務最後更新時間（Unix 毫秒）

	// 執行資訊
	WorkerID string `json:"worker_id,omitempty"` // 負責處理此任務的 worker ID
}

// InFlightInfo 執行中任務資訊，追蹤正在執行的任務狀態
type InFlightInfo struct {
	JobID     JobID `json:"job_id"`      // 執行中的任務 ID
	WorkerID  int   `json:"worker_id"`   // 執行此任務的 worker ID
	Deadline  int64 `json:"deadline_ms"` // 任務執行截止時間（Unix 毫秒）
	StartedAt int64 `json:"started_at"`  // 任務開始執行的時間（Unix 毫秒）
}

// SnapshotData 快照資料，用於系統狀態的持久化和恢復
// 使用統一的 Job 結構，簡化快照格式
type SnapshotData struct {
	Jobs      map[JobID]*Job `json:"jobs"`       // 所有任務的完整資料
	SchemaVer int            `json:"schema_ver"` // 資料結構版本號，用於向後相容性
	LastSeq   uint64         `json:"last_seq"`   // 最後處理的序列號
}
