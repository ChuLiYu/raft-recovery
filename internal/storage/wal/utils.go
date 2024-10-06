package wal

// ============================================================================
// WAL 工具函式
// 職責：提供 WAL 相關的輔助功能
// ============================================================================

import (
	"io"
)

// ============================================================================
// 檔案操作輔助
// ============================================================================

// GetLastEvent 從 WAL 檔案讀取最後一個事件
//
// 用途：
// - NewWAL 時需要取得 last_seq 以繼續編號
// - 驗證 WAL 完整性
//
// 參數：
//   path - WAL 檔案路徑
//
// 回傳：
//   最後一個事件，錯誤（如果檔案為空則回傳 ErrEmptyWAL）
func GetLastEvent(path string) (*Event, error) {
	// TODO: 實作最後事件讀取
	// 策略選擇：
	//
	// 方案 A：從頭到尾掃描（簡單但慢）
	//   - 逐行讀取直到 EOF
	//   - 回傳最後一個成功解析的事件
	//
	// 方案 B：從檔尾往前搜尋（快速但複雜）
	//   - Seek 到檔尾
	//   - 往前搜尋最後一個換行符
	//   - 解析該行
	//
	// 方案 C：維護 index 檔案（最快但需要額外維護）
	//   - WAL.index 記錄每個事件的位置
	//   - 直接跳到最後一個事件
	//
	// 思考：哪種方案適合您的場景？
	
	return nil, nil
}

// CountEvents 計算 WAL 中的事件總數
//
// 用途：
// - 除錯與診斷
// - 統計與監控
func CountEvents(path string) (int, error) {
	// TODO: 實作事件計數
	// 1. 開啟檔案
	// 2. 使用 decoder 逐行讀取
	// 3. 計數成功解析的事件
	// 4. 忽略損壞的事件？還是回傳錯誤？
	
	return 0, nil
}

// ValidateWAL 驗證 WAL 檔案的完整性
//
// 檢查項目：
// - 所有事件的 JSON 格式正確
// - 所有事件的校驗和正確
// - seq 連續且無重複
//
// 回傳：
//   錯誤（如果發現問題）
func ValidateWAL(path string) error {
	// TODO: 實作 WAL 驗證
	// 1. Replay 所有事件
	// 2. 驗證每個事件的 checksum
	// 3. 驗證 seq 的連續性：
	//    lastSeq := uint64(0)
	//    for each event:
	//      if event.Seq != lastSeq + 1:
	//        return error
	//      lastSeq = event.Seq
	// 4. 收集並回報所有錯誤（不只是第一個）
	
	return nil
}

// ============================================================================
// WAL 修復工具（進階功能）
// ============================================================================

// RepairWAL 嘗試修復損壞的 WAL
//
// 修復策略：
// - 掃描檔案，移除無效的事件
// - 重新計算 seq（從 1 開始連續編號）
// - 生成新的 WAL 檔案
//
// 警告：此操作會改變事件序號！
func RepairWAL(srcPath, dstPath string) error {
	// TODO: 實作 WAL 修復（選用）
	// 1. 讀取 srcPath
	// 2. 過濾有效事件：
	//    - JSON 可解析
	//    - Checksum 正確
	// 3. 重新編號 seq
	// 4. 寫入 dstPath
	// 5. 思考：
	//    - 如何處理 checksum 錯誤但 JSON 有效的事件？
	//    - 是否需要使用者確認？
	//    - 是否記錄被移除的事件？
	
	return nil
}

// TruncateWAL 截斷 WAL 到指定序號
//
// 用途：
// - 恢復到某個已知的正確狀態
// - 回滾錯誤的操作
//
// 參數：
//   path - WAL 檔案路徑
//   seq  - 保留到此序號（不包含）
func TruncateWAL(path string, seq uint64) error {
	// TODO: 實作 WAL 截斷（選用）
	// 1. 讀取所有事件
	// 2. 過濾 seq < targetSeq 的事件
	// 3. 寫入新檔案
	// 4. 原子替換舊檔案
	// 5. 警告：確保操作的原子性！
	
	return nil
}

// ============================================================================
// 除錯與診斷工具
// ============================================================================

// DumpWAL 輸出 WAL 內容（人類可讀格式）
//
// 用途：
// - 除錯
// - 手動檢查事件
func DumpWAL(path string, w io.Writer) error {
	// TODO: 實作 WAL dump
	// 1. 讀取所有事件
	// 2. 格式化輸出：
	//    [Seq:1] ENQUEUE job-001 at 2024-01-01T00:00:00 (checksum:0x12345678)
	//    [Seq:2] DISPATCH job-001 at 2024-01-01T00:00:01 (checksum:0x87654321)
	// 3. 標記損壞的事件
	
	return nil
}

// CompareWAL 比較兩個 WAL 檔案的差異
//
// 用途：
// - 測試
// - 驗證 Rotate 正確性
func CompareWAL(path1, path2 string) ([]string, error) {
	// TODO: 實作 WAL 比較（選用）
	// 1. 讀取兩個檔案的所有事件
	// 2. 比較：
	//    - 事件數量
	//    - 每個事件的內容
	// 3. 回傳差異列表
	
	return nil, nil
}

// ============================================================================
// 統計與分析
// ============================================================================

// WALStats WAL 統計資訊
type WALStats struct {
	TotalEvents  int            // 總事件數
	EventTypes   map[EventType]int // 各類型事件計數
	FirstSeq     uint64         // 第一個事件的 seq
	LastSeq      uint64         // 最後一個事件的 seq
	TimeRange    [2]int64       // 時間範圍 [最早, 最晚]
	CorruptedCount int          // 損壞事件數
}

// GetWALStats 取得 WAL 的統計資訊
func GetWALStats(path string) (*WALStats, error) {
	// TODO: 實作統計資訊收集
	// 1. 掃描整個 WAL
	// 2. 收集各種統計資料
	// 3. 回傳 WALStats 結構
	
	return nil, nil
}

// TODO: 其他實用工具思考
//
// 1. WAL 合併：
//    - 合併多個 WAL 檔案成一個
//    - 用於歷史資料整合
//
// 2. WAL 分割：
//    - 將大 WAL 檔案分割成多個小檔案
//    - 按時間或 seq 範圍分割
//
// 3. WAL 壓縮：
//    - 移除已完成任務的 ENQUEUE/DISPATCH/ACK 事件
//    - 只保留必要的事件（例如 Dead 任務）
//
// 4. WAL 匯出：
//    - 轉換成其他格式（CSV, Parquet）
//    - 用於資料分析

