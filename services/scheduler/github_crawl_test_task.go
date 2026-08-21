package scheduler

import (
	"context"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"sublink/models"
	"sublink/utils"
)

// githubCrawlTestJobIDOffset 独立节点全测定时任务 ID 空间
// 3000000 起，与抓取任务 (2000000+) / 其它任务隔离
const githubCrawlTestJobIDOffset = 3000000

// ExecuteGitHubCrawlScheduledTest 调度器入口：执行一次该配置下的独立节点定时全测。
// 流程：
//  1. 校验 Profile / Cron 配置；
//  2. 读取全部独立节点；
//  3. 复用通用链路测试（LinkTestModeFull）逐节点测速；
//  4. 每条结果回调：根据 IsValid 维护 ConsecutiveFailures；
//  5. 全测结束后，若开启 TestAutoDeleteEnabled 且 TestFailureThreshold > 0，
//     删除达到阈值的节点（同步清理总表中 promoted 的对应节点）。
func ExecuteGitHubCrawlScheduledTest(configID int) {
	if configID <= 0 {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(configID)
	if err != nil {
		utils.Error("[GitHubCrawlTestSchedule] 配置不存在 ID=%d: %v", configID, err)
		return
	}
	if !cfg.TestEnabled {
		utils.Info("[GitHubCrawlTestSchedule] 配置 %s (id=%d) 未启用定时全测，跳过", cfg.Name, cfg.ID)
		return
	}
	if cfg.TestProfileID <= 0 {
		utils.Warn("[GitHubCrawlTestSchedule] 配置 %s (id=%d) 缺少节点检测策略，跳过", cfg.Name, cfg.ID)
		_ = cfg.UpdateTestRunStatus("failed", "缺少节点检测策略", ptrTime(time.Now()))
		return
	}
	if _, perr := models.GetNodeCheckProfileByID(cfg.TestProfileID); perr != nil {
		utils.Warn("[GitHubCrawlTestSchedule] 配置 %s (id=%d) 的策略 id=%d 不存在，跳过", cfg.Name, cfg.ID, cfg.TestProfileID)
		_ = cfg.UpdateTestRunStatus("failed", fmt.Sprintf("策略 id=%d 不存在", cfg.TestProfileID), ptrTime(time.Now()))
		return
	}

	nodeIDs, err := models.ListAllGitHubCrawlNodeIDs(configID)
	if err != nil {
		utils.Error("[GitHubCrawlTestSchedule] 读取节点列表失败 config=%d: %v", configID, err)
		_ = cfg.UpdateTestRunStatus("failed", "读取节点列表失败: "+err.Error(), ptrTime(time.Now()))
		return
	}
	if len(nodeIDs) == 0 {
		utils.Info("[GitHubCrawlTestSchedule] 配置 %s (id=%d) 没有可测节点，跳过", cfg.Name, cfg.ID)
		_ = cfg.UpdateTestRunStatus("success", "无节点可测", ptrTime(time.Now()))
		return
	}

	nodes, err := models.GetGitHubCrawlNodesByIDs(nodeIDs)
	if err != nil {
		utils.Error("[GitHubCrawlTestSchedule] 读取节点失败 config=%d: %v", configID, err)
		_ = cfg.UpdateTestRunStatus("failed", "读取节点失败: "+err.Error(), ptrTime(time.Now()))
		return
	}
	items := githubCrawlNodesToLinkItems(configID, nodes)
	if len(items) == 0 {
		_ = cfg.UpdateTestRunStatus("success", "无有效节点可测", ptrTime(time.Now()))
		return
	}

	taskName := fmt.Sprintf("GitHubCrawl-定时全测·%s", cfg.Name)
	tm := getTaskManager()
	task, ctx, createErr := tm.CreateTask(models.TaskTypeGitHubCrawl, taskName, models.TaskTriggerScheduled, len(items))
	if createErr != nil {
		utils.Warn("[GitHubCrawlTestSchedule] 创建任务失败: %v，降级为无进度报告", createErr)
		ctx = context.Background()
	}

	taskID := ""
	if task != nil {
		taskID = task.ID
	}

	// 用配置中指定的节点检测策略解析测速参数
	testCfg, profileName := ResolveSpeedTestConfig(cfg.TestProfileID)
	utils.Info("[GitHubCrawlTestSchedule] config=%d profile=%s nodes=%d",
		configID, profileName, len(items))

	logFn := func(level, msg string) {
		_ = models.AppendGitHubCrawlLog(0, configID, level, msg)
	}
	logFn("info", fmt.Sprintf("定时全测启动（%d 个节点，策略：%s）", len(items), profileName))

	runStart := time.Now()

	var successCount, failedCount int32
	var (
		failedMu     sync.Mutex
		failedIDs    []int
		processedCnt int32
	)

	_ = RunLinkTests(ctx, items, testCfg, LinkTestModeFull, func(result LinkTestResult) {
		outcome := models.GitHubCrawlTestOutcomeSuccess
		if !result.IsValid {
			outcome = models.GitHubCrawlTestOutcomeFailure
		}
		_ = models.UpdateGitHubCrawlNodeTestResultWithOutcome(
			result.ID,
			result.DelayTime,
			result.DelayStatus,
			result.Speed,
			result.SpeedStatus,
			result.IsValid,
			outcome,
		)
		if outcome == models.GitHubCrawlTestOutcomeSuccess {
			atomic.AddInt32(&successCount, 1)
		} else {
			atomic.AddInt32(&failedCount, 1)
			failedMu.Lock()
			failedIDs = append(failedIDs, result.ID)
			failedMu.Unlock()
		}
		if taskID != "" {
			statusBits := result.DelayStatus
			if result.SpeedStatus != "" {
				statusBits = result.DelayStatus + "/" + result.SpeedStatus
			}
			n := int(atomic.AddInt32(&processedCnt, 1))
			_ = tm.UpdateProgress(taskID, n,
				result.Name, map[string]any{
					"nodeId":      result.ID,
					"delayTime":   result.DelayTime,
					"delayStatus": result.DelayStatus,
					"speed":       result.Speed,
					"speedStatus": result.SpeedStatus,
					"isValid":     result.IsValid,
					"status":      statusBits,
					"configId":    configID,
					"mode":        "scheduled_full",
				})
		}
	})

	s := atomic.LoadInt32(&successCount)
	f := atomic.LoadInt32(&failedCount)
	msg := fmt.Sprintf("定时全测完成（成功 %d，失败 %d，共 %d）", s, f, s+f)
	logFn("info", msg)

	// 自动删除：连续失败达阈值的节点
	deleted := 0
	totalRemoved := int64(0)
	if cfg.TestAutoDeleteEnabled && cfg.TestFailureThreshold > 0 {
		failedMu.Lock()
		toCheck := append([]int(nil), failedIDs...)
		failedMu.Unlock()

		deletedNodes, err := models.ListGitHubCrawlNodesByIDsForAutoDelete(configID, toCheck, cfg.TestFailureThreshold)
		if err != nil {
			utils.Warn("[GitHubCrawlTestSchedule] 查询待删除节点失败 config=%d: %v", configID, err)
			logFn("warn", "查询待删除节点失败: "+err.Error())
		} else if len(deletedNodes) > 0 {
			ids := make([]int, 0, len(deletedNodes))
			promotedIDs := make([]int, 0)
			for _, n := range deletedNodes {
				ids = append(ids, n.ID)
				if n.PromotedNodeID > 0 {
					promotedIDs = append(promotedIDs, n.PromotedNodeID)
				}
			}
			// 同步清理总表
			if len(promotedIDs) > 0 {
				if derr := models.BatchDel(promotedIDs); derr != nil {
					utils.Warn("[GitHubCrawlTestSchedule] 同步清理总表节点失败: %v", derr)
					logFn("warn", "同步清理总表节点失败: "+derr.Error())
				} else {
					totalRemoved = int64(len(promotedIDs))
				}
			}
			dn, derr := models.DeleteGitHubCrawlNodesByIDs(configID, ids)
			if derr != nil {
				utils.Warn("[GitHubCrawlTestSchedule] 自动删除独立节点失败: %v", derr)
				logFn("warn", "自动删除独立节点失败: "+derr.Error())
			} else {
				deleted = int(dn)
				delMsg := fmt.Sprintf("自动删除连续失败 ≥%d 的节点 %d 个（独立表）", cfg.TestFailureThreshold, deleted)
				if totalRemoved > 0 {
					delMsg = fmt.Sprintf("%s，同步清理总表 %d 个", delMsg, totalRemoved)
				}
				logFn("info", delMsg)
				msg = msg + "；" + delMsg
			}
		}
	}

	// 任务中心收尾
	if taskID != "" {
		_ = tm.CompleteTask(taskID, msg, map[string]any{
			"success":   s,
			"failed":    f,
			"deleted":   deleted,
			"configId":  configID,
			"trigger":   "scheduled_test",
			"profileId": cfg.TestProfileID,
		})
	}

	status := "success"
	if int(f) > 0 && int(s) == 0 {
		status = "failed"
	}
	_ = cfg.UpdateTestRunStatus(status, msg, &runStart)
}

// AddGitHubCrawlTestJob 注册独立节点定时全测任务
func (sm *SchedulerManager) AddGitHubCrawlTestJob(configID int, cronExpr string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	jobID := githubCrawlTestJobIDOffset + configID
	cleanCronExpr := cleanCronExpression(cronExpr)
	if cleanCronExpr == "" {
		return nil
	}

	if entryID, exists := sm.jobs[jobID]; exists {
		sm.cron.Remove(entryID)
		delete(sm.jobs, jobID)
	}

	entryID, err := sm.cron.AddFunc(cleanCronExpr, func() {
		ExecuteGitHubCrawlScheduledTest(configID)
		nextTime := sm.getNextRunTime(cleanCronExpr)
		now := time.Now()
		sm.updateGitHubCrawlTestRunTime(configID, &now, nextTime)
	})
	if err != nil {
		utils.Error("添加 GitHub 全测定时任务失败 - ID: %d, Cron: %s, Error: %v", configID, cleanCronExpr, err)
		return err
	}
	sm.jobs[jobID] = entryID
	nextTime := sm.getNextRunTime(cleanCronExpr)
	sm.updateGitHubCrawlTestRunTime(configID, nil, nextTime)
	utils.Info("成功添加 GitHub 全测定时任务 - ID: %d, Cron: %s, 下次运行: %v", configID, cleanCronExpr, nextTime)
	return nil
}

// RemoveGitHubCrawlTestJob 删除独立节点定时全测任务
func (sm *SchedulerManager) RemoveGitHubCrawlTestJob(configID int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	jobID := githubCrawlTestJobIDOffset + configID
	if entryID, exists := sm.jobs[jobID]; exists {
		sm.cron.Remove(entryID)
		delete(sm.jobs, jobID)
		utils.Info("成功删除 GitHub 全测定时任务 - ID: %d", configID)
	}
}

// UpdateGitHubCrawlTestJob 更新独立节点定时全测任务
func (sm *SchedulerManager) UpdateGitHubCrawlTestJob(configID int, cronExpr string, enabled bool) error {
	sm.RemoveGitHubCrawlTestJob(configID)
	if enabled && stringsTrim(cronExpr) != "" {
		return sm.AddGitHubCrawlTestJob(configID, cronExpr)
	}
	return nil
}

func (sm *SchedulerManager) updateGitHubCrawlTestRunTime(configID int, lastTest, nextTest *time.Time) {
	// 仅写 LastTestTime 字段，nextTest 当前不持久化（前端通过 cron 自解释显示）
	go func() {
		cfg, err := models.GetGitHubCrawlConfigByID(configID)
		if err != nil {
			utils.Error("获取 GitHub 配置失败 - ID: %d, Error: %v", configID, err)
			return
		}
		_ = cfg.UpdateTestRunStatus(cfg.LastTestStatus, cfg.LastTestMessage, lastTest)
	}()
}

func ptrTime(t time.Time) *time.Time { return &t }
