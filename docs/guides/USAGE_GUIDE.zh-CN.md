# Beaver-Raft 使用指南

> 快速上手 Beaver-Raft 崩潰可恢復任務隊列系統

## 🚀 快速啟動

### 一行命令體驗完整功能

```bash
make demo
```

這會自動完成：構建 → 啟動 → 提交任務 → 模擬崩潰 → 自動恢復 → 驗證

### 手動啟動（3 步驟）

```bash
# 1. 構建
make build

# 2. 啟動服務器（終端 1）
./bin/beaver-raft run --workers 8

# 3. 提交任務（終端 2）
./bin/beaver-raft enqueue --file test/jobs.json
```

## 📋 系統要求

- Go 1.23+
- macOS / Linux
- 8GB+ RAM（推薦）

## 🎯 核心功能

| 功能 | 命令 | 說明 |
|------|------|------|
| 啟動服務器 | `./bin/beaver-raft run` | 8 個 worker 並發處理任務 |
| 提交任務 | `./bin/beaver-raft enqueue --file jobs.json` | 批量入隊任務 |
| 查看狀態 | `./bin/beaver-raft status` | 顯示系統運行狀態 |
| 查看指標 | `curl http://localhost:9090/metrics` | Prometheus 監控數據 |

## 📝 創建任務文件

創建 `my-jobs.json`：

```json
[
  {
    "id": "task-001",
    "payload": {"action": "process", "data": 42},
    "timeout_ms": 5000
  },
  {
    "id": "task-002",
    "payload": {"action": "notify", "user": "admin"},
    "timeout_ms": 3000
  }
]
```

提交：

```bash
./bin/beaver-raft enqueue --file my-jobs.json
```

## 🔧 配置選項

```bash
./bin/beaver-raft run \
  --workers 8 \                  # Worker 數量
  --snapshot-interval 30s \      # 快照間隔
  --task-timeout 30s \           # 任務超時
  --wal-path ./data/wal \        # WAL 路徑
  --snapshot-path ./data/snapshot  # 快照路徑
```

或使用配置文件 `config.yaml`：

```yaml
worker_count: 8
task_timeout: 30s
snapshot_interval: 30s
max_retry: 3
wal_path: ./data/wal
snapshot_path: ./data/snapshot
metrics_port: 9090
```

```bash
./bin/beaver-raft run --config config.yaml
```

## 🧪 測試崩潰恢復

```bash
# 1. 啟動並獲取 PID
./bin/beaver-raft run &
PID=$!

# 2. 提交任務
./bin/beaver-raft enqueue --file test/jobs.json

# 3. 等待處理
sleep 2

# 4. 模擬崩潰
kill -9 $PID

# 5. 重啟恢復
./bin/beaver-raft run

# ✅ 系統應在 3 秒內恢復，未完成任務繼續執行
```

## 📊 監控指標

訪問 `http://localhost:9090/metrics` 查看：

- `beaver_raft_jobs_enqueued_total` - 已入隊任務數
- `beaver_raft_jobs_completed_total` - 已完成任務數
- `beaver_raft_jobs_failed_total` - 失敗任務數
- `beaver_raft_recovery_time_seconds` - 恢復時間

## 🛠️ 開發命令

```bash
make help       # 查看所有命令
make build      # 構建二進制
make test       # 運行測試
make bench      # 性能測試
make coverage   # 生成覆蓋率報告
make clean      # 清理構建產物
```

## 🗂️ 數據存儲

```text
data/
├── wal/              # Write-Ahead Log
│   └── wal-*.log    # 操作日誌
└── snapshot/         # 系統快照
    └── snapshot.json # 狀態快照
```

## ⚡ 性能指標

- **恢復時間**: < 3 秒
- **吞吐量**: ≥ 200 jobs/s
- **數據持久化**: 零丟失（WAL 保證）
- **並發安全**: 通過 race detector 驗證

## 🐛 常見問題

**Q: 端口被占用？**

```bash
# 查看占用進程
lsof -i :9090

# 使用其他端口
./bin/beaver-raft run --metrics-port 9091
```

**Q: 權限錯誤？**

```bash
chmod +x ./bin/beaver-raft
chmod +x ./scripts/demo.sh
```

**Q: 任務一直 pending？**

檢查 worker 是否正常啟動：

```bash
./bin/beaver-raft status
```

## 📚 進階文檔

| 文檔 | 內容 |
|------|------|
| `QUICKSTART.md` | 實作細節與開發指南 |
| `docs/phase1-architecture.md` | 架構設計與原理 |
| `IMPLEMENTATION_ORDER.md` | 模塊實作順序 |
| `PHASE1_SUMMARY.md` | Phase 1 完整總結 |

## 🎓 學習路徑

1. **初學者**: `make demo` → 觀察輸出 → 理解流程
2. **使用者**: 閱讀本文檔 → 創建自定義任務 → 測試恢復
3. **開發者**: `QUICKSTART.md` → 閱讀源碼 → 運行測試
4. **架構師**: `docs/phase1-architecture.md` → 理解設計決策

## 🚦 系統架構（簡化版）

```text
                  ┌─────────────┐
                  │  Controller │
                  └──────┬──────┘
                         │
        ┌────────────────┼────────────────┐
        ▼                ▼                ▼
   ┌─────────┐    ┌───────────┐    ┌─────────┐
   │JobManager│    │Worker Pool│    │ Metrics │
   └────┬────┘    └─────┬─────┘    └─────────┘
        │               │
        ▼               ▼
   ┌─────────────────────────┐
   │    WAL + Snapshot       │
   └─────────────────────────┘
```

## 🎯 立即開始

```bash
# 克隆項目
git clone https://github.com/ChuLiYu/raft-recovery.git
cd raft-recovery

# 安裝依賴
make install

# 運行 Demo
make demo

# 🎉 開始使用！
```

## 📞 需要幫助？

- 查看測試用例：`internal/*/*_test.go`
- 查看完整文檔：`docs/` 目錄
- 查看實作筆記：`docs/ai-notes.md`

---

**Beaver-Raft** - 生產級崩潰可恢復任務隊列系統 🦫
