# Phase 3 實作盤點與交付規格

[English](PHASE3_DELIVERY.md) · [技術／面試筆記](INTERVIEW_GUIDE.zh-CN.md) · [啟動叢集](../README.zh-CN.md)

盤點日期：2026-10-18。基準：`ChuLiYu/raft-recovery`，main commit `c6a8c283431e8cc9ec9aa9324e4344734fbcd9db`。本文件把原始 roadmap／各階段計畫與可執行程式逐一對照。原計畫保留為歷史文件，其中未勾選的方框不是目前進度表。

## 這次交付到哪裡

本次完成到 Phase 3 的功能核心：具持久化的單機工作佇列、gRPC 遠端 worker，以及固定成員叢集的自製 Raft 選舉／複寫。Raft 程式就在此 repo，沒有把共識交給現成函式庫。

原始 roadmap 還包含研究級 Beaver 部分快照、部署練習與效能目標，這些**並未全部完成**。本次採用保留完成去重資訊的 queue checkpoint，以及原子 Raft 狀態 image，而非原計畫的分段 WAL。下文列明差異與剩餘工作；恢復測試通過不代表正式環境 SLA 或形式化正確性證明。

## 原規劃 → 實際程式

| 範圍 | 原始缺口 | 已交付行為 | 證據 |
|---|---|---|---|
| 單機持久化 | WAL 無法完整還原 payload；sequence／重播與錯誤處理不完整 | 完整 job 的版本化事件、payload checksum、同步寫入確認、重啟 sequence 恢復、損壞後拒絕新寫入 | `internal/storage/wal/durability_test.go`、WAL 測試 |
| 單機快照 | 終態去重、FIFO 可能丟失；切檔與 mutation 競爭 | 保存 FIFO、active、完整 DLQ、完成 tombstone；序列化快照與切檔邊界 | JobManager／controller 測試 |
| 遠端執行 | CLI 整合壞掉；failover 重連與 attempt 身分不完整 | 可運行 CLI、多 seed 提交／拉取、註冊／心跳、worker＋attempt ACK fencing、乾淨關閉 | CLI／worker 測試、程序故障 demo |
| 選舉 | 缺日誌新舊判斷與持久狀態 | 隨機 timeout、term／vote 持久化、last term/index 投票、較高 term 退位 | `TestVoteRequiresFreshLogAndPersists` |
| 複寫 | 記憶體 log；提交與錯誤路徑不完整 | 持久化 append、prefix 檢查、衝突修復、每 peer 序列複寫、當前 term 提交規則與 leader no-op | `TestPartitionCommitConflictRepair`、`TestAppendEntriesCommitsOnlyMatchedPrefix` |
| 成功回覆 | 可能把接受提案當成成功 | 等待多數 commit、檢查提案身分、等待本機 FSM 按序 apply | Raft proposal replacement 測試、controller apply 測試 |
| 佇列共識 | 派工／逾時仍由本機修改 | ENQUEUE、DISPATCH、ACK、TIMEOUT 走同一確定性 FSM | `internal/jobmanager/replicated_test.go` |
| Raft 快照 | 缺完整持久化 InstallSnapshot 路徑 | snapshot index/term 與壓縮 log 一起持久化；安裝快照後追趕後綴 | `TestSingleNodeDurableRestartSnapshot`、`TestSnapshotCatchup` |
| 叢集生命週期 | CLI 沒組裝 durable Raft cluster | 明確靜態 peers、獨立資料目錄、可路由 ID、transport/server 關閉 | 真實 gRPC integration、`scripts/demo-three-node-failover.sh` |
| 文件與主張 | README 包含無證據的效能／exactly-once 宣稱 | 可執行三節點命令、誠實限制、架構／面試筆記、CI | README、雙語交付文件／面試筆記 |

## 驗收契約

以下定義目前行為，也是之後改動應維持的條件。

