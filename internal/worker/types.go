package worker

import (
	"time"

	"github.com/ChuLiYu/beaver-raft/pkg/types"
)

// Task 代表要執行的任務
type Task struct {
	ID      types.JobID            // 任務唯一識別碼
	Payload map[string]interface{} // 任務執行所需的資料載荷
	Timeout time.Duration          // 執行超時時間
}

// Result 代表任務執行結果
type Result struct {
	JobID    types.JobID   // 任務 ID
	Success  bool          // 執行是否成功
	Error    error         // 錯誤訊息（如果有）
	Duration time.Duration // 實際執行時間
}
