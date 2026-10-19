# OrbStack 操作與環境管理

本專案以 `./raftctl` 為統一入口。macOS 編輯原始碼，OrbStack Linux 容器負責工具鏈、編譯、測試與執行。無須在主機安裝 Go、protoc、GCC 或 Make。

## 首次使用

前置條件為 Git、已啟動的 OrbStack，以及它提供的 Docker CLI／Compose。腳本會檢查環境，不會自行安裝 Homebrew 套件或改變全域 Docker context。

```bash
git clone https://github.com/ChuLiYu/raft-recovery.git
cd raft-recovery
./raftctl doctor
./raftctl setup
./raftctl up
./raftctl enqueue test/jobs.json
./raftctl status
./raftctl logs
```

程式與 test probe 採同一來源建置；tools 映像包含完整 Go／CGO 與 protobuf 工具。所有依賴下載發生於 Linux 建置或工具容器中。初次執行需要網路，後續建置可使用引擎內快取。映像版本與設定以根目錄 Dockerfile、compose.yaml 為準。

原始碼改動後，重新執行 `./raftctl up` 會建置並更新服務。提交工作檔時傳入 JSON 路徑即可，CLI 在 Compose 網路內連線，不需要主機 Go 或對外公開 master 埠。

## 常用指令

| 指令 | 用途 |
| --- | --- |
| `./raftctl doctor` | 檢查 context、Linux 引擎、CPU 架構與 Compose |
| `./raftctl setup` / `build` | 建置 runtime 與 tools 映像，安裝容器內依賴 |
| `./raftctl up` | 啟動三個 master 和 worker |
| `./raftctl enqueue test/jobs.json` | 以容器 CLI 提交工作 |
| `./raftctl status` | 查看本專案服務狀態 |
| `./raftctl logs` | 查看本專案服務紀錄 |
| `./raftctl test` | Linux 完整測試、race、vet 與跨容器 E2E |
| `./raftctl e2e` | 只跑隔離的跨容器故障測試 |
| `./raftctl proto` | 以容器工具重新產生 protobuf Go bindings |
| `./raftctl down` | 移除服務與網路，保留資料卷 |
| `./raftctl reset --yes` | 移除本專案佇列資料，無法復原 |
| `./raftctl purge --yes` | 另移除本專案工具快取與專用映像 |

詳細支援參數以 `./raftctl help` 為準。`reset`／`purge` 不加 `--yes` 必須拒絕，且不會執行全域 `docker system prune`。`purge` 移除本專案的 Go build-cache volume 與具專案名稱的映像；引擎共用的 BuildKit layer cache 會保留，避免影響其他專案。刪除或移動原始碼目錄不會自動刪除 Docker named volumes；需要清空環境時，先執行對應清理指令。

## 資料、網路與隔離

- 三個 master 分別使用 `master1:50051`、`master2:50051`、`master3:50051` 作為固定成員身分，持久資料各自獨立。
- volume 位於 OrbStack Linux 引擎內，`down`／`up` 仍沿用資料。不可改動既有資料所屬的成員設定，或讓兩個節點共用同一資料卷。
- Go modules／build cache 留在容器環境；runtime 映像不包含 Go 編譯器。不掛載主機 Docker socket 到容器。
- 預設 Compose project 為 `raft-recovery`，映像為 `raft-recovery:local` 與 `raft-recovery-tools:local`。`RAFT_PROJECT_NAME` 同時決定日常服務、資料卷與映像名稱，便於管理不同副本。
- E2E 使用獨立的 `raft-recovery-e2e`，共用來源建置的映像，另建服務與資料卷。若 E2E 名稱和日常 project 相同，或測試 project 已有容器／資料卷／網路，腳本會拒絕執行，避免覆蓋既有環境。
- 預設明確使用 `orbstack` context，不依賴目前全域選擇。只有要在 Linux CI 等其他 Docker 引擎重用流程時，明確設定 `RAFT_DOCKER_CONTEXT=default`。

這是單一 Linux VM 中的多容器環境。它可以驗證容器程序故障、gRPC 網路通訊及資料卷恢復；不能由此推論跨實體主機、磁碟斷電或正式環境 SLA。

## 如何驗證

```bash
./raftctl test
```

同一入口會在 Linux 工具容器執行不重用測試結果的全套 Go 測試、race detector 和 vet，接著用真正三容器叢集與 worker 執行故障恢復案例。跨容器案例逐一核對工作 ID，找出目前可服務的 leader 才注入 SIGKILL，再驗證接手、重啟、去重與 quorum 限制。每一步失敗皆須以非零狀態結束。

GitHub Actions 使用同一個 `raftctl`，但跑在 GitHub 的 Linux Docker 引擎，不是使用者的 OrbStack。兩者的實際結果分開記錄；驗收見 [OrbStack 遷移報告](../reports/ORBSTACK_VALIDATION.zh-CN.md)，原始規格見[遷移計畫](../planning/ORBSTACK_MIGRATION_PLAN.zh-CN.md)。

## 常見問題

- `doctor` 無法連線：先確認 OrbStack 已啟動；不要因此在 macOS 安裝 Go。
- 修改來源但看到舊行為：執行 `./raftctl up` 或 `./raftctl setup` 重新建置映像；保留資料是預期行為，需要空白叢集才使用 `reset --yes`。
- 測試失敗：保留完整命令輸出，檢查 E2E 各服務的診斷日誌。成功結果不等於延遲／吞吐 benchmark。
- 需要 protobuf 再生：執行 `./raftctl proto`，再檢查 bindings diff；工具在容器內執行，只有預期的產生檔回到原始碼目錄。
- 主機工具清理：容器驗收之後才移除已授權的 Homebrew Go／protobuf。保留 OrbStack、Docker、Git，以及其他共用套件；不刪共用 `~/go` 或全域 Go cache。

官方參考：[OrbStack Docker／Compose](https://docs.orbstack.dev/docker/)、[Docker Compose 應用模型](https://docs.docker.com/compose/intro/compose-application-model/)。
