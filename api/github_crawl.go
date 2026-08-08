package api

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/robfig/cron/v3"

	"sublink/database"
	"sublink/models"
	"sublink/node"
	"sublink/services"
	"sublink/services/scheduler"
	"sublink/utils"
)

type githubCrawlConfigRequest struct {
	Name               string `json:"name"`
	GitHubToken        string `json:"githubToken"`
	SearchKeywords     string `json:"searchKeywords"`
	SearchInterval     int    `json:"searchInterval"`
	CollectionInterval int    `json:"collectionInterval"`
	MaxCrawlLinks      int    `json:"maxCrawlLinks"`
	UseProxy           bool   `json:"useProxy"`
	CronExpr           string `json:"cronExpr"`
	Enabled            bool   `json:"enabled"`
	Group              string `json:"group"`
	Remark             string `json:"remark"`
	AutoPromote        bool   `json:"autoPromote"`
}

type githubCrawlToggleRequest struct {
	Enabled bool `json:"enabled"`
}

type githubCrawlNodeIDsRequest struct {
	NodeIDs   []int `json:"nodeIds"`
	ProfileID int   `json:"profileId"` // 可选：使用指定节点检测策略的通用测速配置
}

type simpleError struct{ msg string }

func (e *simpleError) Error() string { return e.msg }

var (
	errNameRequired = &simpleError{"名称不能为空"}
	errInvalidCron  = &simpleError{"Cron 表达式无效，需为 5 字段格式"}
)

func parseGitHubCrawlID(c *gin.Context) (int, bool) {
	id, err := strconv.Atoi(c.Param("id"))
	if err != nil || id <= 0 {
		utils.FailWithMsg(c, "无效的配置 ID")
		return 0, false
	}
	return id, true
}

func validateGitHubCrawlCron(expr string) bool {
	expr = strings.TrimSpace(expr)
	if expr == "" {
		return true
	}
	parser := cron.NewParser(cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow)
	_, err := parser.Parse(expr)
	return err == nil
}

func applyGitHubCrawlRequest(cfg *models.GitHubCrawlConfig, req githubCrawlConfigRequest) error {
	cfg.Name = strings.TrimSpace(req.Name)
	cfg.GitHubToken = strings.TrimSpace(req.GitHubToken)
	cfg.SearchKeywords = strings.TrimSpace(req.SearchKeywords)
	cfg.SearchInterval = req.SearchInterval
	cfg.CollectionInterval = req.CollectionInterval
	cfg.MaxCrawlLinks = req.MaxCrawlLinks
	cfg.UseProxy = req.UseProxy
	cfg.CronExpr = strings.TrimSpace(req.CronExpr)
	cfg.Enabled = req.Enabled
	cfg.Group = strings.TrimSpace(req.Group)
	cfg.Remark = strings.TrimSpace(req.Remark)
	cfg.AutoPromote = req.AutoPromote
	if cfg.Name == "" {
		return errNameRequired
	}
	if !validateGitHubCrawlCron(cfg.CronExpr) {
		return errInvalidCron
	}
	return nil
}

// appendGitHubCrawlOpLog writes page/API operation results into crawl logs (run_id=0).
func appendGitHubCrawlOpLog(configID int, level, message string) {
	if configID <= 0 || strings.TrimSpace(message) == "" {
		return
	}
	if err := models.AppendGitHubCrawlLog(0, configID, level, message); err != nil {
		utils.Warn("[GitHubCrawl] append op log failed config=%d: %v", configID, err)
	}
}

// GitHubCrawlList 配置列表
func GitHubCrawlList(c *gin.Context) {
	list, err := models.ListGitHubCrawlConfigs()
	if err != nil {
		utils.FailWithMsg(c, "获取列表失败: "+err.Error())
		return
	}
	utils.OkDetailed(c, "获取成功", list)
}

// GitHubCrawlGet 配置详情
func GitHubCrawlGet(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}
	utils.OkDetailed(c, "获取成功", cfg)
}

// GitHubCrawlAdd 新建配置
func GitHubCrawlAdd(c *gin.Context) {
	var req githubCrawlConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数错误: "+err.Error())
		return
	}
	cfg := &models.GitHubCrawlConfig{}
	if err := applyGitHubCrawlRequest(cfg, req); err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	if err := cfg.Add(); err != nil {
		utils.FailWithMsg(c, "创建失败: "+err.Error())
		return
	}
	sch := scheduler.GetSchedulerManager()
	_ = sch.UpdateGitHubCrawlJob(cfg.ID, cfg.CronExpr, cfg.Enabled)
	utils.OkDetailed(c, "创建成功", cfg)
}

