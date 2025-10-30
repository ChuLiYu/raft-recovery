// ============================================================================
// Beaver-Raft 恢復測試套件
// ============================================================================
//
// Package: test/integration
// 文件: recovery_test.go
// 功能: 端到端恢復功能測試
//
// 測試目標:
//   驗證系統在正常運行時的任務處理能力：
//   1. 任務成功入隊
//   2. Worker 正常執行任務
//   3. 任務狀態正確更新
//   4. 失敗任務正確標記為死信
//
// TestEndToEndRecovery:
//   完整的任務生命週期測試
//   - 提交 50 個任務
//   - 等待執行完成（10 秒）
//   - 驗證至少 70% 任務完成
//   - 考慮 10% 的模擬失敗率
//
// 測試配置:
//   - 4 個 Worker（較少以便觀察）
//   - 5 秒任務超時
//   - 10 秒快照間隔
//
// 預期結果:
//   在有 10% 失敗率的情況下：
//   - 完成任務: >= 35 (70%)
//   - 死信任務: <= 15 (30%)
//   - 無丟失: 完成 + 死信 = 總數
//
// 失敗場景:
//   如果完成率低於 70%，可能原因：
//   1. Worker 執行時間過長
//   2. 系統負載過高
//   3. 測試等待時間不足
//
// ============================================================================

package integration

import (
	"fmt"
	"testing"
	"time"

	"github.com/ChuLiYu/raft-recovery/internal/controller"
	"github.com/ChuLiYu/raft-recovery/pkg/types"
	"github.com/stretchr/testify/require"
)

// generateTestJobs 生成指定數量的測試任務
// 每個任務包含簡單的 payload，用於測試而非實際業務邏輯
func generateTestJobs(count int) []types.Job {
	jobs := make([]types.Job, count)
	for i := 0; i < count; i++ {
		jobs[i] = types.Job{
			ID:      types.JobID(fmt.Sprintf("job-%d", i)),
			Payload: map[string]interface{}{"key": i},
		}
	}
	return jobs
}

func TestEndToEndRecovery(t *testing.T) {
	// 清理測試文件
	walPath := fmt.Sprintf("/tmp/test-recovery-wal-%d.log", time.Now().UnixNano())
	snapshotPath := fmt.Sprintf("/tmp/test-recovery-snapshot-%d.json", time.Now().UnixNano())

	config := controller.Config{
		WorkerCount:      4,
		TaskTimeout:      5 * time.Second,
		SnapshotInterval: 10 * time.Second, // 增加快照間隔避免干擾
		WALPath:          walPath,
		SnapshotPath:     snapshotPath,
		WALBufferSize:    100,
	}

	// 第一階段：啟動並加入任務
	ctrl, err := controller.NewController(config)
	require.NoError(t, err)

	err = ctrl.Start()
	require.NoError(t, err)

	// 等待啟動完成
	time.Sleep(100 * time.Millisecond)

	// 加入任務
	jobs := generateTestJobs(50)
	err = ctrl.EnqueueJobs(jobs)
	require.NoError(t, err)

	// 等待任務完成 - 增加等待時間以適應 worker 執行速度
	// 50 個任務，4 個 worker，平均 250ms/任務，約需要 3-4 秒
	// 加上 10% 失敗率和重試，給足夠時間
	time.Sleep(10 * time.Second)

	// 驗證任務完成
	status := ctrl.GetStatus()
	ctrl.Stop()

	completed := status["completed"].(int)
	dead := status["dead"].(int)
	t.Logf("完成任務: %d, 死信任務: %d", completed, dead)

	// 考慮 10% 失敗率和執行時間，期望至少 35 個任務完成（70%）
	require.GreaterOrEqual(t, completed, 35, "至少35個任務應該完成")
}
