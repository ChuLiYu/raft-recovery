package wal

// ============================================================================
// 批次寫入器（選用優化）
// 職責：批次累積事件，減少 fsync 次數以提升效能
// ============================================================================

import (
	"sync"
	"time"
)

// BatchWriter 批次寫入器
//
// 設計理念：
// - 累積多個事件，一次性寫入並 fsync
// - 權衡：延遲 vs 吞吐量
//
// 使用時機：
// - 高吞吐量場景（> 1000 events/s）
// - 可接受微小延遲（< 10ms）
type BatchWriter struct {
	wal *WAL // 底層 WAL 實例

	mu     sync.Mutex
	buffer []Event      // 待寫入的事件緩衝
	timer  *time.Timer  // 定時 flush 計時器

	// 配置
	maxBatchSize  int           // 緩衝區大小閾值
	flushInterval time.Duration // 最大等待時間
}

// NewBatchWriter 建立批次寫入器
//
// 參數：
//   wal           - 底層 WAL 實例
//   maxBatchSize  - 累積多少事件後立即 flush
//   flushInterval - 最多等待多久就 flush（即使未滿）
func NewBatchWriter(wal *WAL, maxBatchSize int, flushInterval time.Duration) *BatchWriter {
	// TODO: 實作建構函式
	// 1. 建立 BatchWriter 結構
	// 2. 啟動背景 goroutine 定期 flush：
	//    go bw.flushLoop()
	// 3. 思考：
	//    - 如何優雅地停止背景 goroutine？
	//    - 需要 context.Context 嗎？
	
	return nil
}

// Append 追加事件到緩衝區
//
// 行為：
// - 加入緩衝區
// - 如果緩衝區滿了，立即 flush
// - 否則等待定時 flush
func (bw *BatchWriter) Append(eventType EventType, jobID string) error {
	// TODO: 實作批次追加
	// 1. 建立 Event（但不立即寫入檔案）
	// 2. 加入 buffer
	// 3. 檢查是否需要 flush：
	//    if len(buffer) >= maxBatchSize {
	//      return bw.flush()
	//    }
	// 4. 思考：
	//    - Append 是否應該阻塞直到 flush 完成？
	//    - 還是非同步 flush，立即回傳？
	
	return nil
}

// Flush 立即寫入所有緩衝的事件
func (bw *BatchWriter) Flush() error {
	// TODO: 實作強制 flush
	// 1. 加鎖
	// 2. 遍歷 buffer，逐個寫入 WAL
	// 3. 一次性 Sync
	// 4. 清空 buffer
	// 5. 思考：
	//    - 如果中途某個事件寫入失敗，如何處理？
	//    - 已寫入的事件是否回滾？還是接受部分寫入？
	
	return nil
}

// Close 關閉批次寫入器
func (bw *BatchWriter) Close() error {
	// TODO: 實作關閉邏輯
	// 1. 停止背景 goroutine
	// 2. Flush 剩餘的緩衝事件
	// 3. 不關閉底層 WAL（由呼叫者負責）
	
	return nil
}

// ============================================================================
// 私有方法
// ============================================================================

// flushLoop 背景定時 flush 循環
func (bw *BatchWriter) flushLoop() {
	// TODO: 實作定時 flush
	// ticker := time.NewTicker(flushInterval)
	// for range ticker.C {
	//   bw.Flush()
	// }
	//
	// 思考：
	// - 如何停止這個 goroutine？（context 或 done channel）
	// - Flush 失敗時如何處理？記錄日誌？通知使用者？
}

// TODO: 批次寫入的進階思考
//
// 1. 效能測試：
//    - 測試不同 maxBatchSize 的影響（1, 10, 100, 1000）
//    - 測試不同 flushInterval 的影響（1ms, 10ms, 100ms）
//    - 找到最佳平衡點
//
// 2. 記憶體管理：
//    - 緩衝區太大會佔用記憶體
//    - 考慮使用 sync.Pool 重用 Event 物件
//
// 3. 延遲敏感性：
//    - 批次寫入會增加延遲（最壞情況 = flushInterval）
//    - 是否提供「即時模式」和「批次模式」兩種選項？
//
// 4. 崩潰恢復：
//    - 如果崩潰時緩衝區有未寫入的事件，會遺失
//    - 如何權衡效能與可靠性？
//    - 關鍵任務系統可能不適合批次寫入

