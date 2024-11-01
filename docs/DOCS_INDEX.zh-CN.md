# 文檔索引 | Documentation Index

> Beaver-Raft 完整文檔導覽

## 📚 文檔結構

### 🎯 新手入門（推薦閱讀順序）

1. **[README.md](README.md)** - 項目概覽與快速開始
2. **[USAGE_GUIDE.md](USAGE_GUIDE.md)** - 使用指南（命令、配置、監控）
3. **[QUICKSTART.md](QUICKSTART.md)** - 開發者實作指南

### 📋 完整參考

1. **[PHASE1_SUMMARY.md](PHASE1_SUMMARY.md)** - Phase 1 完整功能說明
2. **[IMPLEMENTATION_ORDER.md](IMPLEMENTATION_ORDER.md)** - 模塊實作順序與細節
3. **[TEST_COVERAGE_REPORT.md](TEST_COVERAGE_REPORT.md)** - 測試覆蓋率報告

### 🏗️ 架構與設計

1. **[docs/phase1-architecture.md](docs/phase1-architecture.md)** - 系統架構設計
2. **[docs/ai-notes.md](docs/ai-notes.md)** - AI 設計筆記與決策
3. **[docs/phase1-snapshot-aware-job-queue.md](docs/phase1-snapshot-aware-job-queue.md)** - Phase 1 技術深度剖析

### 🔮 未來規劃

1. **[docs/roadmap.md](docs/roadmap.md)** - 項目路線圖
2. **[docs/phase2-falconqueue.md](docs/phase2-falconqueue.md)** - Phase 2 設計
3. **[docs/phase3-beaver-raft.md](docs/phase3-beaver-raft.md)** - Phase 3 設計

## 🎓 按使用場景選擇文檔

### 場景 1：我想快速使用這個系統

閱讀順序：

1. [README.md](README.md) - 了解是什麼
2. [USAGE_GUIDE.md](USAGE_GUIDE.md) - 學會怎麼用
3. 運行 `make demo` - 看實際效果

時間：15 分鐘

### 場景 2：我想開發或修改代碼

閱讀順序：

1. [QUICKSTART.md](QUICKSTART.md) - 開發環境設置
2. [IMPLEMENTATION_ORDER.md](IMPLEMENTATION_ORDER.md) - 理解模塊結構
3. [docs/phase1-architecture.md](docs/phase1-architecture.md) - 理解設計
4. 查看源碼中的偽代碼註解

時間：1-2 小時

### 場景 3：我想深入理解系統設計

閱讀順序：

1. [docs/ai-notes.md](docs/ai-notes.md) - 設計決策
2. [docs/phase1-architecture.md](docs/phase1-architecture.md) - 架構設計
3. [docs/phase1-snapshot-aware-job-queue.md](docs/phase1-snapshot-aware-job-queue.md) - 技術細節
4. [PHASE1_SUMMARY.md](PHASE1_SUMMARY.md) - 完整總結

時間：2-3 小時

### 場景 4：我想了解測試情況

閱讀順序：

1. [TEST_COVERAGE_REPORT.md](TEST_COVERAGE_REPORT.md) - 測試報告
2. 查看 `internal/*/*_test.go` - 具體測試用例
3. 查看 `test/integration/` - 集成測試

時間：30 分鐘

## 📖 各文檔功能對照表

| 文檔 | 用途 | 適合人群 | 閱讀時間 |
|------|------|----------|----------|
| **README.md** | 項目介紹、快速開始 | 所有人 | 5 分鐘 |
| **USAGE_GUIDE.md** | 使用手冊、命令參考 | 用戶、運維 | 10 分鐘 |
| **QUICKSTART.md** | 開發指南、實作細節 | 開發者 | 30 分鐘 |
| **PHASE1_SUMMARY.md** | 功能總結、技術棧 | 技術人員 | 15 分鐘 |
| **IMPLEMENTATION_ORDER.md** | 實作順序、模塊說明 | 開發者 | 1 小時 |
| **TEST_COVERAGE_REPORT.md** | 測試覆蓋率、質量保證 | QA、開發者 | 10 分鐘 |
| **docs/phase1-architecture.md** | 架構設計、系統原理 | 架構師、開發者 | 1 小時 |
| **docs/ai-notes.md** | 設計思考、權衡分析 | 架構師 | 30 分鐘 |
| **docs/roadmap.md** | 未來規劃、演進路徑 | PM、架構師 | 15 分鐘 |

## 🔍 快速查找

### 我想知道

- **如何啟動系統？** → [USAGE_GUIDE.md](USAGE_GUIDE.md#快速啟動)
- **如何提交任務？** → [USAGE_GUIDE.md](USAGE_GUIDE.md#創建任務文件)
- **如何測試崩潰恢復？** → [USAGE_GUIDE.md](USAGE_GUIDE.md#測試崩潰恢復)
- **系統架構是什麼？** → [docs/phase1-architecture.md](docs/phase1-architecture.md)
- **如何實作一個模塊？** → [IMPLEMENTATION_ORDER.md](IMPLEMENTATION_ORDER.md)
- **WAL 是如何工作的？** → [docs/phase1-snapshot-aware-job-queue.md](docs/phase1-snapshot-aware-job-queue.md)
- **為什麼這樣設計？** → [docs/ai-notes.md](docs/ai-notes.md)
- **測試覆蓋率如何？** → [TEST_COVERAGE_REPORT.md](TEST_COVERAGE_REPORT.md)
- **未來會有什麼功能？** → [docs/roadmap.md](docs/roadmap.md)

## 📦 文檔更新記錄

- **2025-11-01**: 創建文檔索引
- **2025-10-31**: 完成 Phase 1 所有文檔
- **2025-10-30**: 添加測試覆蓋率報告
- **2025-10-29**: 優化 README 和使用指南

## 🤝 文檔貢獻

發現文檔問題或有改進建議？

1. 在 Issues 中提出
2. 提交 Pull Request
3. 聯繫維護者

## 📝 文檔規範

- 中英文混排使用空格分隔
- 代碼塊必須指定語言
- 標題使用有意義的描述
- 提供實際可運行的示例

---

**快速鏈接**：[使用指南](USAGE_GUIDE.md) | [開發指南](QUICKSTART.md) | [架構設計](docs/phase1-architecture.md)
