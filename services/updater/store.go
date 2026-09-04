// Package updater 实现 SublinkPro 编译成品管理体系：
//   - 本地成品库：按 版本-平台-架构 存放下载/备份的二进制成品，versions.json 作为版本账本；
//   - 升级流水线：远程清单(JSON)/模板 URL 下载 → sha256 校验 → test 模式试运行 → 原子替换 → 重启；
//   - 回退：从本地账本任选历史成品，重新验证后切回。
package updater

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"sublink/config"
	"sublink/utils"
)

const (
	// SchemaVersion versions.json 结构版本，后续结构变更时用于迁移判断
	SchemaVersion = 1

	// ArtifactStatusActive 当前正在运行的版本对应的成品
	ArtifactStatusActive = "active"
	// ArtifactStatusBackup 被新版本替换下来的历史成品（可回退）
	ArtifactStatusBackup = "backup"
	// ArtifactStatusArchived 手动导入/其它来源的存档成品（可回退）
	ArtifactStatusArchived = "archived"

	configFileName  = "config.json"
	ledgerFileName  = "versions.json"
	stateFileName   = "state.json"
	artifactDirName = "artifacts"
	stagingDirName  = "staging"

	// maxLedgerArtifacts 成品账本保留上限，超出后从最旧的 backup/archived 开始淘汰
	maxLedgerArtifacts = 20
)

// ErrBusy 已有升级/回退任务在执行中
var ErrBusy = errors.New("已有升级或回退任务正在执行")

// SourceType 升级源类型
type SourceType string

const (
	// SourceManifest 远程 JSON 清单：一个 URL 描述多版本多平台成品
	SourceManifest SourceType = "manifest"
	// SourceTemplate 单文件模板 URL：通过 {os}/{arch}/{ext} 占位符定位单平台文件
	SourceTemplate SourceType = "template"
	// SourceGitHub GitHub Releases：直接枚举仓库全部历史发布及其资产
	SourceGitHub SourceType = "github"
	// SourceGitea Gitea Releases：自建 Gitea 实例，API 与 GitHub 高度兼容
	SourceGitea SourceType = "gitea"
)

// UpdaterConfig 升级源配置（页面可编辑，落盘 config.json）
type UpdaterConfig struct {
	SourceType    SourceType `json:"sourceType"`    // manifest | template | github | gitea
	ManifestURL   string     `json:"manifestUrl"`   // 清单模式：JSON 清单地址
	TemplateURL   string     `json:"templateUrl"`   // 模板模式：支持 {os} {arch} {ext} 占位符
	GitHubRepo    string     `json:"githubRepo"`    // GitHub 模式：owner/repo
	GitHubToken   string     `json:"githubToken"`   // GitHub 模式：可选令牌（私有库/提升速率限制）
	GiteaURL      string     `json:"giteaUrl"`      // Gitea 模式：实例地址（如 https://gitea.example.com）
	GiteaRepo     string     `json:"giteaRepo"`     // Gitea 模式：owner/repo
	GiteaToken    string     `json:"giteaToken"`    // Gitea 模式：访问令牌（私有库必填）
	UseProxy      bool       `json:"useProxy"`      // 下载是否走 mihomo 代理
	KeepArtifacts int        `json:"keepArtifacts"` // 成品库保留数量上限（含 active），最小 2
	UpdatedAt     time.Time  `json:"updatedAt"`     // 最近修改时间
}

// normalize 补齐默认值并修剪空白
func (c *UpdaterConfig) normalize() {
	c.ManifestURL = strings.TrimSpace(c.ManifestURL)
	c.TemplateURL = strings.TrimSpace(c.TemplateURL)
	c.GitHubRepo = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(c.GitHubRepo), "https://github.com/"))
	c.GitHubRepo = strings.TrimSuffix(c.GitHubRepo, ".git")
	c.GitHubToken = strings.TrimSpace(c.GitHubToken)
	c.GiteaURL = strings.TrimRight(strings.TrimSpace(c.GiteaURL), "/")
	c.GiteaRepo = strings.TrimSpace(strings.TrimPrefix(c.GiteaRepo, c.GiteaURL))
	c.GiteaToken = strings.TrimSpace(c.GiteaToken)
	if c.SourceType != SourceManifest && c.SourceType != SourceTemplate && c.SourceType != SourceGitHub && c.SourceType != SourceGitea {
		if c.TemplateURL != "" && c.ManifestURL == "" {
			c.SourceType = SourceTemplate
		} else {
			c.SourceType = SourceManifest
		}
	}
	if c.KeepArtifacts < 2 {
		c.KeepArtifacts = 10
	}
	if c.KeepArtifacts > maxLedgerArtifacts {
		c.KeepArtifacts = maxLedgerArtifacts
	}
	c.UpdatedAt = time.Now()
}

