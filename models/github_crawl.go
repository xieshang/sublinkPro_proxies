package models

import (
	"crypto/sha256"
	"encoding/hex"
	"strings"
	"time"

	"sublink/database"

	"gorm.io/gorm"
)

// GitHubCrawlConfig GitHub 抓取配置
type GitHubCrawlConfig struct {
	ID                 int        `gorm:"primaryKey" json:"id"`
	Name               string     `gorm:"uniqueIndex" json:"name"`
	GitHubToken        string     `json:"githubToken"`
	SearchKeywords     string     `gorm:"type:text" json:"searchKeywords"`
	SearchInterval     int        `gorm:"default:3600" json:"searchInterval"`
	CollectionInterval int        `gorm:"default:3600" json:"collectionInterval"`
	MaxCrawlLinks      int        `gorm:"default:40" json:"maxCrawlLinks"`
	UseProxy           bool       `gorm:"default:false" json:"useProxy"`
	CronExpr           string     `gorm:"default:'0 */6 * * *'" json:"cronExpr"`
	Enabled            bool       `gorm:"default:false" json:"enabled"`
	Group              string     `json:"group"`
	Remark             string     `gorm:"type:text" json:"remark"`
	LastRunTime        *time.Time `json:"lastRunTime"`
	NextRunTime        *time.Time `json:"nextRunTime"`
	LastStatus         string     `json:"lastStatus"`
	LastMessage        string     `gorm:"type:text" json:"lastMessage"`
	AutoPromote        bool       `gorm:"default:false" json:"autoPromote"`
	CreatedAt          time.Time  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt          time.Time  `gorm:"autoUpdateTime" json:"updatedAt"`
}

func (GitHubCrawlConfig) TableName() string { return "github_crawl_configs" }

func (c *GitHubCrawlConfig) normalize() {
	c.Name = strings.TrimSpace(c.Name)
	c.GitHubToken = strings.TrimSpace(c.GitHubToken)
	c.SearchKeywords = strings.TrimSpace(c.SearchKeywords)
	c.CronExpr = strings.TrimSpace(c.CronExpr)
	c.Group = strings.TrimSpace(c.Group)
	c.Remark = strings.TrimSpace(c.Remark)
	if c.SearchInterval < 0 {
		c.SearchInterval = 0
	}
	if c.CollectionInterval < 0 {
		c.CollectionInterval = 0
	}
	if c.MaxCrawlLinks <= 0 {
		c.MaxCrawlLinks = 40
	}
	if c.CronExpr == "" {
		c.CronExpr = "0 */6 * * *"
	}
	if c.Group == "" {
		c.Group = "github"
	}
}

// Add 新建配置
func (c *GitHubCrawlConfig) Add() error {
	c.normalize()
	return database.DB.Create(c).Error
}

// Update 更新配置
func (c *GitHubCrawlConfig) Update() error {
	c.normalize()
	return database.DB.Model(c).Select(
		"Name", "GitHubToken", "SearchKeywords", "SearchInterval", "CollectionInterval",
		"MaxCrawlLinks", "UseProxy", "CronExpr", "Enabled", "Group", "Remark", "AutoPromote",
	).Updates(c).Error
}

// Delete 删除配置及其关联数据
func (c *GitHubCrawlConfig) Delete() error {
	return database.DB.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("config_id = ?", c.ID).Delete(&GitHubCrawlNode{}).Error; err != nil {
			return err
		}
		if err := tx.Where("config_id = ?", c.ID).Delete(&GitHubCrawlLog{}).Error; err != nil {
			return err
		}
		if err := tx.Where("config_id = ?", c.ID).Delete(&GitHubCrawlRun{}).Error; err != nil {
			return err
		}
		if err := tx.Where("config_id = ?", c.ID).Delete(&GitHubCrawlBlacklist{}).Error; err != nil {
			return err
		}
		return tx.Delete(c).Error
	})
}

