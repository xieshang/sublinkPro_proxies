// github.go 实现 GitHub Releases 升级源：
//   - 直接枚举仓库全部历史发布（Releases API），合并为可升级版本列表；
//   - 从资产文件名推断 goos/goarch，与运行平台匹配后作为下载候选。
package updater

import (
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"sort"
	"strings"
	"time"

	"sublink/utils"
)

const (
	githubAPIBase = "https://api.github.com"
	githubUA      = "SublinkPro-Updater"
	// githubMaxPages Releases 分页拉取页数上限（每页 100，足够覆盖成品库保留上限）
	githubMaxPages = 2
)

// ghAsset GitHub Release 资产（只取需要的字段）
type ghAsset struct {
	Name               string `json:"name"`
	BrowserDownloadURL string `json:"browser_download_url"`
	Size               int64  `json:"size"`
}

// ghRelease GitHub Release（只取需要的字段）
type ghRelease struct {
	TagName     string    `json:"tag_name"`
	Name        string    `json:"name"`
	Body        string    `json:"body"`
	Draft       bool      `json:"draft"`
	Prerelease  bool      `json:"prerelease"`
	PublishedAt time.Time `json:"published_at"`
	Assets      []ghAsset `json:"assets"`
}

// MatchAssetPlatform 从资产文件名推断 goos/goarch。
// 兼容常见命名：sublinkPro-linux-amd64.tar.gz / windows-x86.exe / darwin-arm64.zip、
// linux_x86_64、win64、macos-arm64、aarch64、armv7 等。
// 返回 ok=false 表示无法识别（校验和/文档等非二进制资产）。
func MatchAssetPlatform(name string) (string, string, bool) {
	lower := strings.ToLower(name)
	// .exe 后缀是 Windows 的强信号
	isExe := strings.HasSuffix(lower, ".exe")
	base := strings.TrimSuffix(lower, ".exe")
	// 剥掉压缩包扩展，避免 "gz"/"zip" 干扰分词
	for _, ext := range []string{".tar.gz", ".tar.bz2", ".tgz", ".zip", ".gz"} {
		base = strings.TrimSuffix(base, ext)
	}
	// 归一化会被分词拆散的写法
	base = strings.NewReplacer("x86_64", "amd64", "x86-64", "amd64").Replace(base)
	tokens := strings.FieldsFunc(base, func(r rune) bool {
		return !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9')
	})
	has := func(candidates ...string) bool {
		for _, t := range tokens {
			for _, c := range candidates {
				if t == c {
					return true
				}
			}
		}
		return false
	}
	// 跳过明显不是二进制的资产
	if has("sha256", "sha256sum", "checksum", "checksums", "changelog") ||
		strings.HasSuffix(lower, ".sha256") || strings.HasSuffix(lower, ".txt") ||
		strings.Contains(lower, "version") && strings.HasSuffix(lower, ".json") {
		return "", "", false
	}
	osName := ""
	winPrefix := false
	for _, t := range tokens {
		if strings.HasPrefix(t, "win") {
			winPrefix = true
			break
		}
	}
	switch {
	case isExe || has("windows", "win") || winPrefix:
		osName = "windows"
	case has("darwin", "macos", "osx", "mac", "apple"):
		osName = "darwin"
	case has("linux"):
		osName = "linux"
	default:
		return "", "", false
	}
	arch := ""
	switch {
	case has("amd64", "x86_64", "x64", "win64"):
		arch = "amd64"
	case has("arm64", "aarch64"):
		arch = "arm64"
	case has("armv7", "armv7l", "armv6", "armhf"):
		arch = "arm"
	case has("386", "i386", "i686", "x86", "win32"):
		arch = "386"
	default:
		return "", "", false
	}
	return osName, arch, true
}