// GitHubCrawlUpdate 更新配置
func GitHubCrawlUpdate(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}
	var req githubCrawlConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数错误: "+err.Error())
		return
	}
	if err := applyGitHubCrawlRequest(cfg, req); err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	if err := cfg.Update(); err != nil {
		utils.FailWithMsg(c, "更新失败: "+err.Error())
		return
	}
	sch := scheduler.GetSchedulerManager()
	_ = sch.UpdateGitHubCrawlJob(cfg.ID, cfg.CronExpr, cfg.Enabled)
	utils.OkDetailed(c, "更新成功", cfg)
}

// GitHubCrawlDelete 删除配置
func GitHubCrawlDelete(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}
	sch := scheduler.GetSchedulerManager()
	sch.RemoveGitHubCrawlJob(id)
	if err := cfg.Delete(); err != nil {
		utils.FailWithMsg(c, "删除失败: "+err.Error())
		return
	}
	utils.OkWithMsg(c, "删除成功")
}

// GitHubCrawlToggle 启停
func GitHubCrawlToggle(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}
	var req githubCrawlToggleRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数错误: "+err.Error())
		return
	}
	cfg.Enabled = req.Enabled
	if err := cfg.Update(); err != nil {
		utils.FailWithMsg(c, "更新失败: "+err.Error())
		return
	}
	sch := scheduler.GetSchedulerManager()
	_ = sch.UpdateGitHubCrawlJob(cfg.ID, cfg.CronExpr, cfg.Enabled)
	utils.OkDetailed(c, "更新成功", cfg)
}

// GitHubCrawlRun 立即执行
func GitHubCrawlRun(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}
	if strings.EqualFold(strings.TrimSpace(cfg.LastStatus), "running") {
		utils.FailWithMsg(c, "该配置正在抓取中，请先停止")
		return
	}
	var running models.GitHubCrawlRun
	if err := database.DB.Where("config_id = ? AND status = ?", id, "running").Order("id DESC").First(&running).Error; err == nil {
		utils.FailWithMsg(c, "该配置正在抓取中，请先停止")
		return
	}
	appendGitHubCrawlOpLog(id, "info", "用户启动抓取任务")
	go scheduler.ExecuteGitHubCrawlTask(id, models.TaskTriggerManual)
	utils.OkWithMsg(c, "抓取任务已启动")
}

// GitHubCrawlLogs 日志
func GitHubCrawlLogs(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	afterID, _ := strconv.Atoi(c.Query("afterId"))
	limit, _ := strconv.Atoi(c.Query("limit"))
	runID, _ := strconv.Atoi(c.Query("runId"))
	list, err := models.ListGitHubCrawlLogs(runID, id, afterID, limit)
	if err != nil {
		utils.FailWithMsg(c, "获取日志失败: "+err.Error())
		return
	}
	utils.OkDetailed(c, "获取成功", list)
}

// GitHubCrawlRuns 运行记录
func GitHubCrawlRuns(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	limit, _ := strconv.Atoi(c.Query("limit"))
	list, err := models.ListGitHubCrawlRuns(id, limit)
	if err != nil {
		utils.FailWithMsg(c, "获取运行记录失败: "+err.Error())
		return
	}
	utils.OkDetailed(c, "获取成功", list)
}

// GitHubCrawlNodes 独立节点列表
func GitHubCrawlNodes(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	onlyValid := c.Query("onlyValid") == "true" || c.Query("onlyValid") == "1"
	onlyUnpromoted := c.Query("onlyUnpromoted") == "true" || c.Query("onlyUnpromoted") == "1"
	limit, _ := strconv.Atoi(c.Query("limit"))
	// limit<=0: 不限制条数
	list, err := models.ListGitHubCrawlNodes(id, onlyValid, onlyUnpromoted, limit)
	if err != nil {
		utils.FailWithMsg(c, "获取节点失败: "+err.Error())
		return
	}
	utils.OkDetailed(c, "获取成功", list)
}

