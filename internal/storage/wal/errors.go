package wal

// ============================================================================
// WAL 錯誤定義
// 職責：定義 WAL 相關的所有錯誤類型
// ============================================================================

import "errors"

// 預定義錯誤
var (
	// ErrCorruptedWAL 表示 WAL 檔案損壞（無法解析 JSON）
	ErrCorruptedWAL = errors.New("wal: file is corrupted")

	// ErrChecksumMismatch 表示校驗和不符（資料損壞或篡改）
	ErrChecksumMismatch = errors.New("wal: checksum mismatch")

	// ErrEmptyWAL 表示 WAL 檔案為空（重放時可能遇到）
	ErrEmptyWAL = errors.New("wal: file is empty")

	// ErrWALClosed 表示 WAL 已關閉，無法執行操作
	ErrWALClosed = errors.New("wal: already closed")

	// ErrSyncFailed 表示 fsync 失敗（嚴重錯誤）
	ErrSyncFailed = errors.New("wal: sync to disk failed")
)

// TODO: 思考錯誤處理策略
//
// 1. 錯誤分類：
//    - 可恢復錯誤（Recoverable）：暫時性失敗，可重試
//      例如：磁碟暫時忙碌
//    - 不可恢復錯誤（Fatal）：嚴重問題，必須停止
//      例如：WAL 檔案損壞、校驗和錯誤
//
// 2. 錯誤包裝：
//    - 使用 fmt.Errorf("wal: append failed at seq=%d: %w", seq, err)
//    - 提供更多上下文資訊（seq, jobID, 檔案位置）
//
// 3. 錯誤回報：
//    - 是否需要將錯誤記錄到日誌？
//    - 是否需要通知監控系統（metrics）？
//    - 如何讓使用者知道 WAL 出了問題？

// ChecksumError 表示帶有詳細資訊的校驗和錯誤
type ChecksumError struct {
	Seq      uint64 // 出錯的事件序號
	Expected uint32 // 預期的校驗和
	Actual   uint32 // 實際的校驗和
}

func (e *ChecksumError) Error() string {
	// TODO: 實作錯誤訊息格式化
	// 範例："wal: checksum mismatch at seq=42 (expected=0x12345678, got=0x87654321)"
	return ""
}

// CorruptionError 表示 WAL 損壞錯誤
type CorruptionError struct {
	Seq    uint64 // 出錯的事件序號（如果已知）
	Offset int64  // 檔案中的位元組偏移量
	Cause  error  // 底層錯誤
}

func (e *CorruptionError) Error() string {
	// TODO: 實作錯誤訊息格式化
	return ""
}

func (e *CorruptionError) Unwrap() error {
	return e.Cause
}

// TODO: 進階錯誤處理思考
//
// 1. 錯誤恢復機制：
//    - 遇到損壞的事件時，是否嘗試跳過並繼續？
//    - 是否提供「修復」功能，移除損壞的事件？
//
// 2. 降級策略：
//    - 如果 WAL 完全損壞，是否允許從 Snapshot 啟動？
//    - 如何通知使用者可能有資料遺失？
//
// 3. 防禦性編程：
//    - 在關鍵路徑上使用 panic 還是回傳錯誤？
//    - WAL 寫入失敗是否應該讓整個系統停止？

