package routers

import (
	"sublink/api"
	"sublink/middlewares"

	"github.com/gin-gonic/gin"
)

// GitHubCrawl 注册独立 GitHub 节点抓取相关路由
func GitHubCrawl(r *gin.Engine) {
	group := r.Group("/api/v1/github-crawl")
	group.Use(middlewares.AuthToken)
	{
		group.GET("", api.GitHubCrawlList)
		group.GET("/:id", api.GitHubCrawlGet)
		group.POST("", middlewares.DemoModeRestrict, api.GitHubCrawlAdd)
		group.PUT("/:id", middlewares.DemoModeRestrict, api.GitHubCrawlUpdate)
		group.DELETE("/:id", middlewares.DemoModeRestrict, api.GitHubCrawlDelete)
		group.POST("/:id/toggle", middlewares.DemoModeRestrict, api.GitHubCrawlToggle)
		group.POST("/:id/run", middlewares.DemoModeRestrict, api.GitHubCrawlRun)
		group.GET("/:id/logs", api.GitHubCrawlLogs)
		group.DELETE("/:id/logs", api.GitHubCrawlClearLogs)
		group.GET("/:id/runs", api.GitHubCrawlRuns)
		group.GET("/:id/nodes", api.GitHubCrawlNodes)
		// 更具体的路径需放在 /nodes 之前，避免被通配冲突；invalid 单独注册一次
		group.DELETE("/:id/nodes/invalid", middlewares.DemoModeRestrict, api.GitHubCrawlDeleteInvalidNodes)
		group.POST("/:id/nodes/delete", middlewares.DemoModeRestrict, api.GitHubCrawlDeleteNodes)
		group.DELETE("/:id/nodes", middlewares.DemoModeRestrict, api.GitHubCrawlClearNodes)
		group.POST("/:id/stop", middlewares.DemoModeRestrict, api.GitHubCrawlStop)
		group.POST("/:id/promote", middlewares.DemoModeRestrict, api.GitHubCrawlPromote)
		group.POST("/:id/test-delay", middlewares.DemoModeRestrict, api.GitHubCrawlTestDelay)
		group.POST("/:id/test-speed", middlewares.DemoModeRestrict, api.GitHubCrawlTestSpeed)
		group.POST("/:id/test", middlewares.DemoModeRestrict, api.GitHubCrawlTest)
	}
}