// Validate 配置合法性校验
func (c *UpdaterConfig) Validate() error {
	c.normalize()
	switch c.SourceType {
	case SourceManifest:
		if c.ManifestURL == "" {
			return errors.New("清单模式下必须填写 JSON 清单地址")
		}
		if !strings.HasPrefix(c.ManifestURL, "http://") && !strings.HasPrefix(c.ManifestURL, "https://") {
			return errors.New("清单地址必须是 http(s) URL")
		}
	case SourceTemplate:
		if c.TemplateURL == "" {
			return errors.New("模板模式下必须填写下载地址")
		}
		if !strings.HasPrefix(c.TemplateURL, "http://") && !strings.HasPrefix(c.TemplateURL, "https://") {
			return errors.New("下载地址必须是 http(s) URL")
		}
	case SourceGitHub:
		if c.GitHubRepo == "" {
			return errors.New("GitHub 模式下必须填写仓库（owner/repo）")
		}
		parts := strings.Split(c.GitHubRepo, "/")
		if len(parts) != 2 || strings.TrimSpace(parts[0]) == "" || strings.TrimSpace(parts[1]) == "" {
			return errors.New("GitHub 仓库格式必须为 owner/repo")
		}
	case SourceGitea:
		if c.GiteaURL == "" {
			return errors.New("Gitea 模式下必须填写实例地址")
		}
		if !strings.HasPrefix(c.GiteaURL, "http://") && !strings.HasPrefix(c.GiteaURL, "https://") {
			return errors.New("Gitea 实例地址必须是 http(s) URL")
		}
		if c.GiteaRepo == "" {
			return errors.New("Gitea 模式下必须填写仓库（owner/repo）")
		}
		gParts := strings.Split(c.GiteaRepo, "/")
		if len(gParts) != 2 || strings.TrimSpace(gParts[0]) == "" || strings.TrimSpace(gParts[1]) == "" {
			return errors.New("Gitea 仓库格式必须为 owner/repo")
		}
	default:
		return fmt.Errorf("不支持的升级源类型: %s", c.SourceType)
	}
	return nil
}

// Artifact 成品库中的一条编译产物记录
type Artifact struct {
	ID          string    `json:"id"`                        // {version}-{os}-{arch}
	Version     string    `json:"version"`                   // 版本号（来自清单宣称或试运行实测）
	ObservedVer string    `json:"observedVersion,omitempty"` // test 模式实测输出版本号
	OS          string    `json:"os"`
	Arch        string    `json:"arch"`
	FileName    string    `json:"fileName"` // artifacts/ 目录下的文件名
	Size        int64     `json:"size"`
	SHA256      string    `json:"sha256,omitempty"`      // 入库时计算的校验和
	Status      string    `json:"status"`                // active | backup | archived
	SourceURL   string    `json:"sourceUrl,omitempty"`   // 下载来源
	Note        string    `json:"note,omitempty"`        // 备注（如回退原因、发布说明摘要）
	TestedAt    time.Time `json:"testedAt"`              // 最后一次 test 验证时间
	CreatedAt   time.Time `json:"createdAt"`             // 入库时间
	ActivatedAt time.Time `json:"activatedAt,omitempty"` // 最后一次被切换为运行版本的时间
}

// Ledger versions.json 账本
type Ledger struct {
	SchemaVersion int         `json:"schemaVersion"`
	Current       string      `json:"current,omitempty"` // 当前激活版本号
	UpdatedAt     time.Time   `json:"updatedAt"`
	Artifacts     []*Artifact `json:"artifacts"`
}

// OpState state.json：最近一次操作结果（重启后前端仍可展示）
type OpState struct {
	Action     string    `json:"action"`            // upgrade | rollback
	Version    string    `json:"version,omitempty"` // 目标版本
	Status     string    `json:"status"`            // running | success | failed
	Message    string    `json:"message,omitempty"`
	StartedAt  time.Time `json:"startedAt"`
	FinishedAt time.Time `json:"finishedAt,omitempty"`
}

// Manager 成品库与升级流水线管理器（进程内单例，串行执行写操作）
type Manager struct {
	mutex      sync.Mutex
	busy       bool
	appVersion string // 当前运行实例的版本号（由 api 层注入）
	baseDir    string // db/updater 目录
}

var (
	managerOnce    sync.Once
	defaultManager *Manager
)

// GetManager 获取管理器单例
func GetManager() *Manager {
	managerOnce.Do(func() {
		defaultManager = &Manager{
			baseDir: filepath.Join(config.GetDBPath(), "updater"),
		}
	})
	return defaultManager
}

