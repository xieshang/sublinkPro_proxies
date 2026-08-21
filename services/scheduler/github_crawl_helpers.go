package scheduler

import (
	"strings"

	"sublink/models"
)

// GitHubCrawlNodesToLinkItems 把 GitHub 独立节点转换为通用链路测试输入。
// 过滤掉 link 为空或不属于 configID 的脏数据。
func GitHubCrawlNodesToLinkItems(configID int, nodes []models.GitHubCrawlNode) []LinkTestItem {
	items := make([]LinkTestItem, 0, len(nodes))
	for _, gn := range nodes {
		if gn.ConfigID != configID || strings.TrimSpace(gn.Link) == "" {
			continue
		}
		items = append(items, LinkTestItem{
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

// githubCrawlNodesToLinkItems 兼容旧调用（包内使用）
func githubCrawlNodesToLinkItems(configID int, nodes []models.GitHubCrawlNode) []LinkTestItem {
	return GitHubCrawlNodesToLinkItems(configID, nodes)
}