1. **成功寫入的持久性：** master 模式回覆成功前，指令已由配置成員的多數提交，且服務節點已套用。少數派不能承認新提交。timeout 的結果不確定，client 用相同邏輯 JobID 重送。
2. **共識身分須持久化：** 正向投票／複寫確認前，term、vote、log／snapshot 已寫入持久層。存檔失敗停止正向進展。提案由 index＋term 識別，替換後的 entry 不能滿足舊 waiter。
3. **只有一條狀態變更路徑：** 只有已 apply 的命令能變更 replicated job state。leader 視需求寫入 payload、時間、worker、attempt、deadline、重試政策；followers 不依自己時鐘另做轉移。
4. **派工歸屬：** DISPATCH 選擇 pending FIFO head 並增加 attempt。ACK／TIMEOUT 必須符合目前派工。被拒絕的舊轉移不得改變新派工；已完成且身分相符的 ACK 重送可冪等成功。
5. **恢復等價：** snapshot＋已提交後綴須還原 FIFO、active／in-flight、DLQ、完成去重及 last-applied 邊界。安裝快照須先持久化才能成功；啟動先還原快照，再 apply 已提交後綴。
6. **client 責任：** JobID 代表邏輯操作，重送必須固定。拿相同 ID 換 payload 不會取代舊工作。worker 的外部副作用為 at least once；業務冪等由 executor／下游負責。
7. **拓樸與磁碟歸屬：** 所有節點採相同完整靜態 peer list，包含自己，並使用不同資料目錄。重啟維持相同 ID、membership、目錄。ID 是可到達的 peer 位址。一個資料目錄只能有一個程序，之後仍需加入自動鎖定檢查。

follower 回覆 leader 提示，client 也會輪替 seed。worker lease 是可重建的本機狀態；lease 到期不會繞過已派工作業的 replicated TIMEOUT。

## 重要決策與規劃差異

| 決策 | 原因與效果 | 代價／後續 |
|---|---|---|
| 自製 Raft、固定 membership | 滿足選舉／共識程式要求，讓成員規則可檢視 | 無 joint consensus／自動換節點；正式使用前仍需更多驗證 |
| 派工、逾時也複寫 | 歸屬與重試結果必須能跨 leader 故障保存 | 每個 job 產生更多 log；批次化仍須守住 commit/apply 契約 |
| 註冊／heartbeat 是 soft state | 重新註冊即可恢復連線，不需持久化每次 heartbeat | 與原 REGISTER_WORKER 入 log 計畫不同；真正派工身分仍持久化 |
| 原子 JSON Raft image | 同一 checksum／原子檔案邊界涵蓋 metadata、retained log、snapshot | 每次 mutation 成本 O(retained log＋snapshot bytes)；尚未滿足高效 random-access／分段 Raft WAL 計畫 |
| 完整 pending／in-flight／DLQ＋完成 tombstone | 只留 cursor 無法在 prefix 壓縮後還原工作；刪光完成 ID 會破壞去重 | 沒有 tombstone 上限，快照大小取決於 workload |
| queue checkpoint，非完整 Beaver 協定 | 讓實際恢復不變量明確、可測 | 未實作外部流量的協調 partial snapshot、replay fingerprint 研究或 geo-latency 實驗 |
| 未完成 WAL 管理操作明確回錯 | repair／truncate／compare 不能空做卻回成功 | 管理修復工具與未使用的 batch-writer helper 尚非完整公開功能 |

Raft 寫入順序：暫存檔→file fsync→close→rename→directory fsync。單機恢復採自己的 WAL＋snapshot，master 則以 Raft image 為權威；不可把兩條路徑重複重播進同一 FSM。

## 驗證紀錄

執行環境：macOS arm64、Go 1.25.1；module 宣告 Go 1.23。受限環境的 socket 測試需獲准綁定本機埠；CI runner 以可使用 loopback 的環境執行可重現檢查。

| 檢查 | 結果 |
|---|---|
| `go test ./... -timeout 120s` | 全部 package 通過；integration package 約 50 秒 |
| Raft 重複測試（`-count=5`）及 Raft race 重複（`-race -count=3`） | 通過 |
| CLI／worker／controller race | 最終全套執行前已通過 |
| 真實 TCP/gRPC 的 `TestRaftClusterDurableFailover` | 通過；涵蓋 leader 故障、replica 重啟、全節點重啟、快照去重、少數派不能提交 |
| `scripts/demo-three-node-failover.sh` | 通過；提交六筆固定 ID 工作、SIGKILL 當選 leader、六筆皆觀察到 ACK |
| `go test -race ./... -timeout 180s` | 全部 package 通過；integration package 50.664 秒 |
| `go vet ./...`、`go build -o /private/tmp/raft-recovery-phase3-queue ./cmd/queue` | 通過 |
| `git diff --check`、文件本機連結驗證 | 通過；README、索引、交付文件、面試筆記的連結皆可解析 |
| 最後的取消 RPC 結果清理：controller／server 一般與 race 測試 | 通過；回歸測試涵蓋取消後才 apply 已提交命令，以及成功請求取走結果 |
| 最終 `go test -race ./test/integration -run '^TestRaftClusterDurableFailover$' -count=3 -timeout 120s` | 三次真實 gRPC 測試皆通過，共 10.174 秒；修補後的 vet、build、gofmt、diff 檢查亦通過 |

