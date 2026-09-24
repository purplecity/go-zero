// ————————————————————————————————————————————————————————————————————————————
// fs —— 文件系统小工具(Unix 真实现 + Windows 垫片) —— 文件总结
//
// CloseOnExec:给 fd 打上 FD_CLOEXEC 标记 —— 进程 fork+exec 时
// 自动关闭该 fd,防止句柄泄漏给子进程(如拉起的 shell 命令继承
// 了服务端监听 socket,会让端口无法释放、重启端口被占)。
// Windows 没有 exec 语义,由 files+polyfill.go 提供空实现。
// ————————————————————————————————————————————————————————————————————————————
//go:build linux || darwin || freebsd

package fs

import (
	"os"
	"syscall"
)

// CloseOnExec makes sure closing the file on process forking.
// fork+exec 时自动关闭该文件句柄(打 FD_CLOEXEC 标记)。
func CloseOnExec(file *os.File) {
	if file != nil {
		syscall.CloseOnExec(int(file.Fd()))
	}
}
