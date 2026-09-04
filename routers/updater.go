package routers

import (
	"sublink/api"
	"sublink/middlewares"

	"github.com/gin-gonic/gin"
)

// Updater 注册系统升级/成品库管理路由
func Updater(r *gin.Engine) {
	group := r.Group("/api/v1/updater")
	group.Use(middlewares.AuthToken)
	{
		group.GET("/status", api.UpdaterStatus)
		group.GET("/config", api.UpdaterGetConfig)
		group.PUT("/config", middlewares.DemoModeRestrict, api.UpdaterUpdateConfig)
		group.GET("/remote/versions", api.UpdaterRemoteVersions)
		group.POST("/upgrade", middlewares.DemoModeRestrict, api.UpdaterUpgrade)
		group.POST("/upload", middlewares.DemoModeRestrict, api.UpdaterUpload)
		group.GET("/artifacts", api.UpdaterArtifacts)
		group.POST("/artifacts/:id/rollback", middlewares.DemoModeRestrict, api.UpdaterRollback)
		group.DELETE("/artifacts/:id", middlewares.DemoModeRestrict, api.UpdaterDeleteArtifact)
	}
}
