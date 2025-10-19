package wal

import "github.com/ChuLiYu/raft-recovery/pkg/types"

// ============================================================================
// WAL 型別定義
// 職責：定義 WAL 的核心資料結構
// ============================================================================

// EventType 定義 WAL 事件類型
type EventType string

const (
	EventEnqueue  EventType = "ENQUEUE"  // 任務加入佇列
	EventDispatch EventType = "DISPATCH" // 任務分派給 Worker
	EventAck      EventType = "ACK"      // Worker 確認完成
	EventRetry    EventType = "RETRY"    // 任務重新排隊
	EventTimeout  EventType = "TIMEOUT"  // 任務超時
	EventDead     EventType = "DEAD"     // 任務失敗（超過重試次數）
)

// Event 表示一個 WAL 事件記錄
type Event struct {
	Seq       uint64      `json:"seq"`       // 事件序號（單調遞增）
	Type      EventType   `json:"type"`      // 事件類型
	JobID     types.JobID `json:"job_id"`    // 任務 ID（使用 pkg/types 的類型）
	Timestamp int64       `json:"timestamp"` // Unix 毫秒時間戳
	Checksum  uint32      `json:"checksum"`  // CRC32 校驗和

	// Phase 2: 擴充欄位
	// WorkerID  string      `json:"worker_id,omitempty"` // 處理任務的 Worker ID
	// Attempt   int         `json:"attempt,omitempty"`   // 任務嘗試次數
	// Payload   []byte      `json:"payload,omitempty"`   // 部分任務資料（除錯用）
}

// TODO (Phase 2):
// - 擴充 Event 結構：加入 WorkerID、Attempt、Payload 等欄位
// - 設計 Payload 結構，考慮序列化/反序列化效率
// - 評估 WAL 記錄大小與效能影響
// - 增加事件型別（如 CANCEL, PAUSE 等）
// - 支援事件的版本管理（兼容升級）

// EventHandler 是處理 WAL 事件的函式型別
// 用於 Replay 時應用事件到系統狀態
type EventHandler func(event Event) error

// TODO (Phase 2):
// - handler 回傳錯誤時，是否應該中止整個 Replay？
// - 支援「跳過損壞事件」的寬容模式
// - 記錄哪些事件處理失敗，方便除錯與恢復
