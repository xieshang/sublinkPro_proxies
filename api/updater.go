package api

import (
	"fmt"
	"os"
	"strings"

	"sublink/models"
	"sublink/services"
	"sublink/services/updater"
	"sublink/utils"

	"github.com/gin-gonic/gin"
)

// getUpdaterManager 获取注入了当前版本号的 updater 管理器单例
func getUpdaterManager() *updater.Manager {
	m := updater.GetManager()
	if v := GetCurrentVersion(); v != "" {
		m.SetAppVersion(v)
	}
	return m
}

// UpdaterStatus GET /api/v1/updater/status
func UpdaterStatus(c *gin.Context) {
	m := getUpdaterManager()
	cfg, err := m.LoadConfig()
	if err != nil {
		utils.FailWithMsg(c, "读取升级配置失败: "+err.Error())
		return
	}
	state, err := m.LoadState()
	if err != nil {
		utils.FailWithMsg(c, "读取操作状态失败: "+err.Error())
		return
	}
	exePath, _ := os.Executable()
	platform := updater.DetectPlatform()
	utils.OkDetailed(c, "获取升级状态成功", gin.H{
		"version":       m.AppVersion(),
		"platform":      platform,
		"exePath":       exePath,
		"config":        cfg,
		"busy":          m.IsBusy(),
		"lastOperation": state,
	})
}

// UpdaterGetConfig GET /api/v1/updater/config
func UpdaterGetConfig(c *gin.Context) {
	cfg, err := getUpdaterManager().LoadConfig()
	if err != nil {
		utils.FailWithMsg(c, "读取升级配置失败: "+err.Error())
		return
	}
	utils.OkDetailed(c, "获取升级配置成功", cfg)
}

type updaterConfigRequest struct {
	SourceType    string `json:"sourceType"`
	ManifestURL   string `json:"manifestUrl"`
	TemplateURL   string `json:"templateUrl"`
	GitHubRepo    string `json:"githubRepo"`
	GitHubToken   string `json:"githubToken"`
	UseProxy      bool   `json:"useProxy"`
	KeepArtifacts int    `json:"keepArtifacts"`
}

// UpdaterUpdateConfig PUT /api/v1/updater/config
func UpdaterUpdateConfig(c *gin.Context) {
	var req updaterConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		utils.FailWithMsg(c, "参数解析失败: "+err.Error())
		return
	}
	m := getUpdaterManager()
	cfg, err := m.LoadConfig()
	if err != nil {
		utils.FailWithMsg(c, "读取现有配置失败: "+err.Error())
		return
	}
	// 与其它字段一致采用全量覆盖；前端编辑时会把读取到的配置原样带回
	cfg.SourceType = updater.SourceType(req.SourceType)
	cfg.ManifestURL = req.ManifestURL
	cfg.TemplateURL = req.TemplateURL
	cfg.GitHubRepo = req.GitHubRepo
	cfg.GitHubToken = req.GitHubToken
	cfg.UseProxy = req.UseProxy
	cfg.KeepArtifacts = req.KeepArtifacts
	if err := m.SaveConfig(cfg); err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	utils.OkDetailed(c, "升级配置已保存", cfg)
}

