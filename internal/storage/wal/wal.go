package wal

// ============================================================================
// WAL 核心實作
// 職責：
// 1. 追加事件到日誌檔案（append-only）
// 2. 提供重放功能以恢復系統狀態
// 3. 支援日誌旋轉（快照後清空）
// 4. 確保寫入持久性與資料完整性
// ============================================================================

import (
	"compress/gzip"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"sync"
	"time"

	"github.com/ChuLiYu/beaver-raft/pkg/types"
)

// FileInterface 定義檔案操作所需的方法
// 這允許在測試中對檔案操作進行模擬
type FileInterface interface {
	Write(p []byte) (n int, err error)
	Sync() error
	Close() error
}

// WAL 表示 Write-Ahead Log 實例
type WAL struct {
	mu           sync.Mutex    // 保護並發寫入
	file         FileInterface // WAL 檔案
	encoder      *json.Encoder // JSON 編碼器
	path         string        // WAL 檔案路徑
	seq          uint64        // 當前事件序號
	syncOnAppend bool          // 是否每次追加都強制同步

	buffer        []Event // 批次寫入事件緩衝区，原因：直接用結構化事件，方便序列化與管理，避免 bytes.Buffer 需額外 encode/decode
	bufferSize    int
	lastFlushTime time.Time
	flushInterval time.Duration
}

// SnapshotData represents the metadata for a snapshot
// This is used to integrate WAL with snapshot recovery
type SnapshotData struct {
	LastSeq uint64 // The last sequence number included in the snapshot
}

// ============================================================================
// 公開介面
// ============================================================================

/*
NewWAL 建立或開啟一個 WAL 實例

行為：
- 如果檔案不存在，建立新檔案，seq 從 0 開始
- 如果檔案已存在，讀取最後一個事件的 seq 並繼續
- 以追加模式（O_APPEND）開啟，確保寫入不覆蓋

參數：

	path - WAL 檔案路徑

回傳：

	*WAL 實例，錯誤（如果有）
*/
func NewWAL(path string, syncOnAppend bool) (*WAL, error) {
	// 以 O_CREATE | O_APPEND | O_RDWR 模式打開 WAL 檔案
	file, err := os.OpenFile(path, os.O_CREATE|os.O_APPEND|os.O_RDWR, 0644)
	if err != nil {
		// 開檔失敗直接回傳錯誤
		return nil, err
	}

	// 以 JSON Encoder 包裝檔案，方便後續寫入事件
	encoder := json.NewEncoder(file)

	// 初始化事件序號，預設為 0
	var seq uint64 = 0

	// 若檔案非空，嘗試讀取最後一個事件以取得 seq
	stat, statErr := file.Stat()
	if statErr == nil && stat.Size() > 0 {
		lastEvent, err := GetLastEvent(path)
		if err == nil && lastEvent != nil {
			seq = lastEvent.Seq
		}
		// 若讀取失敗或檔案損毀，可選擇 seq 保持為 0，視需求決定
	}

	// 建立 WAL 實例，注入狀態
	wal := &WAL{
		mu:           sync.Mutex{},
		file:         file,
		encoder:      encoder,
		path:         path,
		seq:          seq,
		syncOnAppend: syncOnAppend,

		buffer:        make([]Event, 0, 1000), // 預設容量 1000
		bufferSize:    1000,
		lastFlushTime: time.Now(),
		flushInterval: 1 * time.Second,
	}

	// 回傳 WAL 實例
	return wal, nil
}

