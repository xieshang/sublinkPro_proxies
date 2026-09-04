package updater

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// TestMain 让测试二进制自身支持 --version：TestBinary 的 test 模式
// 直接拿测试二进制当"候选成品"来验证试运行逻辑。
func TestMain(m *testing.M) {
	if len(os.Args) > 1 && (os.Args[1] == "--version" || os.Args[1] == "-v") {
		fmt.Println("updater-test 9.9.9")
		os.Exit(0)
	}
	os.Exit(m.Run())
}

func TestBinaryAcceptsSelf(t *testing.T) {
	exe, err := os.Executable()
	if err != nil {
		t.Skipf("无法定位测试二进制: %v", err)
	}
	res := TestBinary(exe)
	if !res.OK {
		t.Fatalf("test 模式应通过: %+v", res)
	}
	if res.ObservedVer == "" || !strings.Contains(res.ObservedVer, "9.9.9") {
		t.Errorf("实测版本异常: %q", res.ObservedVer)
	}
}

func TestBinaryRejectsMissing(t *testing.T) {
	res := TestBinary(filepath.Join(t.TempDir(), "no-such-binary"))
	if res.OK {
		t.Error("不存在的文件不应通过 test")
	}
}

func TestBinaryRejectsGarbageFile(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "fake.bin")
	if err := os.WriteFile(p, []byte("this is not an executable"), 0o755); err != nil {
		t.Fatal(err)
	}
	res := TestBinary(p)
	if res.OK {
		t.Error("垃圾文件不应通过 test")
	}
	if res.Err == "" {
		t.Error("失败时应给出原因")
	}
}

func TestUrlExtension(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"https://x/a.zip", ".zip"},
		{"https://x/a.tar.gz?token=1", ".tar.gz"},
		{"https://x/sublinkpro.exe", ""},
		{"https://x/a.tgz", ".tgz"},
		{"https://x/a", ""},
	}
	for _, c := range cases {
		if got := urlExtension(c.in); got != c.want {
			t.Errorf("urlExtension(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestHumanSizeAndShortHash(t *testing.T) {
	if got := humanSize(5 << 20); got != "5.0 MB" {
		t.Errorf("humanSize = %q", got)
	}
	if got := shortHash("abcdef1234567890"); got != "abcdef123456…" {
		t.Errorf("shortHash = %q", got)
	}
}

func TestSwapBinaryRoundTrip(t *testing.T) {
	// 用临时目录里的假 exe 验证替换逻辑本身（不触碰真实运行文件）：
	// swapBinary 依赖 executablePath()，这里仅验证 copyFile + rename 组合的语义。
	m := newTestManager(t)
	if err := m.ensureDirs(); err != nil {
		t.Fatal(err)
	}
	src := filepath.Join(m.stagingDir(), "cand.bin")
	dst := filepath.Join(m.stagingDir(), "target.bin")
	if err := os.WriteFile(src, []byte("new-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(dst, []byte("old-bytes"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := copyFile(src, dst, 0o755); err != nil {
		t.Fatalf("copyFile: %v", err)
	}
	data, _ := os.ReadFile(dst)
	if string(data) != "new-bytes" {
		t.Errorf("复制后内容不符: %q", data)
	}
}
