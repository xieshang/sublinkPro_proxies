package updater

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"

	"sublink/utils"
)

// testRunTimeout test 模式试运行超时：新二进制必须在该时间内正常退出
const testRunTimeout = 20 * time.Second

// TestResult test 模式运行结论
type TestResult struct {
	OK          bool   `json:"ok"`
	ObservedVer string `json:"observedVersion,omitempty"` // 从 --version 输出解析出的版本号
	Output      string `json:"output,omitempty"`          // 原始输出（截断后）
	Err         string `json:"error,omitempty"`
}

// ensureExecBit 赋予可执行权限（Windows 无需）
func ensureExecBit(path string) error {
	if runtime.GOOS == "windows" {
		return nil
	}
	info, err := os.Stat(path)
	if err != nil {
		return err
	}
	if info.Mode()&0o111 != 0 {
		return nil
	}
	return os.Chmod(path, info.Mode()|0o755) // #nosec G302 -- 可执行文件需要 owner 执行位
}

// TestBinary 对候选成品执行 test 模式：
// 运行 `<file> --version`，要求在超时内以退出码 0 结束且产生非空输出，
// 以此证明该文件与当前平台兼容、未损坏、可以正常启动。
func TestBinary(path string) *TestResult {
	if err := ensureExecBit(path); err != nil {
		return &TestResult{OK: false, Err: fmt.Sprintf("设置执行权限失败: %v", err)}
	}
	ctx, cancel := context.WithTimeout(context.Background(), testRunTimeout)
	defer cancel()

	cmd := exec.CommandContext(ctx, path, "--version") // #nosec G204 -- path 为成品库内受管文件
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	runErr := cmd.Run()

	out := strings.TrimSpace(stdout.String())
	errOut := strings.TrimSpace(stderr.String())
	combined := out
	if errOut != "" {
		if combined != "" {
			combined += "\n"
		}
		combined += errOut
	}
	const maxKeep = 2000
	if len(combined) > maxKeep {
		combined = combined[:maxKeep]
	}

	if ctx.Err() != nil && errors.Is(ctx.Err(), context.DeadlineExceeded) {
		return &TestResult{OK: false, Output: combined, Err: "试运行超时（20s 未退出）"}
	}
	if runErr != nil {
		return &TestResult{OK: false, Output: combined, Err: fmt.Sprintf("试运行失败: %v", runErr)}
	}
	if combined == "" {
		return &TestResult{OK: false, Err: "试运行退出码为 0 但没有任何输出"}
	}
	version := parseVersionOutput(combined)
	utils.Info("[updater] test 模式通过: %s (输出版本: %s)", filepathBase(path), version)
	return &TestResult{OK: true, ObservedVer: version, Output: combined}
}

// parseVersionOutput 从 --version 输出中提取版本号：
// 优先取首行中形如 v1.2.3 / 1.2.3 的 token；取不到则返回首行截断内容。
func parseVersionOutput(output string) string {
	firstLine := output
	if idx := strings.IndexAny(output, "\r\n"); idx >= 0 {
		firstLine = output[:idx]
	}
	firstLine = strings.TrimSpace(firstLine)
	for _, token := range strings.FieldsFunc(firstLine, func(r rune) bool {
		return r == ' ' || r == '\t' || r == ',' || r == '"' || r == '\''
	}) {
		t := strings.TrimPrefix(strings.TrimPrefix(strings.ToLower(token), "v"), "V")
		if t == "" || len(t) < 3 {
			continue
		}
		digits := 0
		for _, r := range strings.TrimPrefix(token, "v") {
			if r >= '0' && r <= '9' {
				digits++
			}
		}
		if digits >= 1 && strings.Count(t, ".") <= 4 {
			return token
		}
	}
	if firstLine == "" {
		return ""
	}
	if len(firstLine) > 64 {
		return firstLine[:64]
	}
	return firstLine
}

// filepathBase 避免直接引入 path/filepath 的轻量封装
func filepathBase(p string) string {
	idx := strings.LastIndexAny(p, "/\\")
	if idx >= 0 {
		return p[idx+1:]
	}
	return p
}
