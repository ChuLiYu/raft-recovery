package worker

import (
	"context"
	"errors"
	"math/rand"
	"time"
)

// Worker 代表一個工作執行單元
type Worker struct {
	id       int           // Worker 唯一識別碼
	taskCh   <-chan Task   // 任務通道（只讀）
	resultCh chan<- Result // 結果通道（只寫）
}

// newWorker 建立新的 Worker 實例
func newWorker(id int, taskCh <-chan Task, resultCh chan<- Result) *Worker {
	return &Worker{
		id:       id,
		taskCh:   taskCh,
		resultCh: resultCh,
	}
}

// Run 是 Worker 的主循環，從任務通道接收任務並執行
// 每個任務執行後，會將結果發送到結果通道
func (w *Worker) Run() {
	for task := range w.taskCh {
		start := time.Now()

		// 建立帶超時的 Context，確保任務不會執行超過指定時間
		ctx, cancel := context.WithTimeout(context.Background(), task.Timeout)

		// 執行任務邏輯，並在完成後釋放 Context 資源
		err := w.execute(ctx, task.Payload)
		cancel() // 釋放資源

		// 將執行結果封裝為 Result 結構體
		result := Result{
			JobID:    task.ID,
			Success:  err == nil,
			Error:    err,
			Duration: time.Since(start),
		}

		// 嘗試將結果發送到結果通道
		select {
		case w.resultCh <- result:
			// 成功回報結果
		default:
			// 如果結果通道已滿或關閉，則忽略（罕見情況）
			// 在生產環境中應記錄日誌以便排查問題
		}
	}
}

// execute 執行實際的任務邏輯
// - 使用帶超時的 Context 確保任務不會無限期執行
// - 模擬工作邏輯包括隨機延遲和 10% 的失敗率
func (w *Worker) execute(ctx context.Context, payload map[string]interface{}) error {
	// 模擬 CPU 密集型工作，隨機延遲 0-500 毫秒
	workDuration := time.Duration(rand.Intn(500)) * time.Millisecond

	select {
	case <-ctx.Done():
		// 如果 Context 被取消或超時，返回對應的錯誤
		return ctx.Err()

	case <-time.After(workDuration):
		// 模擬 10% 的失敗率
		if rand.Intn(100) < 10 {
			return errors.New("模擬執行失敗")
		}
		return nil // 成功執行
	}
}
