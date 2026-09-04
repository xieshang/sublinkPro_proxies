package api

import (
	"sublink/config"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// currentAppVersion 当前运行实例的版本号（routers.Version 注册时写入，供升级等模块使用）
var currentAppVersion string

// GetCurrentVersion 返回当前应用版本号
func GetCurrentVersion() string { return currentAppVersion }

// GetVersion 返回当前版本号和启用的功能列表。
func GetVersion(version string) gin.HandlerFunc {
	currentAppVersion = version
	return func(c *gin.Context) {
		utils.OkDetailed(c, "获取版本成功", gin.H{
			"version":  version,
			"features": config.GetEnabledFeatures(),
		})
	}
}
