package main

// ============================================================================
// 職責說明：
// 1. CLI 應用程式入口點
// 2. 初始化並執行 CLI 命令
// 3. 處理頂層錯誤與 panic recovery
// ============================================================================

import (
	"fmt"
	"os"
	
	"github.com/ChuLiYu/beaver-raft/internal/cli"
)

// ============================================================================
// main 函式偽代碼
// ============================================================================

/*
func main():
  // 1. Panic recovery（防止整個程式崩潰）
  defer func() {
    if r := recover(); r != nil {
      fmt.Fprintf(os.Stderr, "嚴重錯誤: %v\n", r)
      os.Exit(1)
    }
  }()
  
  // 2. 建立 CLI
  rootCmd := cli.BuildCLI()
  
  // 3. 執行命令
  if err := rootCmd.Execute(); err != nil {
    fmt.Fprintf(os.Stderr, "錯誤: %v\n", err)
    os.Exit(1)
  }
  
  【簡潔原則】
  main.go 應該非常簡單，所有邏輯在 internal/cli
*/

// ============================================================================
// TODO（實作優先順序）
// ============================================================================

// TODO 1: 引入 cli 套件並呼叫 BuildCLI()
// TODO 2: 加入 panic recovery（生產環境必備）
// TODO 3: 考慮加入版本資訊（--version 旗標）

// ============================================================================
// 編譯與執行
// ============================================================================

/*
# 開發階段
go run cmd/queue/main.go run

# 編譯
go build -o bin/queue cmd/queue/main.go

# 執行
./bin/queue run --workers 8

# 交叉編譯（部署到 Linux）
GOOS=linux GOARCH=amd64 go build -o bin/queue-linux cmd/queue/main.go
*/

// ============================================================================
// 版本管理（可選）
// ============================================================================

/*
var (
  version = "dev"      // 由 CI 注入
  commit  = "unknown"
  date    = "unknown"
)

func main() {
  rootCmd := cli.BuildCLI()
  
  rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)
  
  rootCmd.Execute()
}

# 編譯時注入版本
go build -ldflags "-X main.version=1.0.0 -X main.commit=$(git rev-parse HEAD)"
*/