// GitHubCrawlClearNodes 清空独立节点
func GitHubCrawlClearNodes(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	if err := models.ClearGitHubCrawlNodes(id); err != nil {
		utils.FailWithMsg(c, "清空失败: "+err.Error())
		return
	}
	appendGitHubCrawlOpLog(id, "info", "已清空独立节点列表")
	utils.OkWithMsg(c, "已清空")
}

// GitHubCrawlDeleteNodes 删除选中独立节点（body: {nodeIds:[]}，不可为空）
func GitHubCrawlDeleteNodes(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	var req githubCrawlNodeIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数错误: "+err.Error())
		return
	}
	if len(req.NodeIDs) == 0 {
		utils.FailWithMsg(c, "请选择要删除的节点")
		return
	}
	n, err := models.DeleteGitHubCrawlNodesByIDs(id, req.NodeIDs)
	if err != nil {
		utils.FailWithMsg(c, "删除失败: "+err.Error())
		return
	}
	appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("已删除选中节点 %d 个", n))
	utils.OkDetailed(c, "已删除选中节点", gin.H{"deleted": n})
}

// GitHubCrawlDeleteInvalidNodes 删除无效节点
func GitHubCrawlDeleteInvalidNodes(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	n, err := models.DeleteInvalidGitHubCrawlNodes(id)
	if err != nil {
		utils.FailWithMsg(c, "删除无效节点失败: "+err.Error())
		return
	}
	appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("已删除无效节点 %d 个", n))
	utils.OkDetailed(c, "已删除无效节点", gin.H{"deleted": n})
}

// GitHubCrawlClearLogs 清空抓取日志
func GitHubCrawlClearLogs(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	if err := models.ClearGitHubCrawlLogs(id); err != nil {
		utils.FailWithMsg(c, "清空日志失败: "+err.Error())
		return
	}
	utils.OkWithMsg(c, "日志已清空")
}

// GitHubCrawlStop 停止正在运行的 GitHub 抓取
func GitHubCrawlStop(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}

	// 查找最新运行中的抓取记录
	var run models.GitHubCrawlRun
	if err := database.DB.Where("config_id = ? AND status = ?", id, "running").Order("id DESC").First(&run).Error; err != nil {
		_ = cfg.UpdateRunStatus("cancelled", "用户停止", nil, nil)
		appendGitHubCrawlOpLog(id, "warn", "用户停止：当前无运行中任务")
		utils.OkWithMsg(c, "无运行中任务")
		return
	}

	if run.TaskID != "" {
		if err := services.GetTaskManager().CancelTask(run.TaskID); err != nil {
			utils.Warn("取消任务失败: %v", err)
		}
	}

	_ = run.Finish("cancelled", "用户停止", run.FilesScanned, run.NodesFound, run.NodesValid)
	_ = cfg.UpdateRunStatus("cancelled", "用户停止", nil, nil)
	appendGitHubCrawlOpLog(id, "info", "用户停止抓取任务")
	utils.OkWithMsg(c, "抓取任务已停止")
}

// GitHubCrawlPromote 加入总节点列表
func GitHubCrawlPromote(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	cfg, err := models.GetGitHubCrawlConfigByID(id)
	if err != nil {
		utils.FailWithMsg(c, "配置不存在")
		return
	}
	var req githubCrawlNodeIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数错误: "+err.Error())
		return
	}
	if len(req.NodeIDs) == 0 {
		utils.FailWithMsg(c, "请选择要加入的节点")
		return
	}
	nodes, err := models.GetGitHubCrawlNodesByIDs(req.NodeIDs)
	if err != nil {
		utils.FailWithMsg(c, "读取节点失败: "+err.Error())
		return
	}

	appendGitHubCrawlOpLog(cfg.ID, "info", fmt.Sprintf("开始加入总节点列表（%d 个）", len(nodes)))
	promoted, skipped, failed := node.PromoteGitHubCrawlNodesToTotal(cfg, nodes)
	msg := node.FormatPromoteSummary(promoted, skipped, failed)
	appendGitHubCrawlOpLog(cfg.ID, "info", msg)
	if failed > 0 {
		appendGitHubCrawlOpLog(cfg.ID, "warn", fmt.Sprintf("加入总节点列表部分失败：%d 个节点未能写入总表", failed))
	}
	utils.Info("[GitHubCrawl] %s (config=%d requested=%d)", msg, cfg.ID, len(req.NodeIDs))

	utils.OkDetailed(c, "加入完成", gin.H{
		"promoted": promoted,
		"skipped":  skipped,
		"failed":   failed,
	})
}

