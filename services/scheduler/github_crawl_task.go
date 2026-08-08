package scheduler

import (
	"context"
	"errors"
	"fmt"
	"sublink/models"
	"sublink/node"
	"sublink/utils"
	"time"
)

// ExecuteGitHubCrawlTask 执行独立 GitHub 抓取任务
func ExecuteGitHubCrawlTask(configID int, trigger models.TaskTrigger) {
	if configID <= 0 {
		return
	}

	cfg, err := models.GetGitHubCrawlConfigByID(configID)
	if err != nil {
		utils.Error("[GitHubCrawl] 配置不存在 ID=%d: %v", configID, err)
		return
	}

	taskName := cfg.Name
	if taskName == "" {
		taskName = fmt.Sprintf("GitHubCrawl_%d", configID)
	}

	tm := getTaskManager()
	task, ctx, createErr := tm.CreateTask(models.TaskTypeGitHubCrawl, taskName, trigger, 0)

	var reporter node.TaskReporter
	var taskID string
	if createErr != nil {
		utils.Warn("[GitHubCrawl] 创建任务失败: %v，降级为无进度报告", createErr)
		reporter = &node.NoOpTaskReporter{}
		ctx = context.Background()
		taskID = ""
	} else {
		taskID = task.ID
		reporter = NewTaskManagerReporter(tm, task.ID)
		defer func() {
			if r := recover(); r != nil {
				utils.Error("[GitHubCrawl] panic: %v, task=%s, config=%s", r, task.ID, cfg.Name)
				reporter.ReportFail(fmt.Sprintf("任务异常崩溃: %v", r))
			}
		}()
		select {
		case <-ctx.Done():
			utils.Info("[GitHubCrawl] 任务在执行前已取消: %s", cfg.Name)
			return
		default:
		}
	}

	run := &models.GitHubCrawlRun{
		ConfigID:  cfg.ID,
		Status:    "running",
		StartedAt: time.Now(),
		TaskID:    taskID,
	}
	if err := run.Create(); err != nil {
		msg := "创建运行记录失败: " + err.Error()
		utils.Error("[GitHubCrawl] %s", msg)
		reporter.ReportFail(msg)
		_ = cfg.UpdateRunStatus("failed", msg, nil, nil)
		return
	}

	logFn := func(level, message string) {
		_ = models.AppendGitHubCrawlLog(run.ID, cfg.ID, level, message)
	}

	now := time.Now()
	_ = cfg.UpdateRunStatus("running", "抓取中", &now, nil)

	result, crawlErr := node.CrawlGitHubNodes(ctx, cfg, run.ID, logFn, reporter)
	if crawlErr != nil {
		status := "failed"
		msg := crawlErr.Error()
		if errors.Is(crawlErr, context.Canceled) || errors.Is(crawlErr, context.DeadlineExceeded) {
			status = "cancelled"
			msg = "用户停止或超时取消"
		}
		files, found, valid := 0, 0, 0
		if result != nil {
			files = result.FilesScanned
			found = result.NodesFound
			valid = result.NodesAdded
			if result.Message != "" {
				msg = result.Message
			}
		}
		_ = run.Finish(status, msg, files, found, valid)
		_ = cfg.UpdateRunStatus(status, msg, &now, nil)
		if status == "failed" {
			reporter.ReportFail(msg)
		} else {
			reporter.ReportComplete(msg, map[string]any{"cancelled": true})
		}
		return
	}

	files, found, valid := 0, 0, 0
	msg := "完成"
	status := "success"
	if result != nil {
		files = result.FilesScanned
		found = result.NodesFound
		valid = result.NodesAdded
		if result.Message != "" {
			msg = result.Message
		}
		if result.Skipped {
			status = "skipped"
		}
	}

	// 自动加入总表：抓取成功后把本配置下未导入的有效节点推入总节点列表
	if cfg.AutoPromote && !result.Skipped {
		nodes, nerr := models.ListGitHubCrawlNodes(cfg.ID, true, true, 0)
		if nerr != nil {
			logFn("warn", "自动加入总表读取节点失败: "+nerr.Error())
		} else if len(nodes) > 0 {
			promoted, skipped, failed := node.PromoteGitHubCrawlNodesToTotal(cfg, nodes)
			logFn("info", fmt.Sprintf("自动加入总表：成功 %d，跳过 %d，失败 %d", promoted, skipped, failed))
			msg = fmt.Sprintf("%s；自动加入总表 %d", msg, promoted)
		}
	}

	_ = run.Finish(status, msg, files, found, valid)
	_ = cfg.UpdateRunStatus(status, msg, &now, nil)
	_ = taskID
}

