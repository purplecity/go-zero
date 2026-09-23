// ————————————————————————————————————————————————————————————————————————————
// signals+polyfill —— 信号处理的 Windows 占位实现 —— 文件总结
//
// Windows 没有 SIGUSR1/SIGUSR2/SIGTERM(Unix 专属信号),
// 本文件用 build tag 与 signals.go 互斥编译:
//   Done() 返回 context.Background().Done() —— 一个永不关闭的
//   channel,调用方 select 它等于永远等不到,语义上等于
//   "Windows 下没有优雅退出通知"。
// ————————————————————————————————————————————————————————————————————————————
//go:build windows

package proc

import "context"

// Done returns the channel that notifies the process quitting.
// Windows 下返回永不关闭的 channel(context.Background().Done())。
func Done() <-chan struct{} {
	return context.Background().Done()
}