// Find 按名称查找
func (c *GitHubCrawlConfig) Find() error {
	return database.DB.Where("name = ?", c.Name).First(c).Error
}

// UpdateRunStatus 更新运行状态与时间。
// lastRun/nextRun 非 nil 时更新对应字段。
func (c *GitHubCrawlConfig) UpdateRunStatus(status, message string, lastRun, nextRun *time.Time) error {
	updates := map[string]any{
		"last_status":  status,
		"last_message": message,
	}
	c.LastStatus = status
	c.LastMessage = message
	if lastRun != nil {
		updates["last_run_time"] = lastRun
		c.LastRunTime = lastRun
	}
	if nextRun != nil {
		updates["next_run_time"] = nextRun
		c.NextRunTime = nextRun
	}
	return database.DB.Model(c).Updates(updates).Error
}

// GetGitHubCrawlConfigByID 按 ID 获取
func GetGitHubCrawlConfigByID(id int) (*GitHubCrawlConfig, error) {
	var cfg GitHubCrawlConfig
	if err := database.DB.Where("id = ?", id).First(&cfg).Error; err != nil {
		return nil, err
	}
	return &cfg, nil
}

// ListGitHubCrawlConfigs 列出全部配置
func ListGitHubCrawlConfigs() ([]GitHubCrawlConfig, error) {
	var list []GitHubCrawlConfig
	err := database.DB.Order("id ASC").Find(&list).Error
	return list, err
}

// ListEnabledGitHubCrawlConfigs 列出启用配置
func ListEnabledGitHubCrawlConfigs() ([]GitHubCrawlConfig, error) {
	var list []GitHubCrawlConfig
	err := database.DB.Where("enabled = ?", true).Order("id ASC").Find(&list).Error
	return list, err
}

