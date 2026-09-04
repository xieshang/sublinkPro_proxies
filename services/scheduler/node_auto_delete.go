// node_auto_delete.go 实现「应用设置 → 节点自动处理」的连续失败自动删除：
// 每轮节点检测完成后，对命中配置分组的节点维护连续失败计数（成功清零、失败+1），
// 连续失败达到阈值的节点自动从节点库删除（复用 BatchDel 的事务与缓存清理）。
package scheduler

import (
	"strings"

	"sublink/constants"
	"sublink/models"
	"sublink/utils"
)

// handleAutoDeleteFailedNodes 维护本轮检测结果的连续失败计数并执行阈值删除。
// groupOf 为本轮参与检测的 节点ID→分组 映射；勾选了分组时仅统计命中的节点。
func handleAutoDeleteFailedNodes(results []models.SpeedTestResult, groupOf map[int]string) {
	enabled, threshold, groups := models.GetNodeAutoDeleteConfig()
	if !enabled || len(results) == 0 {
		return
	}
	groupSet := make(map[string]bool, len(groups))
	for _, g := range groups {
		groupSet[g] = true
	}

	failedIDs := make([]int, 0, 32)
	successIDs := make([]int, 0, len(results))
	for _, r := range results {
		if r.NodeID <= 0 {
			continue
		}
		// 分组范围过滤：未选中任何分组 = 全部分组
		if len(groupSet) > 0 {
			g, ok := groupOf[r.NodeID]
			if !ok || !groupSet[g] {
				continue
			}
		}
		switch r.DelayStatus {
		case constants.StatusSuccess:
			successIDs = append(successIDs, r.NodeID)
		case constants.StatusTimeout, constants.StatusError:
			failedIDs = append(failedIDs, r.NodeID)
		default:
			// untested 等状态不参与计数
		}
	}

	if err := models.UpdateConsecutiveFailures(failedIDs, successIDs); err != nil {
		utils.Error("更新节点连续失败计数失败: %v", err)
		return
	}
	if len(failedIDs) == 0 {
		return
	}

	names, err := models.DeleteNodesOverFailureThreshold(threshold, groups)
	if err != nil {
		utils.Error("连续失败自动删除执行失败: %v", err)
		return
	}
	if len(names) > 0 {
		scope := "全部分组"
		if len(groups) > 0 {
			scope = "分组 " + strings.Join(groups, ", ")
		}
		utils.Warn("[节点自动处理] %d 个节点连续检测失败达 %d 次（%s），已自动删除: %s",
			len(names), threshold, scope, strings.Join(names, ", "))
	}
}
