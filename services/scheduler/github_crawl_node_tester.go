package scheduler

import (
	"fmt"
	"sync/atomic"

	"sublink/models"
	"sublink/utils"
)

// StartGitHubCrawlNodeTestTask 将 GitHub 独立节点全测注册到系统任务中心并异步执行。
// 返回 taskID；失败时 error 非空。
// onResult 在每条结果回调时调用（用于写独立节点表），可为 nil。
func StartGitHubCrawlNodeTestTask(
	configID int,
	configName string,
	items []LinkTestItem,
	profileID int,
	mode LinkTestMode,
	onResult func(LinkTestResult),
) (string, error) {
	if len(items) == 0 {
		return "", fmt.Errorf("没有可测节点")
	}
	if mode == "" {
		mode = LinkTestModeFull
	}

	cfg, profileName := ResolveSpeedTestConfig(profileID)
	tm := getTaskManager()
	if tm == nil {
		return "", fmt.Errorf("任务管理器未初始化")
	}

	name := configName
	if name == "" {
		name = fmt.Sprintf("GitHubCrawl_%d", configID)
	}
	if profileName != "" {
		name = fmt.Sprintf("GitHub全测 · %s · %s", name, profileName)
	} else {
		name = fmt.Sprintf("GitHub全测 · %s", name)
	}

	total := len(items)
	// mihomo 全测会有延时+测速两阶段回调，进度按「结果回调次数」估算，总数先按节点数
	task, ctx, err := tm.CreateTask(models.TaskTypeGitHubCrawl, name, models.TaskTriggerManual, total)
	if err != nil {
		return "", fmt.Errorf("创建任务失败: %w", err)
	}
	taskID := task.ID

	go func() {
		defer func() {
			if r := recover(); r != nil {
				utils.Error("[GitHubCrawlTest] panic: %v, task=%s, config=%d", r, taskID, configID)
				_ = tm.FailTask(taskID, fmt.Sprintf("任务异常崩溃: %v", r))
			}
		}()

		var done int32
		sum := RunLinkTests(ctx, items, cfg, mode, func(result LinkTestResult) {
			if onResult != nil {
				onResult(result)
			}
			n := int(atomic.AddInt32(&done, 1))
			// 全测可能 1 节点多次回调，进度不超过 total；超过后扩展 total 以继续显示推进
			progress := n
			if progress > total {
				_ = tm.UpdateTotal(taskID, n)
				progress = n
			}
			itemName := result.Name
			if itemName == "" {
				itemName = fmt.Sprintf("#%d", result.ID)
			}
			statusBits := result.DelayStatus
			if result.SpeedStatus != "" {
				statusBits = result.DelayStatus + "/" + result.SpeedStatus
			}
			_ = tm.UpdateProgress(taskID, progress, itemName, map[string]any{
				"nodeId":      result.ID,
				"delayTime":   result.DelayTime,
				"delayStatus": result.DelayStatus,
				"speed":       result.Speed,
				"speedStatus": result.SpeedStatus,
				"isValid":     result.IsValid,
				"status":      statusBits,
				"configId":    configID,
				"mode":        string(mode),
			})
		})

		// 被取消
		if ctx != nil {
			select {
			case <-ctx.Done():
				utils.Info("[GitHubCrawlTest] 任务已取消 task=%s config=%d done=%d", taskID, configID, atomic.LoadInt32(&done))
				// CancelTask 通常已更新状态；兜底写一次进度
				_ = tm.UpdateProgress(taskID, int(atomic.LoadInt32(&done)), "已取消", nil)
				return
			default:
			}
		}

		msg := fmt.Sprintf("全测完成 (成功: %d, 失败: %d, 共 %d)", sum.Success, sum.Failed, sum.Total)
		utils.Info("[GitHubCrawlTest] %s config=%d profile=%s task=%s", msg, configID, profileName, taskID)
		_ = tm.CompleteTask(taskID, msg, map[string]any{
			"success":  sum.Success,
			"failed":   sum.Failed,
			"total":    sum.Total,
			"configId": configID,
			"mode":     string(mode),
			"profile":  profileName,
		})
	}()

	return taskID, nil
}
