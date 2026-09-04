//go:build !windows

package updater

import (
	"fmt"
	"os"
	"syscall"

	"sublink/utils"
)

// resolveSymlinks 解析符号链接，得到真实二进制路径
func resolveSymlinks(path string) (string, error) {
	if resolved, err := os.Readlink(path); err == nil {
		if !filepathIsAbs(resolved) {
			// 相对链接按原目录拼接
			dir := filepathDir(path)
			resolved = dir + "/" + resolved
		}
		return resolved, nil
	}
	return path, nil
}

func filepathIsAbs(p string) bool {
	return len(p) > 0 && p[0] == '/'
}

func filepathDir(p string) string {
	for i := len(p) - 1; i > 0; i-- {
		if p[i] == '/' {
			return p[:i]
		}
	}
	return "."
}

// RestartSelf 使用新二进制原地替换当前进程（Unix）：
// syscall.Exec 会以新程序映像重建进程，旧监听 socket 随 exec 自动关闭，
// 新进程启动后重新绑定端口。容器内（PID 1）同样适用。
// 注意：exec 不经过优雅停机路径，SQLite 无未决事务时安全。
func RestartSelf(newBinaryPath string) error {
	exe := newBinaryPath
	if exe == "" {
		var err error
		exe, err = executablePath()
		if err != nil {
			return fmt.Errorf("获取当前可执行文件路径失败: %w", err)
		}
	}
	args := currentArgs()
	utils.Info("[updater] exec 重启: %s %v", exe, args[1:])
	return syscall.Exec(exe, args, os.Environ()) // #nosec G204 -- 受管成品路径
}