// UpdaterRemoteVersions GET /api/v1/updater/remote/versions
// 返回远程清单的全部版本，并标注每个版本是否有适配当前平台的文件。
func UpdaterRemoteVersions(c *gin.Context) {
	m := getUpdaterManager()
	cfg, err := m.LoadConfig()
	if err != nil {
		utils.FailWithMsg(c, "读取升级配置失败: "+err.Error())
		return
	}
	if err := cfg.Validate(); err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	platform := updater.DetectPlatform()

	type remoteFileView struct {
		updater.RemoteFile
		Matched bool `json:"matched"` // 是否匹配当前平台
	}
	type versionView struct {
		Version     string           `json:"version"`
		Notes       string           `json:"notes,omitempty"`
		PublishedAt interface{}      `json:"publishedAt,omitempty"`
		Files       []remoteFileView `json:"files"`
		Installable bool             `json:"installable"` // 当前平台可安装
		IsLatest    bool             `json:"isLatest"`
		IsCurrent   bool             `json:"isCurrent"`
	}

	switch cfg.SourceType {
	case updater.SourceManifest, updater.SourceGitHub:
		var manifest *updater.RemoteManifest
		var ferr error
		if cfg.SourceType == updater.SourceManifest {
			manifest, ferr = m.FetchManifest(cfg)
		} else {
			manifest, ferr = m.FetchGitHubReleases(cfg)
		}
		if ferr != nil {
			utils.FailWithMsg(c, ferr.Error())
			return
		}
		list := make([]versionView, 0, len(manifest.Versions))
		for _, v := range manifest.Versions {
			view := versionView{
				Version:     v.Version,
				Notes:       v.Notes,
				PublishedAt: v.PublishedAt,
				IsLatest:    v.Version == manifest.Latest,
				IsCurrent:   updater.CompareVersions(v.Version, m.AppVersion()) == 0,
				Files:       make([]remoteFileView, 0, len(v.Files)),
			}
			for _, f := range v.Files {
				matched := fileMatchesPlatform(f, platform)
				view.Files = append(view.Files, remoteFileView{RemoteFile: *f, Matched: matched})
				if matched {
					view.Installable = true
				}
			}
			list = append(list, view)
		}
		utils.OkDetailed(c, "获取远程版本列表成功", gin.H{
			"sourceType": string(cfg.SourceType),
			"latest":     manifest.Latest,
			"versions":   list,
			"platform":   platform,
		})
	case updater.SourceTemplate:
		url := updater.RenderTemplate(cfg.TemplateURL, platform.OS, platform.Arch, platform.Ext, "")
		utils.OkDetailed(c, "模板模式返回渲染地址", gin.H{
			"sourceType":  string(cfg.SourceType),
			"platform":    platform,
			"renderedUrl": url,
			"templateTip": "模板模式无法列出历史版本，直接使用「立即升级」按占位符下载",
		})
	default:
		utils.FailWithMsg(c, fmt.Sprintf("不支持的升级源类型: %s", cfg.SourceType))
	}
}

func fileMatchesPlatform(f *updater.RemoteFile, p updater.PlatformInfo) bool {
	osMatch := f.OS == "" || f.OS == p.OS || f.OS == "any" || f.OS == "*"
	archMatch := f.Arch == "" || f.Arch == p.Arch || f.Arch == "any" || f.Arch == "*"
	return osMatch && archMatch
}

type updaterUpgradeRequest struct {
	Version string `json:"version"`
}

// UpdaterUpgrade POST /api/v1/updater/upgrade
// 异步启动升级流水线；进度通过任务中心（system_update 类型）与 SSE 推送。
func UpdaterUpgrade(c *gin.Context) {
	var req updaterUpgradeRequest
	_ = c.ShouldBindJSON(&req) // body 可为空：空版本号表示安装 latest

	m := getUpdaterManager()
	if m.IsBusy() {
		utils.FailWithMsg(c, updater.ErrBusy.Error())
		return
	}

	tm := services.GetTaskManager()
	taskName := "系统升级"
	if req.Version != "" {
		taskName = fmt.Sprintf("系统升级 → %s", req.Version)
	}
	task, _, terr := tm.CreateTask(models.TaskTypeSystemUpdate, taskName, models.TaskTriggerManual, 100)
	if terr != nil {
		utils.Warn("[updater] 创建任务失败: %v，降级为无进度报告", terr)
	}
	report := buildUpdaterReporter(tm, task)

	if err := m.StartUpgrade(req.Version, report); err != nil {
		if task != nil {
			_ = tm.FailTask(task.ID, "启动升级失败: "+err.Error())
		}
		utils.FailWithMsg(c, err.Error())
		return
	}
	msg := "升级已启动"
	if req.Version != "" {
		msg = fmt.Sprintf("升级到 %s 已启动", req.Version)
	}
	utils.OkDetailed(c, msg, gin.H{
		"taskId":  taskIDOf(task),
		"version": req.Version,
	})
}

