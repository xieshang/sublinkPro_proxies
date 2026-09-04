package updater

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"sublink/utils"
)

// RemoteFile 清单中一个平台成品条目
type RemoteFile struct {
	OS     string `json:"os"`
	Arch   string `json:"arch"`
	URL    string `json:"url"`
	SHA256 string `json:"sha256,omitempty"` // 可选：下载后校验
	Size   int64  `json:"size,omitempty"`
}

// RemoteVersion 清单中一个版本记录
type RemoteVersion struct {
	Version     string        `json:"version"`
	Notes       string        `json:"notes,omitempty"`
	PublishedAt time.Time     `json:"publishedAt,omitempty"`
	Files       []*RemoteFile `json:"files"`
}

// RemoteManifest 远程 JSON 版本清单（编译成品分发库的描述格式）
type RemoteManifest struct {
	Latest   string           `json:"latest,omitempty"` // 可选；缺省取 versions 中最高版本
	Versions []*RemoteVersion `json:"versions"`
}

// FetchManifest 拉取并解析远程 JSON 清单
func (m *Manager) FetchManifest(cfg *UpdaterConfig) (*RemoteManifest, error) {
	if cfg.SourceType != SourceManifest || cfg.ManifestURL == "" {
		return nil, fmt.Errorf("当前升级源不是 JSON 清单模式")
	}
	data, err := utils.FetchWithProxy(cfg.ManifestURL, cfg.UseProxy, "", 30*time.Second, "SublinkPro-Updater")
	if err != nil {
		return nil, fmt.Errorf("拉取清单失败: %w", err)
	}
	return parseManifestBytes(data)
}

// parseManifestBytes 解析并校验清单 JSON：过滤脏记录，latest 缺省时取最高版本
func parseManifestBytes(data []byte) (*RemoteManifest, error) {
	var manifest RemoteManifest
	if err := json.Unmarshal(data, &manifest); err != nil {
		return nil, fmt.Errorf("解析清单 JSON 失败: %w", err)
	}
	if len(manifest.Versions) == 0 {
		return nil, fmt.Errorf("清单中没有 versions 记录")
	}
	// 校验每条记录的基础字段，过滤脏数据
	clean := make([]*RemoteVersion, 0, len(manifest.Versions))
	for _, v := range manifest.Versions {
		if v == nil || strings.TrimSpace(v.Version) == "" || len(v.Files) == 0 {
			continue
		}
		files := make([]*RemoteFile, 0, len(v.Files))
		for _, f := range v.Files {
			if f == nil || strings.TrimSpace(f.URL) == "" {
				continue
			}
			f.OS = strings.ToLower(strings.TrimSpace(f.OS))
			f.Arch = strings.ToLower(strings.TrimSpace(f.Arch))
			files = append(files, f)
		}
		if len(files) == 0 {
			continue
		}
		v.Version = strings.TrimSpace(v.Version)
		v.Files = files
		clean = append(clean, v)
	}
	if len(clean) == 0 {
		return nil, fmt.Errorf("清单中没有有效的版本记录（需包含 version 与至少一个带 url 的文件）")
	}
	manifest.Versions = clean

	// latest 缺省时取版本号最大的一条
	if strings.TrimSpace(manifest.Latest) == "" {
		best := clean[0]
		for _, v := range clean[1:] {
			if CompareVersions(v.Version, best.Version) > 0 {
				best = v
			}
		}
		manifest.Latest = best.Version
	}
	return &manifest, nil
}

// PickRemoteFile 从版本记录中挑选匹配当前平台的文件；未标注平台的文件视为通配兜底
func PickRemoteFile(v *RemoteVersion, platform PlatformInfo) (*RemoteFile, error) {
	var fallback *RemoteFile
	for _, f := range v.Files {
		osMatch := f.OS == "" || f.OS == platform.OS || f.OS == "any" || f.OS == "*"
		archMatch := f.Arch == "" || f.Arch == platform.Arch || f.Arch == "any" || f.Arch == "*"
		if osMatch && archMatch {
			if f.OS == platform.OS && f.Arch == platform.Arch {
				return f, nil // 精确匹配优先
			}
			if fallback == nil {
				fallback = f
			}
		}
	}
	if fallback != nil {
		return fallback, nil
	}
	return nil, fmt.Errorf("版本 %s 没有适配 %s/%s 的文件", v.Version, platform.OS, platform.Arch)
}

// FindRemoteVersion 按版本号查找清单中的版本记录
func FindRemoteVersion(manifest *RemoteManifest, version string) *RemoteVersion {
	target := verKey(version)
	for _, v := range manifest.Versions {
		if verKey(v.Version) == target {
			return v
		}
	}
	return nil
}
