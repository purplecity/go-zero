// ————————————————————————————————————————————————————————————————————————————
// files+polyfill —— CloseOnExec 的 Windows 占位实现 —— 文件总结
//
// Windows 没有 fork+exec 语义,不存在"句柄被子进程继承"的问题,
// 提供空实现仅为让跨平台引用本包的代码能编译
// (与 signals+polyfill.go 同一模式,详见该文件说明)。
// ————————————————————————————————————————————————————————————————————————————
//go:build windows

package fs

import "os"

// CloseOnExec 空实现:Windows 无 exec 语义,无事可做。
func CloseOnExec(*os.File) {
}