// githubCrawlJobIDOffset 与机场/节点检测任务 ID 空间隔离
const githubCrawlJobIDOffset = 2000000

// AddGitHubCrawlJob 添加 GitHub 抓取定时任务
func (sm *SchedulerManager) AddGitHubCrawlJob(configID int, cronExpr string) error {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	jobID := githubCrawlJobIDOffset + configID
	cleanCronExpr := cleanCronExpression(cronExpr)
	if cleanCronExpr == "" {
		return nil
	}

	if entryID, exists := sm.jobs[jobID]; exists {
		sm.cron.Remove(entryID)
		delete(sm.jobs, jobID)
	}

	entryID, err := sm.cron.AddFunc(cleanCronExpr, func() {
		ExecuteGitHubCrawlTask(configID, models.TaskTriggerScheduled)
		nextTime := sm.getNextRunTime(cleanCronExpr)
		now := time.Now()
		sm.updateGitHubCrawlRunTime(configID, &now, nextTime)
	})
	if err != nil {
		utils.Error("添加 GitHub 抓取定时任务失败 - ID: %d, Cron: %s, Error: %v", configID, cleanCronExpr, err)
		return err
	}
	sm.jobs[jobID] = entryID
	nextTime := sm.getNextRunTime(cleanCronExpr)
	sm.updateGitHubCrawlRunTime(configID, nil, nextTime)
	utils.Info("成功添加 GitHub 抓取定时任务 - ID: %d, Cron: %s, 下次运行: %v", configID, cleanCronExpr, nextTime)
	return nil
}

// RemoveGitHubCrawlJob 删除 GitHub 抓取定时任务
func (sm *SchedulerManager) RemoveGitHubCrawlJob(configID int) {
	sm.mutex.Lock()
	defer sm.mutex.Unlock()

	jobID := githubCrawlJobIDOffset + configID
	if entryID, exists := sm.jobs[jobID]; exists {
		sm.cron.Remove(entryID)
		delete(sm.jobs, jobID)
		utils.Info("成功删除 GitHub 抓取定时任务 - ID: %d", configID)
	}
}

// UpdateGitHubCrawlJob 更新 GitHub 抓取定时任务
func (sm *SchedulerManager) UpdateGitHubCrawlJob(configID int, cronExpr string, enabled bool) error {
	sm.RemoveGitHubCrawlJob(configID)
	if enabled && stringsTrim(cronExpr) != "" {
		return sm.AddGitHubCrawlJob(configID, cronExpr)
	}
	if !enabled {
		sm.updateGitHubCrawlRunTime(configID, nil, nil)
	}
	return nil
}

func stringsTrim(s string) string {
	for len(s) > 0 && (s[0] == ' ' || s[0] == '\t' || s[0] == '\n' || s[0] == '\r') {
		s = s[1:]
	}
	for len(s) > 0 {
		last := s[len(s)-1]
		if last == ' ' || last == '\t' || last == '\n' || last == '\r' {
			s = s[:len(s)-1]
			continue
		}
		break
	}
	return s
}

func (sm *SchedulerManager) updateGitHubCrawlRunTime(configID int, lastRun, nextRun *time.Time) {
	go func() {
		cfg, err := models.GetGitHubCrawlConfigByID(configID)
		if err != nil {
			utils.Error("获取 GitHub 抓取配置失败 - ID: %d, Error: %v", configID, err)
			return
		}
		status := cfg.LastStatus
		message := cfg.LastMessage
		if err := cfg.UpdateRunStatus(status, message, lastRun, nextRun); err != nil {
			utils.Error("更新 GitHub 抓取运行时间失败 - ID: %d, Error: %v", configID, err)
		}
	}()
}
