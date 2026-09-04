package updater

import (
	"os"
	"sync"
)

// shutdownTrigger 由 main.Run() 注入：触发主流程的优雅停机路径（等同收到 SIGTERM）。
// Windows 重启依赖它先释放端口；Unix 的 exec 路径不需要。
var (
	shutdownTriggerOnce sync.Once
	shutdownTrigger     func()
)

// SetShutdownTrigger 注册优雅停机触发器（main.go 启动时调用一次）
func SetShutdownTrigger(fn func()) {
	if fn == nil {
		return
	}
	shutdownTriggerOnce.Do(func() {
		shutdownTrigger = fn
	})
}

// executablePath 获取当前可执行文件真实路径
func executablePath() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	return resolveSymlinks(exe)
}

// currentArgs 当前进程启动参数（重启时原样透传）
func currentArgs() []string {
	if len(os.Args) == 0 {
		return []string{}
	}
	return os.Args
}
