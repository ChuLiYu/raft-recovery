# 遷移指南：State → JobManager

## 概述

本專案已將 `internal/state` 模組重構為 `internal/jobmanager`，以更好地反映其職責。本指南說明如何將舊代碼遷移到新架構。

## 主要變更

### 1. 模組路徑變更

```diff
- import "github.com/ChuLiYu/beaver-raft/internal/state"
+ import "github.com/ChuLiYu/beaver-raft/internal/jobmanager"
```

### 2. 類型名稱變更

```diff
- state.State
+ jobmanager.JobManager

- state.NewState()
+ jobmanager.NewJobManager()
```

### 3. 檔案路徑變更

```diff
- internal/state/state.go
+ internal/jobmanager/job_manager.go

- internal/state/state_test.go
+ internal/jobmanager/job_manager_test.go
```

## 遷移步驟

### 步驟 1：更新 Import 語句

在所有使用 State 的檔案中：

```go
// 舊代碼
import "github.com/ChuLiYu/beaver-raft/internal/state"

// 新代碼
import "github.com/ChuLiYu/beaver-raft/internal/jobmanager"
```

### 步驟 2：更新變數宣告

```go
// 舊代碼
var s *state.State
s = state.NewState()

// 新代碼
var jm *jobmanager.JobManager
jm = jobmanager.NewJobManager()
```

### 步驟 3：更新方法呼叫

所有方法名稱保持不變，只需要更新接收者：

```go
// 舊代碼
s.Enqueue(job)
job := s.PopPending()
s.MarkInFlight(jobID, deadline)

// 新代碼
jm.Enqueue(job)
job := jm.PopPending()
jm.MarkInFlight(jobID, deadline)
```

### 步驟 4：更新測試檔案

```go
// 舊代碼
func TestStateOperations(t *testing.T) {
    s := state.NewState()
    // ...
}

// 新代碼
func TestJobManagerOperations(t *testing.T) {
    jm := jobmanager.NewJobManager()
    // ...
}
```

## 類型定義變更

### JobID 類型

`JobID` 現在是 `jobmanager.JobID` 類型別名：

```go
// 舊代碼
type Event struct {
    JobID string `json:"job_id"`
}

// 新代碼
import "github.com/ChuLiYu/beaver-raft/internal/jobmanager"

type Event struct {
    JobID jobmanager.JobID `json:"job_id"`
}
```

## 相容性說明

### 向後相容性

- ✅ 所有方法簽名保持不變
- ✅ 所有資料結構保持不變
- ✅ 所有 JSON 序列化保持相容
- ✅ 所有測試邏輯保持不變

### 需要更新的地方

- 🔄 Import 路徑
- 🔄 類型名稱
- 🔄 變數名稱（建議）

## 驗證遷移

### 1. 編譯檢查

```bash
go build ./...
```

### 2. 測試檢查

```bash
go test ./internal/jobmanager/
go test -race ./internal/jobmanager/
```

### 3. 覆蓋率檢查

```bash
go test -cover ./internal/jobmanager/
```

## 常見問題

### Q: 為什麼要重命名？

A: `State` 這個名稱太泛用，不能清楚表達其職責。`JobManager` 更準確地描述了它作為任務狀態管理器的角色。

### Q: 會影響效能嗎？

A: 不會。這只是重命名，沒有改變任何實作細節或資料結構。

### Q: 如何處理現有的快照檔案？

A: 快照檔案的 JSON 格式保持相容，不需要遷移現有資料。

## 範例：完整遷移

### 舊代碼

```go
package main

import (
    "github.com/ChuLiYu/beaver-raft/internal/state"
)

func main() {
    s := state.NewState()

    job := state.Job{ID: "test-job"}
    s.Enqueue(job)

    popped := s.PopPending()
    if popped != nil {
        s.MarkInFlight(popped.ID, time.Now().Add(time.Hour))
    }
}
```

### 新代碼

```go
package main

import (
    "github.com/ChuLiYu/beaver-raft/internal/jobmanager"
)

func main() {
    jm := jobmanager.NewJobManager()

    job := jobmanager.Job{ID: "test-job"}
    jm.Enqueue(job)

    popped := jm.PopPending()
    if popped != nil {
        jm.MarkInFlight(popped.ID, time.Now().Add(time.Hour))
    }
}
```

## 支援

如果在遷移過程中遇到問題，請：

1. 檢查本指南的範例
2. 查看 `internal/jobmanager/job_manager_test.go` 中的測試範例
3. 執行 `go test ./internal/jobmanager/` 驗證功能正常
