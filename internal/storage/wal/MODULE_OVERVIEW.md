# WAL 模組架構總覽

## 📦 檔案組織

```
internal/storage/wal/
│
├── 📘 核心檔案（必須實作）
│   ├── types.go              # 型別定義
│   ├── wal.go                # WAL 核心邏輯
│   ├── checksum.go           # 校驗和計算
│   └── errors.go             # 錯誤定義
│
├── 🚀 優化檔案（選用）
│   ├── batch_writer.go       # 批次寫入優化
│   └── utils.go              # 工具函式
│
├── 🧪 測試與文檔
│   ├── wal_test.go           # 測試檔案
│   ├── README.md             # 使用說明
│   ├── MODULE_OVERVIEW.md    # 本文件
│   └── integration_example.go # 整合範例
│
└── 📝 待移除（舊檔案）
    └── /internal/wal/wal.go  # 舊的假代碼檔案（已被取代）
```

---

## 🎯 實作優先順序

### Phase 1：基礎功能（第 1-2 天）

**目標**：實現基本的 WAL 讀寫功能

1. **types.go**

   - [ ] 定義 `Event` 結構
   - [ ] 定義 `EventType` 常數
   - [ ] 定義 `EventHandler` 型別

2. **checksum.go**

   - [ ] 實作 `CalculateChecksum()`
   - [ ] 實作 `VerifyChecksum()`

3. **errors.go**

   - [ ] 定義基本錯誤常數
   - [ ] 實作 `ChecksumError` 型別

4. **wal.go**
   - [ ] 實作 `NewWAL()`
   - [ ] 實作 `Append()`
   - [ ] 實作基本的檔案寫入

**驗證方式**：

```bash
# 建立簡單測試
go test -run TestNewWAL
go test -run TestAppend
```

---

### Phase 2：重放與恢復（第 3 天）

**目標**：實現崩潰恢復能力

1. **wal.go**

   - [ ] 實作 `Replay()`
   - [ ] 實作校驗和驗證
   - [ ] 處理損壞事件

2. **wal_test.go**
   - [ ] 測試 `TestReplay`
   - [ ] 測試 `TestChecksumValidation`
   - [ ] 測試 `TestCorruptedWAL`

**驗證方式**：

```bash
# 測試恢復流程
go test -run TestReplay
# 測試錯誤處理
go test -run TestChecksum
```

---

### Phase 3：日誌旋轉（第 4 天）

**目標**：支援 Snapshot 後清空 WAL

1. **wal.go**

   - [ ] 實作 `Rotate()`
   - [ ] 實作 `Close()`
   - [ ] 實作 `GetLastSeq()`

2. **wal_test.go**
   - [ ] 測試 `TestRotate`
   - [ ] 測試 `TestWALLifecycle`

**驗證方式**：

```bash
# 測試旋轉邏輯
go test -run TestRotate
```

---

### Phase 4：並發與整合（第 5 天）

**目標**：確保並發安全並與 Controller 整合

1. **wal_test.go**

   - [ ] 測試 `TestConcurrentAppend`
   - [ ] 測試 `TestSnapshotIntegration`

2. **整合到 Controller**
   - [ ] 修改 `controller.go` 使用 WAL
   - [ ] 實作恢復流程

**驗證方式**：

```bash
# 並發測試
go test -race -run TestConcurrent
# 整合測試
go test -run TestController
```

---

### Phase 5：優化（選用，第 6+ 天）

1. **batch_writer.go**

   - [ ] 實作批次寫入
   - [ ] 效能測試

2. **utils.go**
   - [ ] 實作 `ValidateWAL()`
   - [ ] 實作 `GetWALStats()`

**驗證方式**：

```bash
# 效能測試
go test -bench=BenchmarkAppend
go test -bench=BenchmarkBatchWriter
```

---

## 🔌 模組介面

### 公開 API

```go
// 建立與管理
func NewWAL(path string) (*WAL, error)
func (w *WAL) Close() error

// 核心操作
func (w *WAL) Append(eventType EventType, jobID string) error
func (w *WAL) Replay(handler EventHandler) error
func (w *WAL) Rotate() error

// 輔助方法
func (w *WAL) GetLastSeq() uint64

// 校驗和
func CalculateChecksum(eventType EventType, jobID string, seq uint64) uint32
func VerifyChecksum(event Event) bool

// 進階功能（選用）
func NewBatchWriter(wal *WAL, maxBatchSize int, flushInterval time.Duration) *BatchWriter
func ValidateWAL(path string) error
func GetWALStats(path string) (*WALStats, error)
```

---

## 🔗 與其他模組的依賴

### 被依賴（提供服務）

```
Controller
    ↓ 呼叫
   WAL
    ↓ 使用
檔案系統
```

### 協作模組