// SetAppVersion 注入当前应用版本号（routers 注册时调用）
func (m *Manager) SetAppVersion(v string) {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	m.appVersion = v
}

// AppVersion 读取当前应用版本号
func (m *Manager) AppVersion() string {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.appVersion != "" {
		return m.appVersion
	}
	return "unknown"
}

// BaseDir 返回成品库根目录（测试时可覆盖）
func (m *Manager) BaseDir() string { return m.baseDir }

// SetBaseDir 覆盖成品库根目录（仅测试使用）
func (m *Manager) SetBaseDir(dir string) { m.baseDir = dir }

// ---- 落盘路径 ----

func (m *Manager) configPath() string  { return filepath.Join(m.baseDir, configFileName) }
func (m *Manager) ledgerPath() string  { return filepath.Join(m.baseDir, ledgerFileName) }
func (m *Manager) statePath() string   { return filepath.Join(m.baseDir, stateFileName) }
func (m *Manager) artifactDir() string { return filepath.Join(m.baseDir, artifactDirName) }
func (m *Manager) stagingDir() string  { return filepath.Join(m.baseDir, stagingDirName) }

// NewStagingFile 为上传文件在 staging 目录生成唯一落盘路径（保留原始扩展名）。
// 扩展名白名单外的输入一律按无扩展名处理，交给解包阶段报错。
func (m *Manager) NewStagingFile(originalName string) (string, error) {
	if err := m.ensureDirs(); err != nil {
		return "", err
	}
	ext := strings.ToLower(filepath.Ext(originalName))
	switch ext {
	case ".exe", ".zip", ".gz", ".tgz":
	case "":
	default:
		ext = "" // .tar.gz 等复合扩展名由调用方传入完整名时已在下方处理
	}
	lower := strings.ToLower(originalName)
	if strings.HasSuffix(lower, ".tar.gz") {
		ext = ".tar.gz"
	} else if strings.HasSuffix(lower, ".tar.bz2") {
		return "", fmt.Errorf("暂不支持 tar.bz2 归档")
	}
	stamp := time.Now().Format("20060102150405")
	return filepath.Join(m.stagingDir(), "upload-"+stamp+ext), nil
}

// ensureDirs 确保成品库目录结构存在
func (m *Manager) ensureDirs() error {
	for _, dir := range []string{m.baseDir, m.artifactDir(), m.stagingDir()} {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			return fmt.Errorf("创建目录 %s 失败: %w", dir, err)
		}
	}
	return nil
}

// ---- 配置读写 ----

// LoadConfig 读取升级源配置；文件不存在时返回默认配置
func (m *Manager) LoadConfig() (*UpdaterConfig, error) {
	data, err := os.ReadFile(m.configPath()) // #nosec G304 -- 路径由受管 baseDir 拼接
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			cfg := &UpdaterConfig{SourceType: SourceManifest, KeepArtifacts: 10}
			return cfg, nil
		}
		return nil, err
	}
	var cfg UpdaterConfig
	if err := json.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("解析升级配置失败: %w", err)
	}
	cfg.normalize()
	return &cfg, nil
}

// SaveConfig 校验并保存升级源配置
func (m *Manager) SaveConfig(cfg *UpdaterConfig) error {
	if err := cfg.Validate(); err != nil {
		return err
	}
	if err := m.ensureDirs(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(cfg, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(m.configPath(), data, 0o600)
}

// ---- 账本读写 ----

// LoadLedger 读取成品账本；文件不存在时返回空账本
func (m *Manager) LoadLedger() (*Ledger, error) {
	data, err := os.ReadFile(m.ledgerPath()) // #nosec G304 -- 路径由受管 baseDir 拼接
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return &Ledger{SchemaVersion: SchemaVersion, Artifacts: []*Artifact{}}, nil
		}
		return nil, err
	}
	var ledger Ledger
	if err := json.Unmarshal(data, &ledger); err != nil {
		return nil, fmt.Errorf("解析成品账本失败: %w", err)
	}
	if ledger.Artifacts == nil {
		ledger.Artifacts = []*Artifact{}
	}
	ledger.SchemaVersion = SchemaVersion
	return &ledger, nil
}

