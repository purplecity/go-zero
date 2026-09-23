// ————————————————————————————————————————————————————————————————————————————
// process —— 进程基本信息(启动时缓存) —— 文件总结
//
// init 时缓存进程名(命令行基础名,如 ./search → "search")与 pid,
// 之后 Pid()/ProcessName() 直接返回,供日志、监控、dump 文件命名等
// 场景使用。进程内这两个值不会变,缓存无一致性问题。
// ————————————————————————————————————————————————————————————————————————————
package proc

import (
	"os"
	"path/filepath"
)

var (
	// procName 进程名(os.Args[0] 的基础名)。
	procName string
	// pid 当前进程 id。
	pid int
)

func init() {
	procName = filepath.Base(os.Args[0])
	pid = os.Getpid()
}

// Pid returns pid of current process.
// 返回当前进程 id。
func Pid() int {
	return pid
}

// ProcessName returns the processname, same as the command name.
// 返回进程名(命令行基础名,如 "/usr/bin/search" → "search")。
func ProcessName() string {
	return procName
}