獨立審查找到取消 RPC 後回覆結果可能殘留的問題。現在用 request-owned response ID，在 proposal 前登記，失敗就釋放、成功就取走；晚到 apply 不保存已無人等待的結果。第二次針對生命週期的審查未發現缺陷。上述全套測試在這項最後修補前執行；最終程式已跑 controller／server 一般與 race 測試，叢集路徑驗證另列。

首次全套 race 被 sandbox 的 loopback bind 限制阻擋，允許 socket 後執行相同 suite 通過。macOS linker 出現 LC_DYSYMTAB 警告，但編譯與測試成功。測試驗證具體安全性案例，不是 exhaustive model check。demo 及測試執行秒數也不是恢復延遲／吞吐 benchmark。原始 <3s failover、<100ms replication lag、恢復快 30–50%、快照小 ≥40% 仍是未驗證目標。

## 還缺什麼，建議優先順序

| 優先 | 缺少能力 | 影響 | 建議驗收 |
|---|---|---|---|
| P1 | queue、歷史 job、tombstone 容量上限 | 歷史資料與記憶體／磁碟持續成長 | 定義容量、背壓、去重期限；期限內重送不失去冪等 |
| P1 | data-dir lock、持久化 cluster identity | 可能誤用同目錄或不安全地改 membership | 第二程序拒啟；node／cluster／membership 不符須清楚拒啟 |
| P1 | live leader／term／lag／commit／apply 指標與查詢 API | 現有 metrics、offline status 未完整呈現即時叢集 | 可觀察 failover、commit/apply lag；定義讀取一致性，線性讀需 ReadIndex／barrier |
| 對外部署前 P1 | TLS、認證授權、備份恢復程序 | demo 假設可信網路與 quorum 磁碟仍在 | 未授權 peer/client 被拒；憑證輪替、restore drill；避免誤建 split cluster |
| P2 | 分段 Raft WAL、group commit、索引儲存、快照門檻 | 整份 image 寫入放大；目前 interval 快照，非原規劃 log-byte threshold | append／rotation／compaction 崩潰注入；量測寫入放大及 p95 |
| P2 | 可重現 benchmark、dashboard | 原 KPI 沒有可比較 baseline | 記錄硬體／runtime、payload／active 比例、seed、重複數、p50/p95/p99；相同恢復契約比較完整與精簡快照 |
| P2 | DLQ inspect／requeue 與管理操作 | 保存 dead letter 但補救工具有限 | leader 驗證的可稽核 requeue 指令，定義 retry／ID；不可直接改本機 FSM |
| P2 | 磁碟故障注入、長期 chaos、model checking | 現有測試只涵蓋代表性 schedule | ENOSPC／fsync／rename 錯誤、並發 poll／ACK／failover soak、FSM／共識不變量驗證 |
| P3 | 動態成員、pre-vote／check-quorum、快照傳輸優化 | 超出固定本機叢集的營運／可用性 | joint consensus、learner 追趕、取消／背壓、分塊 snapshot |
| 研究延伸 | Beaver 重現、geo-latency 實驗 | 完成原研究里程碑前仍必要 | 定義外部流量假設、一致 partial cut／重播證明、可比較 baseline、可重現實驗 |

Phase 3 功能核心可運行，不代表效能目標、研究延伸和正式環境強化也完成。[面試筆記](INTERVIEW_GUIDE.zh-CN.md)提供能準確表達這些取捨的說法。

## 參考與閱讀順序

1. [README](../README.zh-CN.md)：建置、叢集啟動與故障展示。
2. [面試筆記](INTERVIEW_GUIDE.zh-CN.md)：架構、請求生命週期、安全規則與準備題目。
3. `internal/raft/raft.go`、`rpc.go`、`persistent.go`；`internal/jobmanager/replicated.go`；`internal/controller/controller.go`。
4. [原 Phase 3 計畫](planning/PHASE3_IMPLEMENTATION_PLAN.md)、[階段規格](phases/phase3-beaver-raft.md)、[roadmap](../roadmap.md)。
5. [Raft paper](https://raft.github.io/raft.pdf)、[Beaver, OSDI 2024](https://www.usenix.org/conference/osdi24/presentation/yu)。
