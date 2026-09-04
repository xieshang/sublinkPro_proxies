//go:build windows

package updater

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"golang.org/x/sys/windows"

	"sublink/utils"
)

// resolveSymlinks Windows 下一般无符号链接，直接返回
func resolveSymlinks(path string) (string, error) { return path, nil }

// RestartSelf 在 Windows 上完成自重启：
//  1. 把「延迟 + start 新二进制」写入临时批处理文件，派生分离的 cmd 执行它；
//  2. 触发主流程优雅停机（释放端口、落盘状态）；
//  3. 兜底：15 秒后若主流程仍未退出则强制退出，避免延迟启动器抢绑端口。
//
// 为什么用批处理文件而不是 cmd /c "<长命令>"：
// Go 按MSVCRT 规则转义参数内引号（\"），而 cmd /c 有自己的引号剥离规则，
// 复杂单行命令会被解析坏导致 start 根本不执行；批处理文件内容完全受控。
// 延迟用 ping 而非 timeout：timeout 在无控制台的分离进程中会立即报错退出。
func RestartSelf(newBinaryPath string) error {
	exe := newBinaryPath
	if exe == "" {
		var err error
		exe, err = executablePath()
		if err != nil {
			return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
		}
	}
	if !strings.HasSuffix(strings.ToLower(exe), ".exe") {
		return fmt.Errorf("Windows 平台要求可执行文件为 .exe: %s", exe)
	}

	// 组装批处理：约 3 秒后（ping×4≈3s）拉起新二进制并自删
	var sb strings.Builder
	sb.WriteString("@echo off\r\n")
	sb.WriteString("ping -n 4 -w 1000 127.0.0.1 >nul\r\n")
	sb.WriteString(fmt.Sprintf(`start "" "%s"`, exe))
	for _, a := range currentArgs()[1:] {
		if a == "" {
			continue
		}
		sb.WriteString(fmt.Sprintf(` "%s"`, a))
	}
	sb.WriteString("\r\n")
	sb.WriteString("del \"%~f0\"\r\n")

	batchPath := filepath.Join(os.TempDir(), fmt.Sprintf("sublinkpro-restart-%d.cmd", os.Getpid()))
	if err := os.WriteFile(batchPath, []byte(sb.String()), 0o600); err != nil {
		return fmt.Errorf("写入重启脚本失败: %w", err)
	}

	cmd := exec.Command("cmd", "/c", batchPath) // #nosec G204 -- 受管路径的受控脚本
	cmd.Stdout = nil
	cmd.Stderr = nil
	// 优先带 CREATE_BREAKAWAY_FROM_JOB：宿主（IDE/CI）用 Job Object 在主进程退出后
	// 清杀子进程时，脱离作业才能让延迟启动器活到拉起新版本；作业不允许脱离时创建
	// 会直接报错，此时降级为普通分离进程重试一次。
	breakaway := syscall.SysProcAttr{
		CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP | windows.CREATE_BREAKAWAY_FROM_JOB,
		HideWindow:    true,
	}
	cmd.SysProcAttr = &breakaway
	if err := cmd.Start(); err != nil {
		plain := syscall.SysProcAttr{
			CreationFlags: windows.DETACHED_PROCESS | windows.CREATE_NEW_PROCESS_GROUP,
			HideWindow:    true,
		}
		cmd.SysProcAttr = &plain
		if err2 := cmd.Start(); err2 != nil {
			_ = os.Remove(batchPath)
			return fmt.Errorf("启动延迟重启器失败: breakaway=%v, plain=%v", err, err2)
		}
		utils.Warn("[updater] 延迟重启器未能脱离父作业，若宿主在进程退出后清理子进程，需手动启动新版本")
	}
	// 启动器是分离进程，不等待它；立即走优雅停机
	go func() {
		utils.Info("[updater] Windows 延迟重启已安排（PID %d, script=%s），触发优雅停机", cmd.Process.Pid, batchPath)
		if shutdownTrigger != nil {
			shutdownTrigger()
		}
		time.Sleep(15 * time.Second)
		utils.Warn("[updater] 优雅停机超时，强制退出以让新版本接管")
		_ = os.Remove(batchPath)
		os.Exit(0)
	}()
	return nil
}
