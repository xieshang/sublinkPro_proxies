package updater

import (
	"testing"
)

const sampleManifest = `{
  "latest": "v1.6.0",
  "versions": [
    {
      "version": "v1.5.0",
      "notes": "old",
      "files": [
        {"os": "windows", "arch": "amd64", "url": "https://x/v1.5.0-win.zip", "sha256": "aaa"},
        {"os": "linux", "arch": "amd64", "url": "https://x/v1.5.0-linux.tar.gz"},
        {"os": "linux", "arch": "arm64", "url": "https://x/v1.5.0-arm.tar.gz"}
      ]
    },
    {
      "version": "v1.6.0",
      "files": [
        {"os": "windows", "arch": "amd64", "url": "https://x/v1.6.0-win-amd64.exe"},
        {"os": "windows", "arch": "arm64", "url": "https://x/v1.6.0-win-arm.exe"},
        {"os": "linux", "arch": "amd64", "url": "https://x/v1.6.0-linux-amd64"},
        {"os": "linux", "arch": "arm64", "url": "https://x/v1.6.0-linux-arm64"}
      ]
    },
    {"version": "", "files": [{"url": "https://x/bad"}]},
    {"version": "v0.9", "files": [{"url": ""}]}
  ]
}`

func TestParseManifestBytes(t *testing.T) {
	m, err := parseManifestBytes([]byte(sampleManifest))
	if err != nil {
		t.Fatalf("parseManifestBytes: %v", err)
	}
	// 脏记录被过滤
	if len(m.Versions) != 2 {
		t.Fatalf("期望过滤后剩 2 个版本, got %d", len(m.Versions))
	}
	if m.Latest != "v1.6.0" {
		t.Errorf("Latest = %q, want v1.6.0", m.Latest)
	}
}

func TestParseManifestLatestFallback(t *testing.T) {
	noLatest := `{"versions":[{"version":"1.0.0","files":[{"url":"u","os":"linux"}]},{"version":"1.2.0","files":[{"url":"u","os":"linux"}]}]}`
	m, err := parseManifestBytes([]byte(noLatest))
	if err != nil {
		t.Fatalf("parse: %v", err)
	}
	if m.Latest != "1.2.0" {
		t.Errorf("缺省 latest 应取最高版本, got %q", m.Latest)
	}
}

func TestParseManifestEmpty(t *testing.T) {
	if _, err := parseManifestBytes([]byte(`{}`)); err == nil {
		t.Error("空清单应报错")
	}
	if _, err := parseManifestBytes([]byte(`not json`)); err == nil {
		t.Error("非法 JSON 应报错")
	}
}

func TestPickRemoteFile(t *testing.T) {
	m, _ := parseManifestBytes([]byte(sampleManifest))
	v16 := FindRemoteVersion(m, "1.6.0")
	if v16 == nil {
		t.Fatal("未找到 v1.6.0")
	}
	cases := []struct {
		os, arch string
		want     string
	}{
		{"windows", "amd64", "https://x/v1.6.0-win-amd64.exe"},
		{"linux", "arm64", "https://x/v1.6.0-linux-arm64"},
	}
	for _, c := range cases {
		f, err := PickRemoteFile(v16, PlatformInfo{OS: c.os, Arch: c.arch})
		if err != nil {
			t.Fatalf("%s/%s: %v", c.os, c.arch, err)
		}
		if f.URL != c.want {
			t.Errorf("%s/%s → %q, want %q", c.os, c.arch, f.URL, c.want)
		}
	}
	// 无匹配平台应报错
	if _, err := PickRemoteFile(v16, PlatformInfo{OS: "darwin", Arch: "ppc64"}); err == nil {
		t.Error("darwin/ppc64 不应匹配任何文件")
	}
}

func TestFindRemoteVersionNormalize(t *testing.T) {
	m, _ := parseManifestBytes([]byte(sampleManifest))
	if FindRemoteVersion(m, "V1.5.0") == nil {
		t.Error("带 V 前缀查找应命中 v1.5.0")
	}
	if FindRemoteVersion(m, "9.9.9") != nil {
		t.Error("不存在的版本不应命中")
	}
}
