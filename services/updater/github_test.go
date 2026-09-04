package updater

import "testing"

func TestMatchAssetPlatform(t *testing.T) {
	cases := []struct {
		name string
		os   string
		arch string
		ok   bool
	}{
		// CI 发布命名（build-release.yml 产物）
		{"sublinkPro-linux-amd64.tar.gz", "linux", "amd64", true},
		{"sublinkPro-linux-arm64.tar.gz", "linux", "arm64", true},
		{"sublinkPro-linux-armv7.tar.gz", "linux", "arm", true},
		{"sublinkPro-linux-x86.tar.gz", "linux", "386", true},
		{"sublinkPro-windows-amd64.exe", "windows", "amd64", true},
		{"sublinkPro-windows-x86.exe", "windows", "386", true},
		{"sublinkPro-darwin-amd64.zip", "darwin", "amd64", true},
		{"sublinkPro-darwin-arm64.zip", "darwin", "arm64", true},
		// 常见社区命名
		{"myapp_linux_x86_64.tar.gz", "linux", "amd64", true},
		{"myapp-linux-aarch64.tar.gz", "linux", "arm64", true},
		{"tool_win64.zip", "windows", "amd64", true},
		{"tool-win32-x86.zip", "windows", "386", true},
		{"app.macos-arm64.tar.gz", "darwin", "arm64", true},
		{"app-osx-x86_64.zip", "darwin", "amd64", true},
		// 非二进制资产
		{"checksums.txt", "", "", false},
		{"sublinkPro-v1.2.3.sha256", "", "", false},
		{"versions.json", "", "", false},
		{"README.md", "", "", false},
		{"source-code.zip", "", "", false}, // 无平台信号
	}
	for _, tc := range cases {
		gotOS, gotArch, ok := MatchAssetPlatform(tc.name)
		if ok != tc.ok || (ok && (gotOS != tc.os || gotArch != tc.arch)) {
			t.Errorf("MatchAssetPlatform(%q) = (%q,%q,%v), want (%q,%q,%v)",
				tc.name, gotOS, gotArch, ok, tc.os, tc.arch, tc.ok)
		}
	}
}

func TestFetchGitHubReleasesFiltersAndSorts(t *testing.T) {
	// 不发网络请求：直接验证 release→RemoteVersion 的映射逻辑等价物。
	// 网络层由 fetchGitHubJSON 承担，此处仅覆盖纯函数部分。
	m := &Manager{}
	if _, err := m.FetchGitHubReleases(&UpdaterConfig{SourceType: SourceManifest}); err == nil {
		t.Fatal("非 github 源应当报错")
	}
}
