package cli

// ============================================================================
// 職責說明：
// 1. 定義 CLI 命令（enqueue, run, status）
// 2. 解析命令列參數與配置檔
// 3. 初始化並啟動 Controller
// 4. 處理系統訊號（SIGINT/SIGTERM）優雅關閉
// ============================================================================

import (
	"github.com/spf13/cobra"
	"os"
	"os/signal"
	"syscall"
)

// ============================================================================
// 命令定義偽代碼
// ============================================================================

/*
BuildCLI() *cobra.Command:
  rootCmd := &cobra.Command{
    Use: "queue",
    Short: "Beaver-Raft Phase 1 Job Queue",
  }
  
  rootCmd.AddCommand(
    buildEnqueueCmd(),
    buildRunCmd(),
    buildStatusCmd(),
  )
  
  return rootCmd
*/

// ============================================================================
// enqueue 命令偽代碼
// ============================================================================

/*
buildEnqueueCmd() *cobra.Command:
  cmd := &cobra.Command{
    Use: "enqueue --file jobs.json",
    Short: "加入任務到佇列",
    Run: func(cmd *cobra.Command, args []string) {
      // 1. 解析參數
      filePath := cmd.Flags().GetString("file")
      
      // 2. 讀取 JSON 檔案
      data, err := os.ReadFile(filePath)
        → 失敗: 顯示錯誤並退出
      
      var jobs []Job
      json.Unmarshal(data, &jobs)
        → 失敗: 顯示錯誤並退出
      
      // 3. 載入配置
      config := loadConfig()
      
      // 4. 建立 Controller
      ctrl, err := NewController(config)
        → 失敗: 顯示錯誤並退出
      
      // 5. 啟動（會載入快照與 WAL）
      ctrl.Start()
      
      // 6. 加入任務
      err = ctrl.EnqueueJobs(jobs)
        → 失敗: 顯示錯誤並退出
      
      fmt.Printf("成功加入 %d 個任務\n", len(jobs))
      
      // 7. 關閉
      ctrl.Stop()
    },
  }
  
  cmd.Flags().StringP("file", "f", "", "任務 JSON 檔案路徑")
  cmd.MarkFlagRequired("file")
  
  return cmd
  
  【測試場景】
    - 正常加入任務
    - 無效 JSON 格式報錯
    - 檔案不存在報錯
*/

// ============================================================================
// run 命令偽代碼
// ============================================================================

/*
buildRunCmd() *cobra.Command:
  cmd := &cobra.Command{
    Use: "run",
    Short: "啟動佇列處理器",
    Run: func(cmd *cobra.Command, args []string) {
      // 1. 載入配置
      config := loadConfig()
      
      // 2. 命令列旗標覆蓋配置
      if cmd.Flags().Changed("workers"):
        config.WorkerCount = cmd.Flags().GetInt("workers")
      
      if cmd.Flags().Changed("timeout"):
        config.TaskTimeout = cmd.Flags().GetDuration("timeout")
      
      // 3. 建立 Controller
      ctrl, err := NewController(config)
        → 失敗: 顯示錯誤並退出
      
      // 4. 啟動
      err = ctrl.Start()
        → 失敗: 顯示錯誤並退出
      
      fmt.Printf("✓ Controller 已啟動\n")
      fmt.Printf("  Workers: %d\n", config.WorkerCount)
      fmt.Printf("  超時: %v\n", config.TaskTimeout)
      fmt.Printf("  Metrics: http://localhost:%d/metrics\n", config.MetricsPort)
      
      // 5. 啟動 Metrics 伺服器
      go startMetricsServer(config.MetricsPort)
      
      // 6. 等待終止訊號
      sigCh := make(chan os.Signal, 1)
      signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
      
      <-sigCh  // 阻塞直到收到訊號
      
      fmt.Println("\n正在關閉...")
      
      // 7. 優雅關閉
      ctrl.Stop()
      
      fmt.Println("已關閉")
    },
  }
  
  cmd.Flags().IntP("workers", "w", 8, "Worker 數量")
  cmd.Flags().DurationP("timeout", "t", 3*time.Second, "任務超時時間")
  cmd.Flags().DurationP("snapshot", "s", 2*time.Second, "快照間隔")
  
  return cmd
  
  【測試場景】
    - 正常啟動並運行
    - Ctrl+C 優雅關閉
    - 旗標正確覆蓋配置
*/

