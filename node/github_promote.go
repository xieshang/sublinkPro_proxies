package node

import (
	"fmt"
	"strings"

	"sublink/models"
	"sublink/utils"
)

// findExistingTotalNode 按链接查找总节点列表中的已有节点。
// 优先使用 link/link_hash 去重（与 nodes 表唯一约束一致），避免 github 侧 content_hash
// （原始链接 SHA）与总表 content_hash（协议结构化哈希）不一致导致误判为“可新增”。
func findExistingTotalNode(link string) (*models.Node, bool) {
	link = strings.TrimSpace(link)
	if link == "" {
		return nil, false
	}
	n := &models.Node{Link: link}
	if err := n.Find(); err == nil && n.ID > 0 {
		return n, true
	}
	return nil, false
}

// loadExistingTotalNodeIDs 批量确认 promoted_node_id 是否仍在总表。
func loadExistingTotalNodeIDs(ids []int) map[int]bool {
	out := make(map[int]bool, len(ids))
	if len(ids) == 0 {
		return out
	}
	uniq := make([]int, 0, len(ids))
	seen := make(map[int]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return out
	}
	nodes, err := models.GetNodesByIDs(uniq)
	if err != nil {
		utils.Warn("load existing total nodes for promote failed: %v", err)
		return out
	}
	for _, n := range nodes {
		if n.ID > 0 {
			out[n.ID] = true
		}
	}
	return out
}

// PromoteGitHubCrawlNodesToTotal 将独立节点加入总节点列表。
// 返回：成功标记数、跳过数、因错误未能处理数。
//
// 若爬虫侧已标 promoted，但总表对应节点已被删除（孤儿标记），会自动重新加入，
// 避免出现「总表搜不到 + 全部跳过」的死状态。
func PromoteGitHubCrawlNodesToTotal(cfg *models.GitHubCrawlConfig, nodes []models.GitHubCrawlNode) (promoted, skipped, failed int) {
	if cfg == nil || len(nodes) == 0 {
		return 0, 0, 0
	}
	group := strings.TrimSpace(cfg.Group)
	if group == "" {
		group = "github"
	}
	reserved := make(map[string]bool)
	idMap := make(map[int]int)
	successIDs := make([]int, 0, len(nodes))

	// 预加载已声明的 promoted_node_id，批量验证总表是否仍存在
	checkIDs := make([]int, 0, len(nodes))
	for _, gn := range nodes {
		if gn.Promoted && gn.PromotedNodeID > 0 {
			checkIDs = append(checkIDs, gn.PromotedNodeID)
		}
	}
	existingTotalIDs := loadExistingTotalNodeIDs(checkIDs)

	for _, gn := range nodes {
		if cfg.ID > 0 && gn.ConfigID != cfg.ID {
			continue
		}
		link := strings.TrimSpace(gn.Link)
		if link == "" {
			skipped++
			continue
		}

		// 已标记加入：校验总表节点是否仍在
		if gn.Promoted && gn.PromotedNodeID > 0 {
			if existingTotalIDs[gn.PromotedNodeID] {
				skipped++
				continue
			}
			// 总表 ID 已失效：尝试按链接找回
			if existing, ok := findExistingTotalNode(link); ok {
				idMap[gn.ID] = existing.ID
				successIDs = append(successIDs, gn.ID)
				promoted++
				continue
			}
			// 孤儿标记：总表无此节点，重新加入
			utils.Info("github crawl node %d promoted_node_id=%d missing in total list, re-promote", gn.ID, gn.PromotedNodeID)
		} else if gn.Promoted {
			// promoted 但无 promoted_node_id：按链接确认或重入
			if existing, ok := findExistingTotalNode(link); ok {
				idMap[gn.ID] = existing.ID
				successIDs = append(successIDs, gn.ID)
				promoted++
				continue
			}
		} else {
			// 未标记：总表已有同链接则只回写标记
			if existing, ok := findExistingTotalNode(link); ok {
				idMap[gn.ID] = existing.ID
				successIDs = append(successIDs, gn.ID)
				promoted++
				continue
			}
		}

		baseName := strings.TrimSpace(gn.Name)
		if baseName == "" {
			baseName = "github-node"
		}
		uniqueName := models.GenerateUniqueNodeNameWithSource(baseName, cfg.Name, 0, reserved)
		n := &models.Node{
			Link:        link,
			Name:        uniqueName,
			LinkName:    baseName,
			Protocol:    gn.Protocol,
			LinkAddress: gn.LinkAddress,
			LinkHost:    gn.LinkHost,
			LinkPort:    gn.LinkPort,
			Source:      "github-crawl",
			SourceID:    cfg.ID,
			Group:       group,
			// 不强制写入 github 侧 raw-link hash；由后续测速/规范化流程补齐协议 content_hash
			DelayTime:   gn.DelayTime,
			Speed:       gn.Speed,
			DelayStatus: gn.DelayStatus,
			SpeedStatus: gn.SpeedStatus,
		}
		if n.DelayStatus == "" {
			n.DelayStatus = "untested"
		}
		if n.SpeedStatus == "" {
			n.SpeedStatus = "untested"
		}
		models.NormalizeNodeForImport(n)

		if err := n.Add(); err != nil {
			// 并发/竞态或 hash 差异导致唯一约束冲突：回退按链接查找并标记
			if existing, ok := findExistingTotalNode(link); ok {
				idMap[gn.ID] = existing.ID
				successIDs = append(successIDs, gn.ID)
				promoted++
				continue
			}
			utils.Warn("promote github crawl node failed id=%d: %v", gn.ID, err)
			failed++
			continue
		}
		idMap[gn.ID] = n.ID
		successIDs = append(successIDs, gn.ID)
		promoted++
	}

	if len(successIDs) > 0 {
		if err := models.MarkGitHubCrawlNodesPromoted(successIDs, idMap); err != nil {
			utils.Warn("batch mark github crawl nodes promoted failed: %v", err)
			// 回退逐条标记，尽量保住成功状态
			for _, id := range successIDs {
				one := map[int]int{id: idMap[id]}
				if merr := models.MarkGitHubCrawlNodesPromoted([]int{id}, one); merr != nil {
					utils.Warn("mark github crawl node promoted failed id=%d: %v", id, merr)
					failed++
					if promoted > 0 {
						promoted--
					}
				}
			}
		}
	}
	return promoted, skipped, failed
}

// FormatPromoteSummary 生成加入总表结果摘要
func FormatPromoteSummary(promoted, skipped, failed int) string {
	return fmt.Sprintf("加入总节点列表：成功 %d，跳过 %d，失败 %d", promoted, skipped, failed)
}