```
Snapshot ←→ WAL
    ↓        ↓
  State ←→ State
```

**協作流程**：

1. **正常執行**：Controller → WAL.Append() → 記錄事件
2. **快照時**：Controller → Snapshot.Write() + WAL.Rotate()
3. **恢復時**：Snapshot.Load() → State.Restore() → WAL.Replay()

---

## 📋 關鍵決策記錄

### 1. 檔案格式：JSON

**決策**：使用 JSON 作為 WAL 事件序列化格式

**理由**：

- ✅ 人類可讀，方便除錯
- ✅ Go 原生支援
- ✅ 容易擴展
- ❌ 空間效率較低（可接受）

**替代方案**：Protobuf, MessagePack（Phase 2 考慮）

---

### 2. 校驗和：CRC32

**決策**：使用 CRC32-IEEE 計算校驗和

**理由**：

- ✅ 快速計算
- ✅ 檢測隨機錯誤
- ✅ Go 標準庫支援
- ❌ 不防惡意篡改（可接受，非安全需求）

**替代方案**：SHA256（更安全但更慢）

---

### 3. 同步策略：每次 Sync

**決策**：預設每次 Append 都呼叫 fsync

**理由**：

- ✅ 保證持久性
- ✅ 崩潰不丟失資料
- ❌ 效能較低（~200 ops/s）

**優化方案**：提供 `BatchWriter` 選擇性批次 Sync

---

### 4. 旋轉策略：重置 Seq

**決策**：Rotate 後 seq 從 0 重新開始

**理由**：

- ✅ 簡化實作
- ✅ Snapshot 記錄 last_seq，有明確分界
- ❌ Seq 不具全域唯一性（可接受）

**替代方案**：全域遞增 seq（需要額外狀態管理）

---

### 5. 錯誤處理：嚴格模式

**決策**：Replay 遇到錯誤立即中止

**理由**：

- ✅ 保證資料完整性
- ✅ 發現問題立即告警
- ❌ 單個損壞事件導致無法啟動

**優化方案**：提供「寬容模式」跳過損壞事件（需使用者明確選擇）

---

## 🧠 實作提示

### 關鍵思考點

1. **NewWAL 的 Seq 初始化**

   - 如何高效讀取最後一個事件的 seq？
   - 檔案很大時如何避免全檔案掃描？

2. **Append 的原子性**

   - Encode 成功但 Sync 失敗怎麼辦？
   - 如何確保事件完整寫入？

3. **Replay 的冪等性**

   - 如何處理重複事件？
   - Handler 如何判斷事件是否已應用？

4. **Rotate 的安全性**

   - 如何確保原子替換？
   - Rotate 失敗如何恢復？

5. **並發控制**
   - 是否允許並發 Append？
   - Replay 期間是否允許 Append？

### 常見陷阱

❌ **忘記 Sync**：資料未持久化，崩潰時丟失  
✅ 每次 Append 後呼叫 `file.Sync()`

❌ **Checksum 不一致**：計算範圍與驗證範圍不同  
✅ 使用相同的欄位計算與驗證

❌ **非冪等 Replay**：重複執行導致錯誤  
✅ Handler 中檢查狀態，避免重複操作

❌ **Rotate 丟失資料**：舊檔案刪除前新檔案未建立  
✅ 先建立新檔案，再重新命名舊檔案

---

## 📊 測試涵蓋率目標

| 測試類型     | 目標涵蓋率 | 重點                    |
| ------------ | ---------- | ----------------------- |
| 單元測試     | > 80%      | 所有公開方法            |
| 錯誤處理測試 | 100%       | 所有錯誤分支            |
| 並發測試     | N/A        | 通過 `go test -race`    |
| 整合測試     | > 90%      | 與 Snapshot, Controller |

---

## 🚀 快速開始

### Step 1：實作基礎型別

```bash
# 編輯 types.go
vim internal/storage/wal/types.go
```

### Step 2：實作校驗和

```bash
# 編輯 checksum.go
vim internal/storage/wal/checksum.go

# 立即測試
go test -run TestChecksum
```

### Step 3：實作核心邏輯

```bash
# 編輯 wal.go
vim internal/storage/wal/wal.go

# 逐步測試
go test -run TestNewWAL
go test -run TestAppend
go test -run TestReplay
```

### Step 4：整合到系統

```bash
# 修改 controller.go
vim internal/controller/controller.go

# 執行整合測試
go test ./...
```

---

## 📚 延伸閱讀

- `README.md` - 使用說明與 API 文檔
- `integration_example.go` - Controller 整合範例
- `/docs/phase1-quick-reference.md` - WAL 設計理念
- PostgreSQL WAL 實作參考

---

**建立時間**：2024  
**維護者**：Beaver-Raft 團隊  
**版本**：1.0.0