// ============================================================================
// status 命令偽代碼
// ============================================================================

/*
buildStatusCmd() *cobra.Command:
  cmd := &cobra.Command{
    Use: "status",
    Short: "顯示佇列狀態",
    Run: func(cmd *cobra.Command, args []string) {
      // 1. 載入快照（不啟動 Controller）
      config := loadConfig()
      
      snapshot := NewSnapshotManager(config.SnapshotPath)
      data, err := snapshot.Load()
      
      if err != nil:
        fmt.Println("無法讀取狀態:", err)
        return
      
      // 2. 顯示統計
      fmt.Println("佇列狀態：")
      fmt.Printf("  待處理: %d\n", len(data.Queue))
      fmt.Printf("  執行中: %d\n", len(data.InFlight))
      fmt.Printf("  已完成: %d\n", len(data.Completed))
      fmt.Printf("  失敗: %d\n", len(data.Dead))
      
      if data.Timestamp > 0:
        t := time.Unix(data.Timestamp, 0)
        fmt.Printf("  快照時間: %s\n", t.Format("2006-01-02 15:04:05"))
      
    },
  }
  
  return cmd
  
  【測試場景】
    - 無快照時顯示空狀態
    - 正常顯示統計
*/

// ============================================================================
// 配置載入偽代碼
// ============================================================================

/*
loadConfig() Config:
  // 1. 預設配置
  config := Config{
    WorkerCount: 8,
    TaskTimeout: 3 * time.Second,
    SnapshotInterval: 2 * time.Second,
    MaxRetry: 3,
    WALPath: "./data/wal.log",
    SnapshotPath: "./data/snapshot.json",
    MetricsPort: 9090,
  }
  
  // 2. 從檔案載入（如果存在）
  configPath := os.Getenv("QUEUE_CONFIG")
  if configPath == "":
    configPath = "./configs/default.yaml"
  
  if fileExists(configPath):
    data, _ := os.ReadFile(configPath)
    yaml.Unmarshal(data, &config)
  
  // 3. 環境變數覆蓋
  if val := os.Getenv("QUEUE_WORKERS"):
    config.WorkerCount = parseInt(val)
  
  return config
  
  【優先順序】預設 < YAML < 環境變數 < 命令列旗標
*/

/*
startMetricsServer(port int):
  http.Handle("/metrics", promhttp.Handler())
  
  err := http.ListenAndServe(fmt.Sprintf(":%d", port), nil)
  if err != nil:
    log.Error("Metrics 伺服器錯誤", err)
  
  【測試】
  curl http://localhost:9090/metrics | grep queue_
*/

// ============================================================================
// TODO（實作優先順序）
// ============================================================================

// TODO 1: 實作 run 命令與訊號處理（核心功能）
// TODO 2: 實作 enqueue 命令與 JSON 解析
// TODO 3: 實作 status 命令與配置載入

// ============================================================================
// 測試重點
// ============================================================================

/*
TestEnqueueCommand:
  - 建立測試 JSON 檔案
  - 執行 enqueue 命令
  - 驗證任務被加入
  
TestRunCommand:
  - 在 goroutine 中執行 run
  - 發送 SIGINT
  - 驗證優雅關閉

TestStatusCommand:
  - 建立測試快照
  - 執行 status 命令
  - 驗證輸出正確

TestConfigPriority:
  - 設定 YAML, 環境變數, 旗標
  - 驗證優先順序正確
*/

// ============================================================================
// 使用範例
// ============================================================================

/*
# 加入任務
./queue enqueue --file jobs.json

# 啟動處理器
./queue run --workers 8 --timeout 5s

# 查看狀態
./queue status

# 自訂配置
QUEUE_WORKERS=16 ./queue run

# 指定配置檔
QUEUE_CONFIG=/etc/queue/config.yaml ./queue run
*/

