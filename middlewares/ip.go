package middlewares

import (
	"sublink/models"
	"sublink/services/geoip"
	"sublink/utils"
	"time"

	"github.com/gin-gonic/gin"
)

// ipLogTask 携带记录 IP 访问日志所需的全部上下文。
// 必须在 handler 返回前从 gin.Context 中提取，
// 因为 gin.Context 在请求结束后会被复用，不能跨 goroutine 持有。
type ipLogTask struct {
	ip      string
	subname string
	shareID int
}

// ipLogQueue IP 日志异步写入队列。
// 缓冲 1024 条，队列满时丢弃并记录错误日志，避免慢消费拖垮 HTTP 服务；
// 单 worker 串行消费，天然规避同一 (IP, 订阅, 分享) 记录的并发读写竞态，
// 同时将订阅访问热路径上的 SQLite 写操作全部移出响应链路。
var ipLogQueue = make(chan ipLogTask, 1024)

func init() {
	go func() {
		for task := range ipLogQueue {
			recordIPLog(task)
		}
	}()
}

// recordIPLog 执行 GeoIP 归属地查询与订阅访问日志的落库（在后台 worker 中运行）。
func recordIPLog(task ipLogTask) {
	// Get location from local GeoIP database
	addr, err := geoip.GetLocation(task.ip)
	if err != nil {
		utils.Error("Failed to get location for IP %s: %v", task.ip, err)
		addr = "Unknown"
	}

	var sub models.Subcription
	sub.Name = task.subname
	if err := sub.Find(); err != nil {
		utils.Error("查找订阅失败: %s", err.Error())
		return
	}

	var iplog models.SubLogs
	iplog.IP = task.ip

	// 使用 FindByShare 精确查找
	err = iplog.FindByShare(sub.ID, task.shareID)
	// 如果没有找到记录
	if err != nil {
		iplog.Addr = addr
		iplog.SubcriptionID = sub.ID
		iplog.ShareID = task.shareID
		iplog.Date = time.Now().Format("2006-01-02 15:04:05")
		iplog.Count = 1
		if err := iplog.Add(); err != nil {
			utils.Error("Failed to add new IP log: %v", err)
		}
		return
	}

	// 更新访问次数
	iplog.Count++
	iplog.Addr = addr
	iplog.Date = time.Now().Format("2006-01-02 15:04:05")
	if err := iplog.Update(); err != nil {
		utils.Error("更新IP日志失败: %s", err.Error())
	}
}

// GetIp 订阅访问日志中间件。
// 仅同步提取请求上下文数据，实际的 GeoIP 查询与数据库读写均由后台 worker 异步完成，
// 避免每次订阅拉取都在响应链路上等待磁盘写入（SQLite 写锁争用时可达秒级）。
func GetIp(c *gin.Context) {
	c.Next()

	task := ipLogTask{
		ip: c.ClientIP(),
	}

	subname, _ := c.Get("subname")
	if s, ok := subname.(string); ok {
		task.subname = s
	}
	shareIDVal, _ := c.Get("shareID")
	if sid, ok := shareIDVal.(int); ok {
		task.shareID = sid
	}

	select {
	case ipLogQueue <- task:
	default:
		utils.Error("IP日志队列已满，丢弃本次访问日志: ip=%s sub=%s", task.ip, task.subname)
	}
}
