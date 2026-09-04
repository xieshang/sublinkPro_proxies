package updater

import (
	"fmt"
	"runtime"
	"strconv"
	"strings"
)

// PlatformInfo 当前运行环境信息
type PlatformInfo struct {
	OS   string `json:"os"`
	Arch string `json:"arch"`
	Ext  string `json:"ext"` // Windows 为 .exe，其它为空
}

// DetectPlatform 识别当前运行环境（GOOS/GOARCH）
func DetectPlatform() PlatformInfo {
	ext := ""
	if runtime.GOOS == "windows" {
		ext = ".exe"
	}
	return PlatformInfo{OS: runtime.GOOS, Arch: runtime.GOARCH, Ext: ext}
}

// RenderTemplate 渲染单文件模板 URL，支持 {os} {arch} {ext} {version} 占位符
func RenderTemplate(tpl string, osName, arch, ext, version string) string {
	r := strings.NewReplacer(
		"{os}", osName,
		"{arch}", arch,
		"{ext}", ext,
		"{version}", version,
		"{OS}", strings.ToUpper(osName),
		"{ARCH}", strings.ToUpper(arch),
	)
	return r.Replace(tpl)
}

// ArtifactFileName 生成成品库内的标准文件名：v1.2.3-windows-amd64.exe
func ArtifactFileName(version, osName, arch, ext string) string {
	safe := sanitizeVersion(version)
	return fmt.Sprintf("%s-%s-%s%s", safe, osName, arch, ext)
}

// ArtifactID 生成成品唯一 ID：v1.2.3-windows-amd64
func ArtifactID(version, osName, arch string) string {
	return fmt.Sprintf("%s-%s-%s", sanitizeVersion(version), osName, arch)
}

// sanitizeVersion 清理版本号中不适合做文件名的字符
func sanitizeVersion(version string) string {
	v := strings.TrimSpace(version)
	if v == "" {
		v = "unknown"
	}
	var b strings.Builder
	for _, r := range v {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '.', r == '-', r == '_':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// CompareVersions 比较 semver 风格版本号（支持 v 前缀、预发布段、构建元数据）。
// 返回 -1/0/1：a<b / a==b / a>b。无法解析时按字符串比较。
func CompareVersions(a, b string) int {
	acore, apre := normalizeVersion(a)
	bcore, bpre := normalizeVersion(b)
	if acore == bcore {
		// 核心段相同：无预发布段者更大（1.0.0 > 1.0.0-rc.1）
		switch {
		case apre == bpre:
			return 0
		case apre == "":
			return 1
		case bpre == "":
			return -1
		default:
			if apre > bpre {
				return 1
			}
			return -1
		}
	}
	aparts := strings.Split(acore, ".")
	bparts := strings.Split(bcore, ".")
	n := len(aparts)
	if len(bparts) > n {
		n = len(bparts)
	}
	for i := 0; i < n; i++ {
		var sa, sb string
		if i < len(aparts) {
			sa = aparts[i]
		}
		if i < len(bparts) {
			sb = bparts[i]
		}
		ai, aerr := strconv.Atoi(sa)
		bi, berr := strconv.Atoi(sb)
		switch {
		case aerr == nil && berr == nil:
			if ai != bi {
				if ai < bi {
					return -1
				}
				return 1
			}
		default:
			// 混有非数字段：数字段视为更大，其余按字符串比较
			if sa != sb {
				if sa > sb {
					return 1
				}
				return -1
			}
		}
	}
	if acore > bcore {
		return 1
	}
	return -1
}

// normalizeVersion 拆分版本号为核心段与预发布段：
// 去 v/V 前缀、剥离 +build 元数据；"v1.0.0-rc.1+exp" → ("1.0.0", "rc.1")
func normalizeVersion(v string) (core, pre string) {
	v = strings.TrimSpace(strings.ToLower(v))
	if len(v) >= 2 && (v[0] == 'v' || v[0] == 'V') {
		v = v[1:]
	}
	if idx := strings.IndexByte(v, '+'); idx >= 0 {
		v = v[:idx]
	}
	if idx := strings.IndexByte(v, '-'); idx >= 0 {
		return v[:idx], v[idx+1:]
	}
	return v, ""
}

// IsNewer 判断 candidate 是否严格新于 current
func IsNewer(candidate, current string) bool {
	if current == "" || current == "unknown" || current == "dev" {
		return candidate != "" && candidate != "unknown" && candidate != "dev"
	}
	return CompareVersions(candidate, current) > 0
}

// verKey 返回用于宽松相等判断的版本键（核心段，忽略预发布/构建元数据）
func verKey(v string) string {
	core, _ := normalizeVersion(v)
	return core
}
