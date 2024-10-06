package wal

// ============================================================================
// 校驗和計算
// 職責：計算與驗證 WAL 事件的 CRC32 校驗和
// ============================================================================

import (
	"hash/crc32"

	"github.com/ChuLiYu/beaver-raft/pkg/types"
)

// CalculateChecksum 計算事件的 CRC32 校驗和
//
// 演算法：
// - 將事件的關鍵欄位串接成字串
// - 使用 CRC32-IEEE 多項式計算
//
// 參數：
//
//	eventType - 事件類型
//	jobID     - 任務 ID
//	seq       - 事件序號
//
// 回傳：
//
//	uint32 校驗和
func CalculateChecksum(eventType EventType, jobID types.Job, seq uint64) uint32 {
	// 組合事件的關鍵欄位
	// 使用 Type + JobID + Seq 來計算 checksum
	// 不包含 Timestamp，因為它會在重放時變化
	data := string(eventType) + string(jobID.ID) + string(rune(seq))

	// 使用 CRC32-IEEE 計算校驗和
	return crc32.ChecksumIEEE([]byte(data))
}

// VerifyChecksum 驗證事件的校驗和是否正確
//
// 參數：
//
//	event - 要驗證的事件
//
// 回傳：
//
//	bool - true 表示校驗和正確
func VerifyChecksum(event Event) bool {
	// 重新計算預期的校驗和
	// 注意：需要創建一個 types.Job 來匹配 CalculateChecksum 的簽名
	job := types.Job{ID: event.JobID}
	expected := CalculateChecksum(event.Type, job, event.Seq)

	// 比較計算出的校驗和與事件中存儲的校驗和
	return event.Checksum == expected
}

// TODO: 進階功能思考
//
// 1. 多種校驗演算法支援：
//    - CRC32（快速，檢測隨機錯誤）
//    - SHA256（安全，防止篡改）
//    - 讓使用者選擇？
//
// 2. 校驗範圍：
//    - 目前只校驗 Type + JobID + Seq
//    - 是否應該包含 Timestamp？
//    - 是否應該包含整個 Event JSON？
//
// 3. 效能優化：
//    - 預先分配字串緩衝區避免重複分配
//    - 使用 strings.Builder
