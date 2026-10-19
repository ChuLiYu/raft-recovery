# OrbStack 遷移驗收

驗收日期：2026-10-18（America/Vancouver）。遷移基準為 `c021d538`，實作與操作規格見[遷移計畫](../planning/ORBSTACK_MIGRATION_PLAN.zh-CN.md)，日常指令見[操作指南](../guides/ORBSTACK.zh-CN.md)。

## 環境與交付

- macOS 主機編輯原始碼；使用明確的 Docker `orbstack` context，Linux/aarch64 引擎、Docker 29.8.2、Compose v5.5.0。
- `Dockerfile` 固定 Go 1.25.1、protoc 33.4、protoc-gen-go v1.36.8、protoc-gen-go-grpc v1.5.1。runtime 與 tools 皆以一般使用者執行。
- 三個 master 各有獨立資料卷，worker 與 client 透過 Compose 網路使用 gRPC；未將 master 埠公開到主機。
- `./raftctl` 整合檢查、建置、啟動、工作提交、測試、protobuf 再生與清理。沒有新增 Go runtime 套件依賴。
- GitHub Actions 改為使用相同入口、`RAFT_DOCKER_CONTEXT=default`。CI 使用 GitHub Linux Docker 引擎，與本機 OrbStack 驗收分開記錄。

## 已完成的首次驗收

以下均在實際 OrbStack 引擎執行，未將 macOS 原生測試結果列作容器結果。

| 驗證 | 結果 |
| --- | --- |
| `./raftctl doctor`、`./raftctl setup` | 通過，Linux/aarch64 映像建置成功 |
| `go test -count=1 ./... -timeout 180s` | 通過，包含 integration |
| `go test -race -count=1 ./... -timeout 240s` | 通過，包含 integration |
| `go vet ./...`、`go build ./cmd/queue` | 通過 |
| `./raftctl test` 的跨容器 E2E | 通過，六個唯一 JobID 逐一驗證 |
| CLI 遠端提交結果回歸測試 | 容器內通過，涵蓋全數成功、部分拒絕與 RPC 失敗 |
| 腳本語法、`git diff --check` | 通過 |
| 無 `--yes` 的 reset、E2E 與日常 project 同名 | 拒絕執行，符合保護規格 |
| E2E project 已有網路 | 管理入口與直接 helper 均拒絕；驗證專用網路已移除 |

E2E 的六個 ID 分別覆蓋：真實 worker 成功 ACK、當前 leader 被 SIGKILL 後繼續完成工作、全叢集使用相同資料卷重啟後恢復兩個 pending 工作、少數派提交與 quorum 恢復後提交。驗證包含 payload、attempt、重複 ACK，以及跨重啟後完成 JobID 重送不再派工。少數派測試先確認保留節點仍回應，再檢查沒有正向提交回覆。

已完成工作不再派工的觀察窗為每組 4 秒，並非任意時間範圍的形式化證明。少數派 RPC timeout 的提交結果可能不確定；恢復 quorum 後使用相同 JobID 重試並驗證，不將 timeout 宣稱為寫入一定不存在。

## 驗收中修正的問題

1. 初版 tools 以 root 執行，讓無效路徑及唯讀目錄測試失去權限限制，造成兩個案例失敗。改為一般使用者後，全套測試通過；保留原斷言。
2. 初版 E2E 假設少數派 timeout 的工作不會稍後提交，與分散式寫入語意不符。調整為恢復 quorum 後以相同 ID 重試，再核對工作內容。
3. 原遠端 enqueue 即使部分或全部提交失敗仍回傳成功。現在保留成功／失敗數量，任何提交失敗皆回傳 error，並加入回歸測試。
4. E2E 清理只允許全新測試 project，包含已停止容器、資料卷與網路；未執行全域 prune。專案映像使用 project 名稱，避免清理其他副本的映像。

## 主機清理與最終重驗

首次完整容器驗收通過後，依使用者明確授權執行：

```bash
HOMEBREW_NO_AUTOREMOVE=1 /opt/homebrew/bin/brew uninstall go protobuf
```

已成功移除主機 Go 1.25.1 與 protobuf/protoc 33.4。未執行共用套件自動移除，也未刪除共用 `~/go` 或全域 cache。

移除後已確認 `command -v go` 與 `command -v protoc` 均找不到主機指令，並完成以下重驗：

| 命令／檢查 | 結果 |
| --- | --- |
| `./raftctl doctor` | exit 0，仍使用 OrbStack Linux/aarch64 |
| `./raftctl proto` | exit 0，兩個 Go bindings 沒有 diff；僅有既存未使用 timestamp import 警告 |
| `./raftctl test` | exit 0；完整 normal／race 測試、vet、build 與跨容器 E2E 全數通過 |
| `./raftctl up`、`./raftctl enqueue test/jobs.json` | 成功啟動；提交 5/5 工作 |
| `./raftctl status`、worker 日誌 | 三個 master 與 worker 皆 Up；`job-001` 至 `job-005` 各有 attempt 1、success=true ACK |
| `./raftctl down` 與 volume inspect | exit 0；容器與網路移除，三個 master 資料卷保留 |

最終 E2E ID 前綴為 `orb-e2e-*`，該次唯一識別尾碼為 `20261019T022811Z-7901`（UTC）；normal 與 race 的 integration package 分別完成於 54.351s 與 50.114s，僅為當次測試耗時。完整本機輸出存於 `/private/tmp/raft-orbstack-final-validation.log`，屬本機暫存紀錄，未當作可攜交付檔案。

最終重驗開始前，保護機制曾拒絕先前工具檢查留下的 `raft-recovery-e2e_default` 空網路；確認是本次驗證資源後只移除該網路，再由預設入口完整重跑成功。驗收結束後日常服務保持停止，保留 `raft-recovery_master1-data`、`raft-recovery_master2-data`、`raft-recovery_master3-data` 與 `raft-recovery_tools-cache`，方便下次啟動；未執行全域容器、映像或快取清除。

## 驗證邊界

這是單一 OrbStack Linux VM 中的三容器叢集，涵蓋程序故障、跨容器 RPC、volume 恢復與少數派限制。尚未驗證跨實體主機、磁碟斷電、網路分割矩陣、長時間壓力或效能 SLA。GitHub Actions 結果以 PR 的實際 check 為準，不因本機通過而推定 CI 通過。
