# Beaver-Raft: 可崩潰恢復的任務佇列系統

**[English](README.md)** | **中文** | **[語言指南](LANGUAGE.zh-CN.md)**

[![Go Version](https://img.shields.io/badge/Go-1.23-blue.svg)](https://golang.org/)
[![Tests](https://img.shields.io/badge/tests-passing-brightgreen.svg)](https://github.com/ChuLiYu/raft-recovery)
[![License](https://img.shields.io/badge/license-MIT-blue.svg)](LICENSE)

生產級、可崩潰恢復的任務佇列系統，支援 3 秒內恢復且零資料遺失。

> 📚 **[完整文檔導覽](DOCS_INDEX.zh-CN.md)** | 快速找到您需要的文檔

## ✨ 特性

- ⚡ **快速恢復**: 使用 WAL + Snapshot 實現 3 秒內崩潰恢復
- 📊 **高效能**: 吞吐量 ≥200 jobs/s
- 🔄 **零資料遺失**: Write-Ahead Log 確保持久性
- 📈 **可觀測性**: Prometheus 指標和即時監控
- 🎯 **簡單易用**: 易於使用的 CLI 介面

## 🚀 快速開始

```bash
# 一行命令看效果
make demo

# 或手動啟動
make build
./bin/beaver-raft run --workers 8

# 在另一個終端提交任務
./bin/beaver-raft enqueue --file test/jobs.json
```

## 📖 文檔

| 文檔 | 說明 |
|------|------|
| **[USAGE_GUIDE.zh-CN.md](USAGE_GUIDE.zh-CN.md)** | 🎯 快速使用指南 |
| **[QUICKSTART.zh-CN.md](QUICKSTART.zh-CN.md)** | 📘 開發者入門 |
| **[PHASE1_SUMMARY.zh-CN.md](PHASE1_SUMMARY.zh-CN.md)** | 📋 Phase 1 完整總結 |
| **[IMPLEMENTATION_ORDER.zh-CN.md](IMPLEMENTATION_ORDER.zh-CN.md)** | 🔢 模塊實作順序 |

### 架構文檔

- 🏗️ [Phase 1 架構](docs/phase1-architecture.md) - 系統設計
- 💡 [AI 筆記](docs/ai-notes.md) - 設計決策
- 📊 [Phase 1 詳細說明](docs/phase1-snapshot-aware-job-queue.md) - 技術深度剖析

## 🏗️ 架構

```text
┌─────────────────────────────────────────┐
│            Controller                    │
│  ┌──────────┐  ┌──────────┐  ┌────────┐│
│  │JobManager│  │Worker Pool│  │Metrics ││
│  └────┬─────┘  └─────┬────┘  └────────┘│
└───────┼──────────────┼─────────────────┘
        │              │
        ▼              ▼
  ┌──────────────────────────┐
  │    WAL + Snapshot         │
  │  (持久化存儲)              │
  └──────────────────────────┘
```

### 核心組件

- **Controller**: 協調 4 個核心循環（分派、結果、超時、快照）
- **JobManager**: 管理任務生命週期的狀態機
- **Worker Pool**: 並發任務執行器，支援超時控制
- **WAL**: Write-Ahead Log，確保操作持久性
- **Snapshot Manager**: 定期狀態快照，實現快速恢復

## 🛠️ 開發

```bash
# 安裝依賴
make install

# 建構
make build

# 執行測試
make test

# 執行基準測試
make bench

# 生成覆蓋率報告
make coverage

# 清理建構產物
make clean
```

## 📊 性能指標

| 指標 | 目標 | 狀態 |
|------|------|------|
| 恢復時間 | < 3秒 | ✅ |
| 吞吐量 | ≥ 200 jobs/s | ✅ |
| 資料遺失 | 零 | ✅ (WAL) |
| 並發安全 | 無競態 | ✅ (已測試) |

## 🎯 使用場景

- 背景任務處理
- 支援崩潰恢復的任務佇列
- 分散式任務調度（Phase 2+）
- 關鍵任務執行

## 🗺️ 路線圖

### Phase 1: Snapshot-Aware Job Queue ✅（當前）

- 基於 Goroutine 的 Worker
- WAL + JSON 快照
- 快速崩潰恢復
- Prometheus 指標

### Phase 2: FalconQueue（計劃中）

- 多節點部署
- HTTP RPC 通訊
- 服務註冊與心跳
- 可觀測性堆疊

### Phase 3: Beaver-Raft（未來）

- Raft 共識整合
- 分散式協調
- 部分快照優化
- 研究級架構

## 📝 使用範例

### 建立任務

```json
[
  {
    "id": "task-001",
    "payload": {"action": "process", "value": 42},
    "timeout_ms": 5000
  }
]
```

### 提交與監控

```bash
# 啟動伺服器
./bin/beaver-raft run --workers 8

# 提交任務
./bin/beaver-raft enqueue --file jobs.json

# 檢查狀態
./bin/beaver-raft status

# 查看指標
curl http://localhost:9090/metrics
```

### 測試崩潰恢復

```bash
# 1. 啟動伺服器
./bin/beaver-raft run &
PID=$!

# 2. 提交任務
./bin/beaver-raft enqueue --file test/jobs.json

# 3. 模擬崩潰
kill -9 $PID

# 4. 重新啟動 - 將自動恢復
./bin/beaver-raft run

# ✅ 未完成的任務會繼續處理
```

## 🧪 測試

```bash
# 單元測試
go test ./internal/...

# 整合測試
go test ./test/integration/...

# 競態檢測
go test -race ./...

# 特定模塊
go test -v ./internal/controller/
```

## 📂 專案結構

```text
beaver-raft/
├── cmd/queue/          # CLI 入口
├── internal/
│   ├── controller/     # 核心協調
│   ├── jobmanager/     # 狀態管理
│   ├── worker/         # 任務執行
│   ├── storage/
│   │   ├── wal/       # Write-Ahead Log
│   │   └── snapshot/  # 快照管理
│   ├── cli/           # 命令列介面
│   └── metrics/       # Prometheus 指標
├── test/
│   └── integration/   # 整合測試
├── docs/              # 文檔
└── scripts/           # 輔助腳本
```

## 🤝 貢獻

1. Fork 此儲存庫
2. 建立您的功能分支
3. 為您的變更添加測試
4. 確保 `make test` 通過
5. 提交 Pull Request

## 📄 授權

MIT License - 請參閱 [LICENSE](LICENSE) 文件

## 🙏 致謝

靈感來自分散式系統研究和生產級佇列系統：

- Raft 共識演算法
- Redis 佇列模式
- Kafka 日誌設計
- PostgreSQL WAL 架構

---

用 ❤️ 為可靠的分散式系統而建

**快速鏈接**: [使用指南](USAGE_GUIDE.zh-CN.md) | [開發指南](QUICKSTART.zh-CN.md) | [完整文檔](DOCS_INDEX.zh-CN.md)
