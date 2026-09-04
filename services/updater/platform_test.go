package updater

import (
	"runtime"
	"testing"
)

func TestCompareVersions(t *testing.T) {
	cases := []struct {
		a, b string
		want int
	}{
		{"1.0.0", "1.0.0", 0},
		{"v1.2.3", "1.2.3", 0},
		{"V1.2.3", "1.2.3", 0},
		{"1.2.3", "1.2.4", -1},
		{"1.10.0", "1.9.9", 1},
		{"2.0", "1.9.9", 1},
		{"1.0.0+build5", "1.0.0", 0},
		{"1.0.0-rc.1", "1.0.0", -1}, // 预发布段按字符串比较小于空段
		{"dev", "dev", 0},
	}
	for _, c := range cases {
		got := CompareVersions(c.a, c.b)
		if got != c.want {
			t.Errorf("CompareVersions(%q, %q) = %d, want %d", c.a, c.b, got, c.want)
		}
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("v1.6.0", "1.5.9") {
		t.Error("v1.6.0 应新于 1.5.9")
	}
	if IsNewer("1.5.9", "v1.6.0") {
		t.Error("1.5.9 不应新于 v1.6.0")
	}
	if !IsNewer("anything", "") || !IsNewer("anything", "unknown") {
		t.Error("当前版本未知时任何非空目标都视为可升级")
	}
}

func TestRenderTemplate(t *testing.T) {
	got := RenderTemplate("https://example.com/dl/{version}/sublink_{os}_{arch}{ext}", "windows", "amd64", ".exe", "v1.6.0")
	want := "https://example.com/dl/v1.6.0/sublink_windows_amd64.exe"
	if got != want {
		t.Errorf("RenderTemplate = %q, want %q", got, want)
	}
	got = RenderTemplate("{os}/{arch}/{ext}", "linux", "arm64", "", "")
	if got != "linux/arm64/" {
		t.Errorf("RenderTemplate(linux) = %q", got)
	}
}

func TestArtifactIDAndFileName(t *testing.T) {
	if id := ArtifactID("v1.2.3", "windows", "amd64"); id != "v1.2.3-windows-amd64" {
		t.Errorf("ArtifactID = %q", id)
	}
	if name := ArtifactFileName("v1.2.3", "windows", "amd64", ".exe"); name != "v1.2.3-windows-amd64.exe" {
		t.Errorf("ArtifactFileName = %q", name)
	}
	// 版本号中的非法字符被替换
	if name := ArtifactFileName("v1.0/evil", "linux", "amd64", ""); name != "v1.0_evil-linux-amd64" {
		t.Errorf("sanitize 失败: %q", name)
	}
}

func TestDetectPlatform(t *testing.T) {
	p := DetectPlatform()
	if p.OS != runtime.GOOS || p.Arch != runtime.GOARCH {
		t.Fatalf("DetectPlatform = %+v, want %s/%s", p, runtime.GOOS, runtime.GOARCH)
	}
	wantExt := ""
	if runtime.GOOS == "windows" {
		wantExt = ".exe"
	}
	if p.Ext != wantExt {
		t.Errorf("Ext = %q, want %q", p.Ext, wantExt)
	}
}

func TestParseVersionOutput(t *testing.T) {
	cases := []struct {
		in   string
		want string
	}{
		{"SublinkPro v1.6.0\nbuild 2024", "v1.6.0"},
		{"1.2.3", "1.2.3"},
		{"sublinkpro version 2.10.0 (commit abc)", "2.10.0"},
		{"plain output without version", "plain output without version"},
	}
	for _, c := range cases {
		if got := parseVersionOutput(c.in); got != c.want {
			t.Errorf("parseVersionOutput(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}