// Append 追加一個事件到 WAL
//
// 行為：
// - 自動遞增 seq
// - 計算 checksum
// - 寫入檔案並同步到磁碟（可選：批次同步）
//
// 參數：
//
//	eventType - 事件類型（ENQUEUE, DISPATCH, ACK 等）
//	job       - 任務實例
//
// 回傳：
//
//	錯誤（如果寫入失敗）
func (w *WAL) Append(eventType EventType, job types.Job, isForceFlush bool) error {
	w.mu.Lock()
	w.seq++
	timestamp := time.Now().UnixMilli()
	event := Event{
		Seq:       w.seq,
		Type:      eventType,
		JobID:     job.ID,
		Timestamp: timestamp,
	}
	event.Checksum = CalculateChecksum(eventType, job, w.seq)

	// 批次寫入：先加入 buffer，滿了或超時才 flush
	w.buffer = append(w.buffer, event)

	// 檢查是否需要 flush
	needFlush := isForceFlush || len(w.buffer) >= w.bufferSize || time.Since(w.lastFlushTime) > w.flushInterval

	if needFlush {
		// 在鎖內調用內部方法
		err := w.flushLocked()
		w.mu.Unlock()
		return err
	}

	w.mu.Unlock()
	return nil
}

// Replay 重放所有 WAL 事件
//
// 行為：
// - 從頭讀取 WAL 檔案
// - 驗證每個事件的 checksum
// - 呼叫 handler 應用事件
// - 遇到錯誤立即停止
//
// 參數：
//
//	handler - 事件處理函式
//
// 回傳：
//
//	錯誤（如果重放失敗）
func (w *WAL) Replay(handler EventHandler) error {
	// 加鎖保護，避免與其他操作衝突
	w.mu.Lock()
	defer w.mu.Unlock()

	// 重新開啟檔案（只讀模式）
	file, err := os.Open(w.path)
	if err != nil {
		return err
	}
	defer file.Close()

	// 建立 JSON decoder
	decoder := json.NewDecoder(file)

	// 循環讀取每個事件
	for decoder.More() {
		var event Event
		// Decode 事件
		if err := decoder.Decode(&event); err != nil {
			return err
		}

		// 驗證 checksum（使用 VerifyChecksum）
		if !VerifyChecksum(event) {
			return ErrChecksumMismatch
		}

		// 呼叫 handler(event)
		if err := handler(event); err != nil {
			return err
		}
	}

	return nil
}

// Rotate 旋轉日誌檔案
//
// 回傳：
//
//	錯誤（如果旋轉失敗）
func (w *WAL) Rotate() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 先 flush buffer，確保所有事件寫入
	if err := w.flushLocked(); err != nil {
		return err
	}

	if err := w.file.Close(); err != nil {
		return err
	}

	backupPath := w.path + "." + time.Now().Format("20060102_150405")
	if err := os.Rename(w.path, backupPath); err != nil {
		return err
	}

	newFile, err := os.OpenFile(w.path, os.O_CREATE|os.O_RDWR|os.O_TRUNC, 0644)
	if err != nil {
		return err
	}

	w.file = newFile
	w.encoder = json.NewEncoder(newFile)
	w.seq = 0
	w.buffer = w.buffer[:0]
	w.lastFlushTime = time.Now()

	return nil
}

// Close 關閉 WAL
func (w *WAL) Close() error {
	w.mu.Lock()
	defer w.mu.Unlock()

	// 先 flush buffer，確保所有事件寫入
	if err := w.flushLocked(); err != nil {
		return err
	}

	if err := w.file.Close(); err != nil {
		return err
	}

	// 決定：關閉後的 WAL 實例不建議重用。
	//    原因：
	//    - 檔案描述器與 encoder 已釋放，重用會造成錯誤（如 nil pointer）。
	//    - Go 標準慣例：Close 後即失效，禁止再用。
	//    - 明確禁止重用可提升安全性與可維護性。
	return nil
}

// GetLastSeq 取得當前的事件序號
//
// 用途：快照時需要記錄 last_seq，確保恢復時知道從哪裡開始重放
func (w *WAL) GetLastSeq() uint64 {
	if w == nil {
		return 0
	}

	w.mu.Lock()
	defer w.mu.Unlock()
	return w.seq
}

// ============================================================================
// 內部輔助方法（私有）
// ============================================================================

// 如果需要批次寫入優化，可以考慮以下私有方法：
//
// flush 公開方法，供外部調用（如 Close、Rotate）
// 負責加鎖並調用內部實現
func (w *WAL) flush() error {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.flushLocked()
}