// loadGitHubCrawlNodesForTest 读取待测独立节点（nodeIds 为空则全部）
func loadGitHubCrawlNodesForTest(configID int, nodeIDs []int) ([]models.GitHubCrawlNode, error) {
	if len(nodeIDs) == 0 {
		ids, err := models.ListAllGitHubCrawlNodeIDs(configID)
		if err != nil {
			return nil, err
		}
		return models.GetGitHubCrawlNodesByIDs(ids)
	}
	return models.GetGitHubCrawlNodesByIDs(nodeIDs)
}

func githubCrawlNodesToLinkItems(configID int, nodes []models.GitHubCrawlNode) []scheduler.LinkTestItem {
	items := make([]scheduler.LinkTestItem, 0, len(nodes))
	for _, gn := range nodes {
		if gn.ConfigID != configID || strings.TrimSpace(gn.Link) == "" {
			continue
		}
		items = append(items, scheduler.LinkTestItem{
			ID:              gn.ID,
			Link:            gn.Link,
			Name:            gn.Name,
			PrevDelayTime:   gn.DelayTime,
			PrevDelayStatus: gn.DelayStatus,
			PrevSpeed:       gn.Speed,
			PrevSpeedStatus: gn.SpeedStatus,
			PrevIsValid:     gn.IsValid,
		})
	}
	return items
}

func runGitHubCrawlLinkTests(configID int, nodes []models.GitHubCrawlNode, profileID int, mode scheduler.LinkTestMode) scheduler.LinkTestSummary {
	items := githubCrawlNodesToLinkItems(configID, nodes)
	if len(items) == 0 {
		return scheduler.LinkTestSummary{}
	}
	cfg, profileName := scheduler.ResolveSpeedTestConfig(profileID)
	utils.Info("GitHub 独立节点%s测试: config=%d nodes=%d profile=%s", mode, configID, len(items), profileName)
	return scheduler.RunLinkTests(nil, items, cfg, mode, func(result scheduler.LinkTestResult) {
		_ = models.UpdateGitHubCrawlNodeTestResult(
			result.ID,
			result.DelayTime,
			result.DelayStatus,
			result.Speed,
			result.SpeedStatus,
			result.IsValid,
		)
	})
}

// GitHubCrawlTestDelay 对独立节点测延时（nodeIds 为空则测全部）
// 使用通用测速模块（节点检测策略并发/URL/超时配置）
func GitHubCrawlTestDelay(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	var req githubCrawlNodeIDsRequest
	_ = c.ShouldBindJSON(&req)
	nodes, err := loadGitHubCrawlNodesForTest(id, req.NodeIDs)
	if err != nil {
		utils.FailWithMsg(c, "读取节点失败: "+err.Error())
		return
	}
	items := githubCrawlNodesToLinkItems(id, nodes)
	if len(items) == 0 {
		appendGitHubCrawlOpLog(id, "warn", "测延时：没有可测节点")
		utils.OkDetailed(c, "没有可测节点", gin.H{"success": 0, "failed": 0, "total": 0})
		return
	}

	// 单个节点同步返回；批量全测异步执行，避免长时间阻塞请求
	if len(items) <= 1 {
		appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("开始测延时（%d 个节点）", len(items)))
		sum := runGitHubCrawlLinkTests(id, nodes, req.ProfileID, scheduler.LinkTestModeDelay)
		appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("测延时完成：成功 %d，失败 %d，共 %d", sum.Success, sum.Failed, sum.Total))
		utils.OkDetailed(c, "测延时完成", gin.H{"success": sum.Success, "failed": sum.Failed, "total": sum.Total, "async": false})
		return
	}

	appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("全测延时任务已启动（%d 个节点）", len(items)))
	go func() {
		sum := runGitHubCrawlLinkTests(id, nodes, req.ProfileID, scheduler.LinkTestModeDelay)
		appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("全测延时完成：成功 %d，失败 %d，共 %d", sum.Success, sum.Failed, sum.Total))
		utils.Info("GitHub 全测延时完成 config=%d success=%d failed=%d total=%d", id, sum.Success, sum.Failed, sum.Total)
	}()
	utils.OkDetailed(c, "全测延时任务已启动", gin.H{
		"success": 0,
		"failed":  0,
		"total":   len(items),
		"async":   true,
	})
}