// SaveLedger 持久化账本并按保留上限淘汰最旧的非激活成品
func (m *Manager) SaveLedger(ledger *Ledger) error {
	if err := m.ensureDirs(); err != nil {
		return err
	}
	ledger.SchemaVersion = SchemaVersion
	ledger.UpdatedAt = time.Now()
	m.pruneLedger(ledger)
	data, err := json.MarshalIndent(ledger, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(m.ledgerPath(), data, 0o600)
}

// pruneLedger 按 KeepArtifacts 上限淘汰最旧的 backup/archived 成品（active 永不淘汰）
func (m *Manager) pruneLedger(ledger *Ledger) {
	cfgErr := error(nil)
	cfg, err := m.LoadConfig()
	if err != nil {
		cfgErr = err
	}
	keep := maxLedgerArtifacts
	if cfgErr == nil && cfg != nil && cfg.KeepArtifacts >= 2 {
		keep = cfg.KeepArtifacts
	}
	removable := len(ledger.Artifacts) - keep
	if removable <= 0 {
		return
	}
	// 最旧的排前面：优先淘汰 created_at 最早且非 active 的记录
	sort.SliceStable(ledger.Artifacts, func(i, j int) bool {
		return ledger.Artifacts[i].CreatedAt.Before(ledger.Artifacts[j].CreatedAt)
	})
	removed := 0
	kept := make([]*Artifact, 0, len(ledger.Artifacts))
	for _, a := range ledger.Artifacts {
		if removed < removable && a.Status != ArtifactStatusActive {
			_ = os.Remove(filepath.Join(m.artifactDir(), a.FileName)) // #nosec G304 -- 受管目录内拼接
			utils.Info("[updater] 成品超限淘汰: %s (%s)", a.ID, a.FileName)
			removed++
			continue
		}
		kept = append(kept, a)
	}
	ledger.Artifacts = kept
}

// FindArtifact 按 ID 查找成品
func (m *Manager) FindArtifact(ledger *Ledger, id string) *Artifact {
	for _, a := range ledger.Artifacts {
		if a.ID == id {
			return a
		}
	}
	return nil
}

// ListArtifacts 返回按入库时间倒序的成品列表
func (m *Manager) ListArtifacts() ([]*Artifact, error) {
	ledger, err := m.LoadLedger()
	if err != nil {
		return nil, err
	}
	list := append([]*Artifact(nil), ledger.Artifacts...)
	sort.SliceStable(list, func(i, j int) bool {
		return list[i].CreatedAt.After(list[j].CreatedAt)
	})
	return list, nil
}

// DeleteArtifact 删除成品文件与账本记录（active 成品禁止删除）
func (m *Manager) DeleteArtifact(id string) error {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	if m.busy {
		return ErrBusy
	}
	ledger, err := m.LoadLedger()
	if err != nil {
		return err
	}
	a := m.FindArtifact(ledger, id)
	if a == nil {
		return fmt.Errorf("成品 %s 不存在", id)
	}
	if a.Status == ArtifactStatusActive {
		return errors.New("不能删除当前正在运行的版本成品")
	}
	if err := os.Remove(filepath.Join(m.artifactDir(), a.FileName)); err != nil && !errors.Is(err, os.ErrNotExist) { // #nosec G304 -- 受管目录内拼接
		return fmt.Errorf("删除成品文件失败: %w", err)
	}
	for i, item := range ledger.Artifacts {
		if item.ID == id {
			ledger.Artifacts = append(ledger.Artifacts[:i], ledger.Artifacts[i+1:]...)
			break
		}
	}
	return m.SaveLedger(ledger)
}

// ---- 操作状态 ----

// LoadState 读取最近一次操作状态
func (m *Manager) LoadState() (*OpState, error) {
	data, err := os.ReadFile(m.statePath()) // #nosec G304 -- 路径由受管 baseDir 拼接
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return nil, nil
		}
		return nil, err
	}
	state := &OpState{}
	if err := json.Unmarshal(data, state); err != nil {
		return nil, fmt.Errorf("解析操作状态失败: %w", err)
	}
	return state, nil
}

// saveState 持久化操作状态
func (m *Manager) saveState(state *OpState) error {
	if err := m.ensureDirs(); err != nil {
		return err
	}
	data, err := json.MarshalIndent(state, "", "  ")
	if err != nil {
		return err
	}
	return atomicWrite(m.statePath(), data, 0o600)
}

// IsBusy 是否有升级/回退任务进行中
func (m *Manager) IsBusy() bool {
	m.mutex.Lock()
	defer m.mutex.Unlock()
	return m.busy
}

// acquire 占用执行槽；已有任务时返回 ErrBusy
func (m *Manager) acquire() error {
	if m.busy {
		return ErrBusy
	}
	m.busy = true
	return nil
}

// release 释放执行槽
func (m *Manager) release() {
	m.busy = false
}

// atomicWrite 先写临时文件再 rename，避免半截 JSON
func atomicWrite(path string, data []byte, perm os.FileMode) error {
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, data, perm); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
