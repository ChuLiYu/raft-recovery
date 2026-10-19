# OrbStack 遷移規格與執行計畫

日期：2026-10-18。基準：`codex/readme-architecture` / `c021d538`。

## 目的與邊界

macOS 僅保留原始碼、Git、OrbStack／Docker CLI 與管理腳本。Go 建置、測試、模組下載、程式執行、資料與快取全部交由 OrbStack 的 Linux 容器管理。沿用固定三節點 Raft，這次不改共識或佇列語意。這是單一 OrbStack VM 上的容器整合環境，不代表多實體主機或斷電耐久驗證。

採 Docker Compose，而非手動安裝一台 Linux 開發機：服務、網路、映像與資料卷可按專案辨識，停止和重建也有一致入口。主機已安裝 OrbStack，腳本不需再次安裝它，也不應自行安裝 Homebrew 套件。

## 操作契約

唯一使用者入口：根目錄 `./raftctl`。Dockerfile、Compose 設定和測試 helper 是內部實作檔，不要求使用者記住多套指令。

| 指令 | 行為 |
| --- | --- |
| `doctor` | 確認 Docker context、Linux 架構、引擎和 Compose 可用；預設明確使用 orbstack，不切換全域 context |
| `setup` / `build` | 在 Linux 建置 runtime 與 tools 映像；模組下載與 C toolchain 均在容器內 |
| `up` | 建置並啟動三個 master 和 worker；每個 master 有獨立持久卷 |
| `status` / `logs` | 顯示本專案容器狀態／日誌 |
| `enqueue FILE` | 以容器內 CLI 提交主機上的 JSON 工作檔案；不用主機 Go |
| `test` | 不使用快取測試結果，執行完整 Go 測試、race、vet、建置驗證及跨容器 E2E |
| `e2e` | 在獨立 Compose project／資料卷執行故障恢復測試，不碰日常叢集資料 |
| `proto` | 使用容器內固定版本的 protoc 與 Go plugins，重新產生已納入版本管理的 bindings |
| `down` | 停止並移除本專案容器和網路，保留資料卷 |
| `reset --yes` | 明確破壞性操作：只刪本專案資料，不做全域 prune |
| `purge --yes` | 清除本專案容器、資料、工具快取及專用映像；不刪其他專案資源 |

Docker context 預設 `orbstack`；CI 可透過明確變數改用 `default`。不掛載 Docker socket 到容器、不做 privileged 容器、不共用各 master 資料目錄。不必將 master 埠公開到主機：操作由容器內 client 走 Compose DNS 進行，避免 leader 提示回傳容器內名稱造成主機 CLI 錯誤。

## 檔案與責任

- 主代理：本規格、README 操作說明、docs 導覽、驗收報告、主機清理決策與最終 PR。
- 低消耗實作代理 A：`raftctl`、`Dockerfile`、`compose.yaml`、`.dockerignore`、容器專用設定。
- 低消耗實作代理 B：`scripts/orbstack-e2e.sh`、`test/e2e/` gRPC probe；不得新增 runtime 套件依賴。
- 低消耗複查代理：檢查命令失敗傳遞、volume 隔離、測試是否真的注入故障、清理是否限定本專案；不更動共識程式。

工具映像固定 Go 版本，支援 Linux 原生 arm64 與 CI amd64。runtime 不包含 Go 編譯器；支援 protobuf 再生的工具也放在 tools 映像內，版本明確。來源複製進 image，測試產物不寫回主機。執行日誌可匯出供審查，但不能在報告中把秒數當作效能 KPI。

驗收期間補充的實作決策：runtime 與 tools 使用一般使用者，保留既有檔案權限測試的有效性。E2E 開始前檢查既有容器、資料卷與網路，避免重用後清除既有資源。遠端 enqueue 若有工作提交失敗，CLI 必須回傳非零狀態，讓整合入口能正確判定失敗；此修正不改變共識或工作狀態轉移。

## 驗收條件

1. 語法／設定檢查：腳本語法、Compose config、建置與 doctor 皆成功。
2. Linux 全套：`go test -count=1 ./... -timeout 180s`、`go test -race -count=1 ./... -timeout 240s`、`go vet ./...`、container build。工具映像須支援 CGO/race。
3. 跨容器 E2E：三個獨立 master、有真實 worker、以確定 JobID 提交並逐一確認 ACK；不能只比對日誌總數。
4. 找出目前可服務的 leader，再對該容器 SIGKILL；提交後續工作，確認新 leader 多數提交且 worker 恢復。不能只殺固定節點或依賴舊選舉日誌。
5. 重啟原 leader；全叢集停止並以相同 volumes 重啟。確認已接受但未派出的工作保留，以及已完成 JobID 重送不會再次派工。用 gRPC probe 進行明確斷言，失敗回非零 exit code。
6. 少數派案例：保留單一 master 時不能承認新寫入；恢復 quorum 後可繼續。既有 Go integration 的快照、fencing 與少數派案例仍在 Linux 執行。
7. `down`／`up` 保留資料；`reset` 沒有 `--yes` 必須拒絕；E2E 和清理不影響原有 basecamp、Postgres 等容器。
8. 最後移除已獲使用者同意的主機 Go 1.25.1 與 protobuf/protoc 33.4，再執行容器 smoke 驗證，證明主機無 Go／protoc 也可工作。

## 主機清理

唯讀盤點發現 Go 與 protobuf 皆早於本次任務安裝，目前沒有已安裝 Homebrew 套件依賴它們。使用者已明確選擇兩者一併移除，之後在容器使用。清理須在容器驗收之後；不擅自移除 abseil、cmake、Git、Docker、OrbStack 或修改共用 shell 設定。沒有辨識到專案專用的主機 Go binary/cache 時，不刪共用 `~/go` 或 Go cache。

## 執行順序

1. 完成此規格與環境盤點（先規劃）。
2. 按上述 ownership 平行實作管理入口和 E2E（再遷移）。
3. 在實際 OrbStack 執行建置、Linux 測試、跨容器恢復及管理操作驗收，修正失敗。
4. 複查後清理主機工具，重驗無主機 Go 的流程。
5. 文件記錄精確命令、環境、通過／失敗及尚未驗證範圍，提交可審查 PR，不自行合併。