// GitHubCrawlTestSpeed 对独立节点测速（nodeIds 为空则测全部）
// 使用通用测速模块（节点检测策略并发/URL/超时配置）
func GitHubCrawlTestSpeed(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	var req githubCrawlNodeIDsRequest
	_ = c.ShouldBindJSON(&req)
	nodes, err := loadGitHubCrawlNodesForTest(id, req.NodeIDs)
	if err != nil {
		utils.FailWithMsg(c, "读取节点失败: "+err.Error())
		return
	}
	items := githubCrawlNodesToLinkItems(id, nodes)
	if len(items) == 0 {
		appendGitHubCrawlOpLog(id, "warn", "测速：没有可测节点")
		utils.OkDetailed(c, "没有可测节点", gin.H{"success": 0, "failed": 0, "total": 0})
		return
	}

	if len(items) <= 1 {
		appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("开始测速（%d 个节点）", len(items)))
		sum := runGitHubCrawlLinkTests(id, nodes, req.ProfileID, scheduler.LinkTestModeSpeed)
		appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("测速完成：成功 %d，失败 %d，共 %d", sum.Success, sum.Failed, sum.Total))
		utils.OkDetailed(c, "测速完成", gin.H{"success": sum.Success, "failed": sum.Failed, "total": sum.Total, "async": false})
		return
	}

	appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("全测速度任务已启动（%d 个节点）", len(items)))
	go func() {
		sum := runGitHubCrawlLinkTests(id, nodes, req.ProfileID, scheduler.LinkTestModeSpeed)
		appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("全测速度完成：成功 %d，失败 %d，共 %d", sum.Success, sum.Failed, sum.Total))
		utils.Info("GitHub 全测速度完成 config=%d success=%d failed=%d total=%d", id, sum.Success, sum.Failed, sum.Total)
	}()
	utils.OkDetailed(c, "全测速度任务已启动", gin.H{
		"success": 0,
		"failed":  0,
		"total":   len(items),
		"async":   true,
	})
}

// GitHubCrawlTest 按节点检测策略对独立节点执行全测（nodeIds 为空则全部）
// profileId 必填：使用对应策略的 URL/超时/并发/模式（tcp=仅延时，mihomo=延时+测速）
// 全测会注册到系统任务中心，可在任务列表查看进度/取消。
func GitHubCrawlTest(c *gin.Context) {
	id, ok := parseGitHubCrawlID(c)
	if !ok {
		return
	}
	var req githubCrawlNodeIDsRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数错误: "+err.Error())
		return
	}
	if req.ProfileID <= 0 {
		utils.FailWithMsg(c, "请选择节点检测策略")
		return
	}
	if _, err := models.GetNodeCheckProfileByID(req.ProfileID); err != nil {
		utils.FailWithMsg(c, "节点检测策略不存在")
		return
	}
	nodes, err := loadGitHubCrawlNodesForTest(id, req.NodeIDs)
	if err != nil {
		utils.FailWithMsg(c, "读取节点失败: "+err.Error())
		return
	}
	items := githubCrawlNodesToLinkItems(id, nodes)
	if len(items) == 0 {
		utils.OkDetailed(c, "没有可测节点", gin.H{"success": 0, "failed": 0, "total": 0, "async": false})
		return
	}

	configName := ""
	if cfg, cfgErr := models.GetGitHubCrawlConfigByID(id); cfgErr == nil && cfg != nil {
		configName = cfg.Name
	}

	taskID, startErr := scheduler.StartGitHubCrawlNodeTestTask(
		id,
		configName,
		items,
		req.ProfileID,
		scheduler.LinkTestModeFull,
		func(result scheduler.LinkTestResult) {
			_ = models.UpdateGitHubCrawlNodeTestResult(
				result.ID,
				result.DelayTime,
				result.DelayStatus,
				result.Speed,
				result.SpeedStatus,
				result.IsValid,
			)
		},
	)
	if startErr != nil {
		utils.FailWithMsg(c, startErr.Error())
		return
	}

	appendGitHubCrawlOpLog(id, "info", fmt.Sprintf("全测任务已启动（%d 个节点，task=%s）", len(items), taskID))
	utils.OkDetailed(c, "全测任务已启动", gin.H{
		"success": 0,
		"failed":  0,
		"total":   len(items),
		"async":   true,
		"taskId":  taskID,
	})
}