// UpdaterUpload POST /api/v1/updater/upload (multipart/form-data)
// 手动上传成品升级：file=二进制或 zip/tar.gz 归档，version=可选版本号覆盖。
// 上传后自动走完整流水线：test 试运行 → 快照当前版 → 原子替换 → 重启。
func UpdaterUpload(c *gin.Context) {
	m := getUpdaterManager()
	if m.IsBusy() {
		utils.FailWithMsg(c, updater.ErrBusy.Error())
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		utils.FailWithMsg(c, "缺少上传文件字段 file: "+err.Error())
		return
	}
	const maxUploadSize = 512 << 20 // 512MB，与下载上限一致
	if fileHeader.Size <= 0 || fileHeader.Size > maxUploadSize {
		utils.FailWithMsg(c, fmt.Sprintf("文件大小无效（1B ~ 512MB），当前 %d 字节", fileHeader.Size))
		return
	}
	stagingPath, err := m.NewStagingFile(fileHeader.Filename)
	if err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	if err := c.SaveUploadedFile(fileHeader, stagingPath); err != nil {
		utils.FailWithMsg(c, "保存上传文件失败: "+err.Error())
		return
	}

	version := strings.TrimSpace(c.PostForm("version"))
	tm := services.GetTaskManager()
	taskName := "系统升级（手动上传）"
	if version != "" {
		taskName = fmt.Sprintf("系统升级（上传）→ %s", version)
	}
	task, _, terr := tm.CreateTask(models.TaskTypeSystemUpdate, taskName, models.TaskTriggerManual, 100)
	if terr != nil {
		utils.Warn("[updater] 创建任务失败: %v，降级为无进度报告", terr)
	}
	report := buildUpdaterReporter(tm, task)

	if err := m.StartUploadUpgrade(stagingPath, report); err != nil {
		_ = os.Remove(stagingPath)
		if task != nil {
			_ = tm.FailTask(task.ID, "启动上传升级失败: "+err.Error())
		}
		utils.FailWithMsg(c, err.Error())
		return
	}
	utils.OkDetailed(c, "上传升级已启动：test 通过后将自动替换并重启", gin.H{
		"taskId":   taskIDOf(task),
		"fileName": fileHeader.Filename,
		"size":     fileHeader.Size,
	})
}

// UpdaterRollback POST /api/v1/updater/artifacts/:id/rollback
func UpdaterRollback(c *gin.Context) {
	id := c.Param("id")
	if id == "" {
		utils.FailWithMsg(c, "缺少成品 ID")
		return
	}
	m := getUpdaterManager()
	if m.IsBusy() {
		utils.FailWithMsg(c, updater.ErrBusy.Error())
		return
	}

	tm := services.GetTaskManager()
	task, _, terr := tm.CreateTask(models.TaskTypeSystemUpdate, fmt.Sprintf("回退 → %s", id), models.TaskTriggerManual, 100)
	if terr != nil {
		utils.Warn("[updater] 创建任务失败: %v，降级为无进度报告", terr)
	}
	report := buildUpdaterReporter(tm, task)

	if err := m.StartRollback(id, report); err != nil {
		if task != nil {
			_ = tm.FailTask(task.ID, "启动回退失败: "+err.Error())
		}
		utils.FailWithMsg(c, err.Error())
		return
	}
	utils.OkDetailed(c, fmt.Sprintf("回退到 %s 已启动", id), gin.H{"taskId": taskIDOf(task), "artifactId": id})
}

// UpdaterArtifacts GET /api/v1/updater/artifacts
func UpdaterArtifacts(c *gin.Context) {
	list, err := getUpdaterManager().ListArtifacts()
	if err != nil {
		utils.FailWithMsg(c, "读取成品库失败: "+err.Error())
		return
	}
	if list == nil {
		list = []*updater.Artifact{}
	}
	utils.OkDetailed(c, "获取成品库成功", gin.H{
		"artifacts": list,
		"current":   getUpdaterManager().AppVersion(),
	})
}

// UpdaterDeleteArtifact DELETE /api/v1/updater/artifacts/:id
func UpdaterDeleteArtifact(c *gin.Context) {
	id := c.Param("id")
	if err := getUpdaterManager().DeleteArtifact(id); err != nil {
		utils.FailWithMsg(c, err.Error())
		return
	}
	utils.OkWithMsg(c, "成品已删除")
}

// buildUpdaterReporter 把 updater 进度回调桥接到任务中心 + SSE
func buildUpdaterReporter(tm *services.TaskManager, task *models.Task) updater.Reporter {
	if tm == nil || task == nil {
		return func(int, string, string) {}
	}
	return func(percent int, step, message string) {
		_ = tm.UpdateProgress(task.ID, percent, step, map[string]any{
			"step":    step,
			"percent": percent,
			"message": message,
		})
		switch step {
		case "failed":
			_ = tm.FailTask(task.ID, message)
		case "restart":
			// 重启后进程退出，任务以完成态收尾
			_ = tm.CompleteTask(task.ID, message, map[string]any{"step": step, "message": message})
		default:
			// 中间步骤仅更新进度
		}
	}
}

func taskIDOf(task *models.Task) string {
	if task == nil {
		return ""
	}
	return task.ID
}