// fetchGitHubJSON 请求 GitHub API 并解析 JSON；带可选令牌与代理
func (m *Manager) fetchGitHubJSON(cfg *UpdaterConfig, url string, out any) error {
	client, _, err := utils.CreateProxyHTTPClient(cfg.UseProxy, "", 30*time.Second)
	if err != nil {
		return fmt.Errorf("创建 HTTP 客户端失败: %w", err)
	}
	req, err := http.NewRequest(http.MethodGet, url, nil) // #nosec G107 -- URL 由配置的受管仓库拼接
	if err != nil {
		return fmt.Errorf("构造请求失败: %w", err)
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("X-GitHub-Api-Version", "2022-11-28")
	req.Header.Set("User-Agent", githubUA)
	if cfg.GitHubToken != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.GitHubToken)
	}
	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("请求失败: %w", err)
	}
	defer func() { _ = resp.Body.Close() }()
	data, err := io.ReadAll(io.LimitReader(resp.Body, 16<<20))
	if err != nil {
		return fmt.Errorf("读取响应失败: %w", err)
	}
	if resp.StatusCode != http.StatusOK {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		hint := ""
		if resp.StatusCode == http.StatusForbidden && cfg.GitHubToken == "" {
			hint = "（可能触发匿名速率限制，可配置 GitHub Token）"
		} else if resp.StatusCode == http.StatusNotFound {
			hint = "（检查仓库名 owner/repo 是否正确、仓库是否公开）"
		}
		return fmt.Errorf("GitHub API HTTP %d%s %s", resp.StatusCode, hint, msg)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("解析响应 JSON 失败: %w", err)
	}
	return nil
}

// FetchGitHubReleases 枚举仓库全部历史发布，合并为版本清单（按版本号降序）。
// 每个发布只保留能识别平台的二进制资产；草稿跳过，预发布保留（自然排序靠后）。
func (m *Manager) FetchGitHubReleases(cfg *UpdaterConfig) (*RemoteManifest, error) {
	if cfg.SourceType != SourceGitHub {
		return nil, fmt.Errorf("当前升级源不是 GitHub Releases 模式")
	}
	repo := strings.Trim(cfg.GitHubRepo, "/")
	all := make([]*ghRelease, 0, 64)
	for page := 1; page <= githubMaxPages; page++ {
		url := fmt.Sprintf("%s/repos/%s/releases?per_page=100&page=%d", githubAPIBase, repo, page)
		var batch []*ghRelease
		if err := m.fetchGitHubJSON(cfg, url, &batch); err != nil {
			if page > 1 && len(batch) == 0 {
				break // 翻页失败的容忍：保留已获取页
			}
			return nil, fmt.Errorf("拉取 Releases 失败: %w", err)
		}
		all = append(all, batch...)
		if len(batch) < 100 {
			break
		}
	}
	if len(all) == 0 {
		return nil, fmt.Errorf("仓库 %s 没有任何发布记录", cfg.GitHubRepo)
	}

	manifest := &RemoteManifest{Versions: make([]*RemoteVersion, 0, len(all))}
	for _, rel := range all {
		if rel == nil || rel.Draft || strings.TrimSpace(rel.TagName) == "" {
			continue
		}
		rv := &RemoteVersion{
			Version:     rel.TagName,
			Notes:       releaseNotes(rel),
			PublishedAt: rel.PublishedAt,
			Files:       make([]*RemoteFile, 0, len(rel.Assets)),
		}
		for _, a := range rel.Assets {
			if a.Size <= 0 || a.BrowserDownloadURL == "" {
				continue
			}
			goos, arch, ok := MatchAssetPlatform(a.Name)
			if !ok {
				continue
			}
			rv.Files = append(rv.Files, &RemoteFile{
				OS:   goos,
				Arch: arch,
				URL:  a.BrowserDownloadURL,
				Size: a.Size,
			})
		}
		if len(rv.Files) == 0 {
			continue // 无可识别二进制资产的发布不列出
		}
		manifest.Versions = append(manifest.Versions, rv)
	}
	if len(manifest.Versions) == 0 {
		return nil, fmt.Errorf("仓库 %s 的发布中没有可识别的二进制资产", cfg.GitHubRepo)
	}
	sort.SliceStable(manifest.Versions, func(i, j int) bool {
		return CompareVersions(manifest.Versions[i].Version, manifest.Versions[j].Version) > 0
	})
	manifest.Latest = manifest.Versions[0].Version
	return manifest, nil
}

// releaseNotes 组装发布说明：标题 + 正文（截断），标注预发布
func releaseNotes(rel *ghRelease) string {
	parts := make([]string, 0, 3)
	title := strings.TrimSpace(rel.Name)
	if title != "" && title != rel.TagName {
		parts = append(parts, title)
	}
	if rel.Prerelease {
		parts = append(parts, "[pre-release]")
	}
	body := strings.TrimSpace(rel.Body)
	const maxBody = 2000
	if len(body) > maxBody {
		body = body[:maxBody] + "\n…"
	}
	if body != "" {
		parts = append(parts, body)
	}
	return strings.Join(parts, "\n")
}