// GitHubCrawlRun 单次运行记录
type GitHubCrawlRun struct {
	ID           int        `gorm:"primaryKey" json:"id"`
	ConfigID     int        `gorm:"index" json:"configId"`
	Status       string     `gorm:"index" json:"status"`
	StartedAt    time.Time  `json:"startedAt"`
	FinishedAt   *time.Time `json:"finishedAt"`
	FilesScanned int        `json:"filesScanned"`
	NodesFound   int        `json:"nodesFound"`
	NodesValid   int        `json:"nodesValid"`
	Message      string     `gorm:"type:text" json:"message"`
	TaskID       string     `json:"taskId"`
	CreatedAt    time.Time  `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt    time.Time  `gorm:"autoUpdateTime" json:"updatedAt"`
}

func (GitHubCrawlRun) TableName() string { return "github_crawl_runs" }

// Create 创建运行记录
func (r *GitHubCrawlRun) Create() error {
	if r.StartedAt.IsZero() {
		r.StartedAt = time.Now()
	}
	if r.Status == "" {
		r.Status = "running"
	}
	return database.DB.Create(r).Error
}

// Finish 结束运行
func (r *GitHubCrawlRun) Finish(status, message string, filesScanned, nodesFound, nodesValid int) error {
	now := time.Now()
	r.Status = status
	r.Message = message
	r.FilesScanned = filesScanned
	r.NodesFound = nodesFound
	r.NodesValid = nodesValid
	r.FinishedAt = &now
	return database.DB.Model(r).Updates(map[string]any{
		"status":        status,
		"message":       message,
		"files_scanned": filesScanned,
		"nodes_found":   nodesFound,
		"nodes_valid":   nodesValid,
		"finished_at":   now,
	}).Error
}

// HasActiveGitHubCrawlRun 是否存在真正进行中的抓取（排除僵尸 running 记录）。
// 超过 maxAge 仍未结束的 running 视为僵尸，自动标记 cancelled。
func HasActiveGitHubCrawlRun(configID int, maxAge time.Duration) (bool, error) {
	if configID <= 0 {
		return false, nil
	}
	if maxAge <= 0 {
		maxAge = 3 * time.Hour
	}
	var runs []GitHubCrawlRun
	if err := database.DB.Where("config_id = ? AND status = ?", configID, "running").
		Order("id DESC").Find(&runs).Error; err != nil {
		return false, err
	}
	if len(runs) == 0 {
		return false, nil
	}
	cutoff := time.Now().Add(-maxAge)
	active := false
	for i := range runs {
		r := &runs[i]
		// 无 finished_at 且启动时间过久 → 僵尸
		if r.StartedAt.Before(cutoff) {
			_ = r.Finish("cancelled", "僵尸任务自动清理（进程重启或异常中断）", r.FilesScanned, r.NodesFound, r.NodesValid)
			continue
		}
		active = true
	}
	return active, nil
}

// CancelAllRunningGitHubCrawlRuns 取消某配置下全部 running 记录（含僵尸）。
// 返回清理条数。
func CancelAllRunningGitHubCrawlRuns(configID int, reason string) (int, error) {
	if configID <= 0 {
		return 0, nil
	}
	if strings.TrimSpace(reason) == "" {
		reason = "用户停止"
	}
	var runs []GitHubCrawlRun
	if err := database.DB.Where("config_id = ? AND status = ?", configID, "running").Find(&runs).Error; err != nil {
		return 0, err
	}
	n := 0
	for i := range runs {
		r := &runs[i]
		if err := r.Finish("cancelled", reason, r.FilesScanned, r.NodesFound, r.NodesValid); err != nil {
			return n, err
		}
		n++
	}
	return n, nil
}

// RecoverStaleGitHubCrawlRuns 启动时清理全局僵尸 running 记录，并修正配置 last_status。
func RecoverStaleGitHubCrawlRuns(maxAge time.Duration) (int, error) {
	if maxAge <= 0 {
		maxAge = 3 * time.Hour
	}
	cutoff := time.Now().Add(-maxAge)
	var runs []GitHubCrawlRun
	if err := database.DB.Where("status = ? AND started_at < ?", "running", cutoff).Find(&runs).Error; err != nil {
		return 0, err
	}
	n := 0
	configIDs := map[int]struct{}{}
	for i := range runs {
		r := &runs[i]
		if err := r.Finish("cancelled", "启动恢复：清理僵尸抓取任务", r.FilesScanned, r.NodesFound, r.NodesValid); err != nil {
			return n, err
		}
		n++
		if r.ConfigID > 0 {
			configIDs[r.ConfigID] = struct{}{}
		}
	}
	// 配置 last_status 仍为 running 且无活跃 run → 纠正
	var cfgs []GitHubCrawlConfig
	if err := database.DB.Where("last_status = ?", "running").Find(&cfgs).Error; err == nil {
		for i := range cfgs {
			cfg := &cfgs[i]
			active, _ := HasActiveGitHubCrawlRun(cfg.ID, maxAge)
			if !active {
				_ = cfg.UpdateRunStatus("cancelled", "启动恢复：无活跃抓取", nil, nil)
			}
		}
	}
	_ = configIDs
	return n, nil
}

// GitHubCrawlLog 抓取日志
type GitHubCrawlLog struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	RunID     int       `gorm:"index" json:"runId"`
	ConfigID  int       `gorm:"index" json:"configId"`
	Level     string    `json:"level"`
	Message   string    `gorm:"type:text" json:"message"`
	CreatedAt time.Time `gorm:"autoCreateTime" json:"createdAt"`
}

func (GitHubCrawlLog) TableName() string { return "github_crawl_logs" }

// githubCrawlLogMaxKeep 每个配置最多保留的日志条数，防止表无限增长占内存/磁盘。
const githubCrawlLogMaxKeep = 500

// AppendGitHubCrawlLog 追加日志，并裁剪该配置超出上限的旧日志。
func AppendGitHubCrawlLog(runID, configID int, level, message string) error {
	level = strings.TrimSpace(level)
	if level == "" {
		level = "info"
	}
	log := &GitHubCrawlLog{
		RunID:    runID,
		ConfigID: configID,
		Level:    level,
		Message:  message,
	}
	if err := database.DB.Create(log).Error; err != nil {
		return err
	}
	if configID > 0 {
		_ = trimGitHubCrawlLogs(configID, githubCrawlLogMaxKeep)
	}
	return nil
}

// trimGitHubCrawlLogs 仅保留某配置最新 keep 条日志，删除更旧记录。
func trimGitHubCrawlLogs(configID, keep int) error {
	if configID <= 0 || keep <= 0 {
		return nil
	}
	var count int64
	if err := database.DB.Model(&GitHubCrawlLog{}).Where("config_id = ?", configID).Count(&count).Error; err != nil {
		return err
	}
	if count <= int64(keep) {
		return nil
	}
	// 第 keep 新的 id：保留 id >= 该值，删除更旧的
	var keepFrom struct {
		ID int
	}
	if err := database.DB.Model(&GitHubCrawlLog{}).
		Select("id").
		Where("config_id = ?", configID).
		Order("id DESC").
		Offset(keep - 1).
		Limit(1).
		Scan(&keepFrom).Error; err != nil {
		return err
	}
	if keepFrom.ID <= 0 {
		return nil
	}
	return database.DB.Where("config_id = ? AND id < ?", configID, keepFrom.ID).Delete(&GitHubCrawlLog{}).Error
}

// ListGitHubCrawlRuns 运行记录
func ListGitHubCrawlRuns(configID, limit int) ([]GitHubCrawlRun, error) {
	if limit <= 0 {
		limit = 20
	}
	if limit > 100 {
		limit = 100
	}
	var list []GitHubCrawlRun
	err := database.DB.Where("config_id = ?", configID).Order("id DESC").Limit(limit).Find(&list).Error
	return list, err
}

// ListGitHubCrawlLogs 抓取日志（支持 afterId 增量）。
// afterID=0 时返回最新 limit 条（升序）；afterID>0 时返回其后增量。
func ListGitHubCrawlLogs(runID, configID, afterID, limit int) ([]GitHubCrawlLog, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > githubCrawlLogMaxKeep {
		limit = githubCrawlLogMaxKeep
	}
	q := database.DB.Model(&GitHubCrawlLog{})
	if runID > 0 {
		q = q.Where("run_id = ?", runID)
	} else if configID > 0 {
		q = q.Where("config_id = ?", configID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
		var list []GitHubCrawlLog
		err := q.Order("id ASC").Limit(limit).Find(&list).Error
		return list, err
	}
	// 全量拉取：取最新 limit 条，再按 id 升序返回，便于前端直接展示
	var latest []GitHubCrawlLog
	if err := q.Order("id DESC").Limit(limit).Find(&latest).Error; err != nil {
		return nil, err
	}
	for i, j := 0, len(latest)-1; i < j; i, j = i+1, j-1 {
		latest[i], latest[j] = latest[j], latest[i]
	}
	return latest, nil
}

// ClearGitHubCrawlLogs 清空某配置日志
func ClearGitHubCrawlLogs(configID int) error {
	return database.DB.Where("config_id = ?", configID).Delete(&GitHubCrawlLog{}).Error
}

// GitHubCrawlNode 独立节点列表
type GitHubCrawlNode struct {
	ID             int       `gorm:"primaryKey" json:"id"`
	ConfigID       int       `gorm:"index" json:"configId"`
	RunID          int       `gorm:"index" json:"runId"`
	Link           string    `gorm:"type:text" json:"link"`
	Name           string    `json:"name"`
	Protocol       string    `gorm:"index" json:"protocol"`
	LinkAddress    string    `json:"linkAddress"`
	LinkHost       string    `json:"linkHost"`
	LinkPort       string    `json:"linkPort"`
	ContentHash    string    `gorm:"index" json:"contentHash"`
	DelayTime      int       `json:"delayTime"`
	Speed          float64   `json:"speed"`
	DelayStatus    string    `gorm:"default:'untested'" json:"delayStatus"`
	SpeedStatus    string    `gorm:"default:'untested'" json:"speedStatus"`
	IsValid        bool      `gorm:"index;default:false" json:"isValid"`
	Promoted       bool      `gorm:"index;default:false" json:"promoted"`
	PromotedNodeID int       `json:"promotedNodeId"`
	CreatedAt      time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt      time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

func (GitHubCrawlNode) TableName() string { return "github_crawl_nodes" }

func hashGitHubCrawlLink(link string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(link)))
	return hex.EncodeToString(sum[:])
}

// UpsertGitHubCrawlNodes 写入/更新独立节点（含测速字段）
func UpsertGitHubCrawlNodes(nodes []GitHubCrawlNode) (int, error) {
	added := 0
	for i := range nodes {
		n := &nodes[i]
		if strings.TrimSpace(n.Link) == "" {
			continue
		}
		if n.ContentHash == "" {
			n.ContentHash = hashGitHubCrawlLink(n.Link)
		}
		if n.DelayStatus == "" {
			n.DelayStatus = "untested"
		}
		if n.SpeedStatus == "" {
			n.SpeedStatus = "untested"
		}

		var existing GitHubCrawlNode
		err := database.DB.Where("config_id = ? AND content_hash = ?", n.ConfigID, n.ContentHash).First(&existing).Error
		if err == nil {
			updates := map[string]any{
				"delay_time":   n.DelayTime,
				"delay_status": n.DelayStatus,
				"speed":        n.Speed,
				"speed_status": n.SpeedStatus,
				"is_valid":     n.IsValid || existing.IsValid,
				"name":         n.Name,
				"protocol":     n.Protocol,
				"link_address": n.LinkAddress,
				"link_host":    n.LinkHost,
				"link_port":    n.LinkPort,
				"link":         n.Link,
				"run_id":       n.RunID,
			}
			_ = database.DB.Model(&existing).Updates(updates).Error
			continue
		}
		if err := database.DB.Create(n).Error; err != nil {
			continue
		}
		added++
	}
	return added, nil
}

// ListGitHubCrawlNodes 独立节点列表。
// limit<=0 表示不限制条数；仅当 limit>0 时按条数截断。
func ListGitHubCrawlNodes(configID int, onlyValid, onlyUnpromoted bool, limit int) ([]GitHubCrawlNode, error) {
	var list []GitHubCrawlNode
	q := database.DB.Where("config_id = ?", configID)
	if onlyValid {
		q = q.Where("is_valid = ?", true)
	}
	if onlyUnpromoted {
		q = q.Where("promoted = ?", false)
	}
	q = q.Order("id DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	err := q.Find(&list).Error
	return list, err
}

// GetGitHubCrawlNodesByIDs 按 ID 批量读取
func GetGitHubCrawlNodesByIDs(ids []int) ([]GitHubCrawlNode, error) {
	if len(ids) == 0 {
		return []GitHubCrawlNode{}, nil
	}
	var list []GitHubCrawlNode
	err := database.DB.Where("id IN ?", ids).Find(&list).Error
	return list, err
}

// ClearGitHubCrawlNodes 清空独立节点
func ClearGitHubCrawlNodes(configID int) error {
	return database.DB.Where("config_id = ?", configID).Delete(&GitHubCrawlNode{}).Error
}

// DeleteInvalidGitHubCrawlNodes 删除无效节点（is_valid = false）。
// 若节点曾加入总节点列表，同步从总列表删除对应节点，避免总表残留失效节点。
// 返回：独立节点删除数、总列表删除数。
func DeleteInvalidGitHubCrawlNodes(configID int) (crawlDeleted int64, totalDeleted int64, err error) {
	var nodes []GitHubCrawlNode
	if err = database.DB.Where("config_id = ? AND is_valid = ?", configID, false).Find(&nodes).Error; err != nil {
		return 0, 0, err
	}
	if len(nodes) == 0 {
		return 0, 0, nil
	}

	totalIDSet := make(map[int]struct{})
	links := make([]string, 0, len(nodes))
	crawlIDs := make([]int, 0, len(nodes))
	for _, n := range nodes {
		crawlIDs = append(crawlIDs, n.ID)
		if n.PromotedNodeID > 0 {
			totalIDSet[n.PromotedNodeID] = struct{}{}
		}
		if link := strings.TrimSpace(n.Link); link != "" {
			links = append(links, link)
		}
	}

	// 按本配置 github-crawl 来源 + 链接补齐，覆盖 promoted_node_id 丢失的情况
	if len(links) > 0 {
		var byLink []int
		if err = database.DB.Model(&Node{}).
			Where("source = ? AND source_id = ? AND link IN ?", "github-crawl", configID, links).
			Pluck("id", &byLink).Error; err != nil {
			return 0, 0, err
		}
		for _, id := range byLink {
			if id > 0 {
				totalIDSet[id] = struct{}{}
			}
		}
	}

	if len(totalIDSet) > 0 {
		totalIDs := make([]int, 0, len(totalIDSet))
		for id := range totalIDSet {
			if id > 0 {
				totalIDs = append(totalIDs, id)
			}
		}
		if len(totalIDs) > 0 {
			if err = BatchDel(totalIDs); err != nil {
				return 0, 0, err
			}
			totalDeleted = int64(len(totalIDs))
		}
	}

	res := database.DB.Where("config_id = ? AND id IN ?", configID, crawlIDs).Delete(&GitHubCrawlNode{})
	return res.RowsAffected, totalDeleted, res.Error
}

// DeleteGitHubCrawlNodesByIDs 按 ID 删除独立节点（限定 configID 防止误删）
func DeleteGitHubCrawlNodesByIDs(configID int, ids []int) (int64, error) {
	if len(ids) == 0 {
		return 0, nil
	}
	res := database.DB.Where("config_id = ? AND id IN ?", configID, ids).Delete(&GitHubCrawlNode{})
	return res.RowsAffected, res.Error
}

// ListAllGitHubCrawlNodeIDs 返回某配置下全部节点 ID
func ListAllGitHubCrawlNodeIDs(configID int) ([]int, error) {
	var ids []int
	err := database.DB.Model(&GitHubCrawlNode{}).Where("config_id = ?", configID).Order("id DESC").Pluck("id", &ids).Error
	return ids, err
}

// MarkGitHubCrawlNodesPromoted 标记已加入总节点列表
func MarkGitHubCrawlNodesPromoted(nodeIDs []int, crawlToNode map[int]int) error {
	for _, id := range nodeIDs {
		updates := map[string]any{"promoted": true}
		if nid, ok := crawlToNode[id]; ok && nid > 0 {
			updates["promoted_node_id"] = nid
		}
		if err := database.DB.Model(&GitHubCrawlNode{}).Where("id = ?", id).Updates(updates).Error; err != nil {
			return err
		}
	}
	return nil
}

// UpdateGitHubCrawlNodeTestResult 更新测延时/测速结果
func UpdateGitHubCrawlNodeTestResult(id int, delayTime int, delayStatus string, speed float64, speedStatus string, isValid bool) error {
	return database.DB.Model(&GitHubCrawlNode{}).Where("id = ?", id).Updates(map[string]any{
		"delay_time":   delayTime,
		"delay_status": delayStatus,
		"speed":        speed,
		"speed_status": speedStatus,
		"is_valid":     isValid,
	}).Error
}

// GitHubCrawlBlacklist 爬虫黑名单：链接失败 / 仓库全 404 / 多次 0 有效节点
type GitHubCrawlBlacklist struct {
	ID        int       `gorm:"primaryKey" json:"id"`
	ConfigID  int       `gorm:"index;not null" json:"configId"`
	Scope     string    `gorm:"size:16;index;not null" json:"scope"` // link | repo
	Target    string    `gorm:"type:text;not null" json:"target"`    // 链接 URL 或 owner/repo
	Repo      string    `gorm:"size:255;index" json:"repo"`          // 关联仓库，便于列表展示
	Reason    string    `gorm:"type:text" json:"reason"`
	HitCount  int       `gorm:"default:1" json:"hitCount"` // 命中/失败累计（0 有效节点次数等）
	CreatedAt time.Time `gorm:"autoCreateTime" json:"createdAt"`
	UpdatedAt time.Time `gorm:"autoUpdateTime" json:"updatedAt"`
}

func (GitHubCrawlBlacklist) TableName() string { return "github_crawl_blacklists" }

const (
	GitHubCrawlBlacklistScopeLink = "link"
	GitHubCrawlBlacklistScopeRepo = "repo"
	// 同一仓库累计多少次「拉取后 0 有效节点」则拉黑仓库
	GitHubCrawlZeroValidRepoThreshold = 3
)

func (b *GitHubCrawlBlacklist) normalize() {
	b.Scope = strings.TrimSpace(strings.ToLower(b.Scope))
	if b.Scope != GitHubCrawlBlacklistScopeRepo {
		b.Scope = GitHubCrawlBlacklistScopeLink
	}
	b.Target = strings.TrimSpace(b.Target)
	b.Repo = strings.TrimSpace(b.Repo)
	b.Reason = strings.TrimSpace(b.Reason)
	if b.HitCount <= 0 {
		b.HitCount = 1
	}
}

// ListGitHubCrawlBlacklists 列出某配置黑名单
func ListGitHubCrawlBlacklists(configID int) ([]GitHubCrawlBlacklist, error) {
	var list []GitHubCrawlBlacklist
	err := database.DB.Where("config_id = ?", configID).Order("id DESC").Find(&list).Error
	return list, err
}

// GetGitHubCrawlBlacklistByID 按 ID 读取（限定 config）
func GetGitHubCrawlBlacklistByID(configID, id int) (*GitHubCrawlBlacklist, error) {
	var b GitHubCrawlBlacklist
	if err := database.DB.Where("config_id = ? AND id = ?", configID, id).First(&b).Error; err != nil {
		return nil, err
	}
	return &b, nil
}

// AddGitHubCrawlBlacklist 新增；同 config+scope+target 已存在则更新 reason/hit_count
func AddGitHubCrawlBlacklist(configID int, scope, target, repo, reason string) error {
	b := &GitHubCrawlBlacklist{
		ConfigID: configID,
		Scope:    scope,
		Target:   target,
		Repo:     repo,
		Reason:   reason,
		HitCount: 1,
	}
	b.normalize()
	if b.Target == "" || configID <= 0 {
		return gorm.ErrInvalidData
	}
	var existing GitHubCrawlBlacklist
	err := database.DB.Where("config_id = ? AND scope = ? AND target = ?", configID, b.Scope, b.Target).First(&existing).Error
	if err == nil {
		updates := map[string]any{
			"hit_count": existing.HitCount + 1,
			"reason":    b.Reason,
		}
		if b.Repo != "" {
			updates["repo"] = b.Repo
		}
		return database.DB.Model(&existing).Updates(updates).Error
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return err
	}
	return database.DB.Create(b).Error
}

// UpdateGitHubCrawlBlacklist 更新
func UpdateGitHubCrawlBlacklist(configID, id int, scope, target, repo, reason string) error {
	b, err := GetGitHubCrawlBlacklistByID(configID, id)
	if err != nil {
		return err
	}
	b.Scope = scope
	b.Target = target
	b.Repo = repo
	b.Reason = reason
	b.normalize()
	if b.Target == "" {
		return gorm.ErrInvalidData
	}
	return database.DB.Model(b).Select("Scope", "Target", "Repo", "Reason").Updates(b).Error
}

// DeleteGitHubCrawlBlacklist 删除单条
func DeleteGitHubCrawlBlacklist(configID, id int) error {
	return database.DB.Where("config_id = ? AND id = ?", configID, id).Delete(&GitHubCrawlBlacklist{}).Error
}

// ClearGitHubCrawlBlacklists 清空某配置黑名单
func ClearGitHubCrawlBlacklists(configID int) error {
	return database.DB.Where("config_id = ?", configID).Delete(&GitHubCrawlBlacklist{}).Error
}

// LoadGitHubCrawlBlacklistSets 加载内存集合，供抓取前过滤
func LoadGitHubCrawlBlacklistSets(configID int) (links map[string]struct{}, repos map[string]struct{}, err error) {
	links = make(map[string]struct{})
	repos = make(map[string]struct{})
	list, err := ListGitHubCrawlBlacklists(configID)
	if err != nil {
		return links, repos, err
	}
	for _, b := range list {
		key := strings.ToLower(strings.TrimSpace(b.Target))
		if key == "" {
			continue
		}
		if b.Scope == GitHubCrawlBlacklistScopeRepo {
			repos[key] = struct{}{}
		} else {
			links[key] = struct{}{}
		}
	}
	return links, repos, nil
}

// RecordGitHubCrawlZeroValidRepo 记录仓库 0 有效节点次数，达到阈值则拉黑仓库。
// 返回是否已（或刚）拉黑仓库。
func RecordGitHubCrawlZeroValidRepo(configID int, repoFullName, reason string) (blacklisted bool, err error) {
	repoFullName = strings.TrimSpace(repoFullName)
	if configID <= 0 || repoFullName == "" || strings.EqualFold(repoFullName, "orphan") {
		return false, nil
	}
	// 已是仓库黑名单
	var existing GitHubCrawlBlacklist
	err = database.DB.Where("config_id = ? AND scope = ? AND target = ?",
		configID, GitHubCrawlBlacklistScopeRepo, repoFullName).First(&existing).Error
	if err == nil {
		_ = database.DB.Model(&existing).Updates(map[string]any{
			"hit_count": existing.HitCount + 1,
			"reason":    reason,
		}).Error
		return true, nil
	}
	if err != nil && err != gorm.ErrRecordNotFound {
		return false, err
	}
	// 用 link 作用域 + 特殊 target 记次：__zero_valid__:owner/repo
	counterKey := "__zero_valid__:" + repoFullName
	var counter GitHubCrawlBlacklist
	cerr := database.DB.Where("config_id = ? AND scope = ? AND target = ?",
		configID, GitHubCrawlBlacklistScopeLink, counterKey).First(&counter).Error
	if cerr == nil {
		newHit := counter.HitCount + 1
		_ = database.DB.Model(&counter).Update("hit_count", newHit).Error
		if newHit >= GitHubCrawlZeroValidRepoThreshold {
			_ = AddGitHubCrawlBlacklist(configID, GitHubCrawlBlacklistScopeRepo, repoFullName, repoFullName, reason)
			_ = database.DB.Delete(&counter).Error
			return true, nil
		}
		return false, nil
	}
	if cerr != nil && cerr != gorm.ErrRecordNotFound {
		return false, cerr
	}
	return false, database.DB.Create(&GitHubCrawlBlacklist{
		ConfigID: configID,
		Scope:    GitHubCrawlBlacklistScopeLink,
		Target:   counterKey,
		Repo:     repoFullName,
		Reason:   "zero_valid_counter",
		HitCount: 1,
	}).Error
}