// flushLocked 內部方法，假設調用者已經持有 w.mu 鎖
// 將緩衝的事件批次寫入並同步到磁碟
func (w *WAL) flushLocked() error {
	for _, event := range w.buffer {
		if err := w.encoder.Encode(event); err != nil {
			return err
		}
	}
	w.buffer = w.buffer[:0]
	w.lastFlushTime = time.Now()
	if err := w.file.Sync(); err != nil {
		return err
	}
	return nil
}

// TODO: 進階優化思考
//
// 4. 設計考慮：是否需要記錄重放進度（seq）？
//    - 可在此處記錄設計相關問題，避免混入函式實作中。

// ============================================================================
// 進階優化：gzip 壓縮與多檔案管理（暫不引用）
// ============================================================================

// gzip 壓縮 WAL 檔案
// 最佳實踐：
// - 只在檔案旋轉或快照時進行壓縮，避免每次寫入都壓縮造成效能瓶頸
// - 檔名加 .gz，方便辨識
// - 使用 io.Pipe + goroutine 可非同步壓縮，減少主流程阻塞ile(srcPath, dstPath string) error {
// - 檔名加 .gz，方便辨識
func compressWALFile(srcPath, dstPath string) error {
	srcFile, err := os.Open(srcPath)
	if err != nil {
		return err
	}
	defer srcFile.Close()
	dstFile, err := os.Create(dstPath)
	if err != nil {
		return err
	}
	defer dstFile.Close()

	// 建立 gzip writer
	gzipWriter := gzip.NewWriter(dstFile)
	defer gzipWriter.Close()

	// 直接複製內容到壓縮檔
	_, err = io.Copy(gzipWriter, srcFile)
	return err
}

// 多檔案管理：自動切分 WAL 檔案
// 最佳實踐：
// - 依檔案大小或事件數量自動切分
// - 切分時保證事件完整性（不可中斷事件序列）
// - 切分後檔名加序號或 timestamp，方便重放時排序
func splitWALFile(srcPath string, maxSize int64) ([]string, error) {
	// 定義變數：files 儲存分檔路徑，srcFile 為來源檔案，outFile 為當前分檔
	var files []string               // 儲存所有分檔的路徑
	srcFile, err := os.Open(srcPath) // 開啟原始 WAL 檔案
	if err != nil {
		return nil, err
	}
	defer srcFile.Close()

	// 建立 JSON 解碼器，逐行讀取事件
	decoder := json.NewDecoder(srcFile) // 用於解析 WAL 檔案中的事件
	var part int                        // 分檔的序號，用於生成分檔名稱
	var currentSize int64               // 當前分檔的大小，用於判斷是否需要切分
	var outFile *os.File                // 當前分檔的檔案指標
	var encoder *json.Encoder           // 用於將事件寫入分檔的 JSON 編碼器
	for decoder.More() {
		var event Event // 單個事件的結構
		if err := decoder.Decode(&event); err != nil {
			return nil, err
		}
		// 若尚未建立分檔或已達最大大小，則新建分檔
		if outFile == nil || currentSize >= maxSize {
			if outFile != nil {
				outFile.Close()
			}
			part++
			outPath := srcPath + ".part" + fmt.Sprintf("%03d", part) // 生成分檔名稱
			outFile, err = os.Create(outPath)                        // 建立新分檔
			if err != nil {
				return nil, err
			}
			encoder = json.NewEncoder(outFile) // 初始化 JSON 編碼器
			files = append(files, outPath)     // 將分檔路徑加入列表
			currentSize = 0
		}
		// 寫入事件到當前分檔
		if err := encoder.Encode(event); err != nil {
			outFile.Close()
			return nil, err
		}
		// 更新當前分檔大小
		currentSize += int64(len(fmt.Sprintf("%v", event)))
	}
	// 關閉最後一個分檔
	if outFile != nil {
		outFile.Close()
	}
	return files, nil
}
