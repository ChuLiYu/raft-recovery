// ============================================================================
// Beaver-Raft 隊列系統 - 主程序入口
// ============================================================================
//
// 文件: cmd/queue/main.go
// 功能: 應用程序啟動入口，初始化 CLI 命令行界面
//
// 職責說明:
//   1. 版本信息管理 - 在編譯時注入版本、提交哈希、構建日期
//   2. Panic 恢復 - 捕獲未預期的 panic，防止程序異常退出
//   3. CLI 初始化 - 構建並配置 Cobra 命令行界面
//   4. 錯誤處理 - 統一處理命令執行錯誤，返回適當的退出碼
//
// 版本注入:
//   這些變量在編譯時通過 -ldflags 注入:
//   go build -ldflags "-X main.version=1.0.0 -X main.commit=abc123"
//
// 使用方式:
//   ./beaver-raft --help              # 顯示幫助信息
//   ./beaver-raft --version           # 顯示版本信息
//   ./beaver-raft run                 # 啟動隊列系統
//   ./beaver-raft enqueue -f jobs.json # 提交任務
//   ./beaver-raft status              # 查看系統狀態
//
// ============================================================================

package main

import (
	"fmt"
	"os"

	"github.com/ChuLiYu/raft-recovery/internal/cli"
)

// 版本信息變量 - 在編譯時通過 ldflags 注入
// 例如: go build -ldflags "-X main.version=1.0.0"
var (
	version = "1.0.0"   // 語義化版本號
	commit  = "dev"     // Git 提交哈希
	date    = "unknown" // 構建日期
)

// main 是程序的入口函數
// 負責初始化 CLI、處理 panic 和執行命令
func main() {
	// 全局 panic 恢復機制
	// 作用: 防止未捕獲的 panic 導致程序異常退出
	// 場景: 代碼深層調用中的邏輯錯誤、nil 指針引用等
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "Fatal error: %v\n", r)
			os.Exit(1) // 返回非零退出碼表示異常
		}
	}()

	// 構建 CLI 命令樹
	// 包括 run、enqueue、status 等子命令
	rootCmd := cli.BuildCLI()

	// 設置版本信息，顯示在 --version 輸出中
	// 格式: "1.0.0 (commit: abc123, built: 2025-10-31)"
	rootCmd.Version = fmt.Sprintf("%s (commit: %s, built: %s)", version, commit, date)

	// 執行命令行解析和相應的業務邏輯
	// 如果命令執行失敗，返回錯誤並退出
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintf(os.Stderr, "Error: %v\n", err)
		os.Exit(1) // 命令執行失敗，返回錯誤碼 1
	}

	// 正常退出，返回碼 0
}
