// ————————————————————————————————————————————————————————————————————————————
// profile+polyfill —— 性能剖析的 Windows 占位实现 —— 文件总结
//
// pprof 的 CPU 剖析等依赖 Unix 信号(syscall.SIGINT 常量在 Windows
// 不可用),Windows 下 StartProfile 直接返回 noopStopper 空实现,
// 与 profile.go 按 build tag 互斥编译。
// ————————————————————————————————————————————————————————————————————————————
//go:build windows

package proc

// StartProfile Windows 下返回空实现,不做任何剖析。
func StartProfile() Stopper {
	return noopStopper
}
