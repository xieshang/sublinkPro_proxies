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

// AppendGitHubCrawlLog 追加日志
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
	return database.DB.Create(log).Error
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

// ListGitHubCrawlLogs 抓取日志（支持 afterId 增量）
func ListGitHubCrawlLogs(runID, configID, afterID, limit int) ([]GitHubCrawlLog, error) {
	if limit <= 0 {
		limit = 200
	}
	if limit > 500 {
		limit = 500
	}
	q := database.DB.Model(&GitHubCrawlLog{})
	if runID > 0 {
		q = q.Where("run_id = ?", runID)
	} else if configID > 0 {
		q = q.Where("config_id = ?", configID)
	}
	if afterID > 0 {
		q = q.Where("id > ?", afterID)
	}
	var list []GitHubCrawlLog
	err := q.Order("id ASC").Limit(limit).Find(&list).Error
	return list, err
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

// DeleteInvalidGitHubCrawlNodes 删除无效节点（is_valid = false）
func DeleteInvalidGitHubCrawlNodes(configID int) (int64, error) {
	res := database.DB.Where("config_id = ? AND is_valid = ?", configID, false).Delete(&GitHubCrawlNode{})
	return res.RowsAffected, res.Error
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
