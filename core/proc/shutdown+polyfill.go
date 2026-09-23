// ————————————————————————————————————————————————————————————————————————————
// shutdown+polyfill —— 优雅退出的 Windows 占位实现 —— 文件总结
//
// Windows 无 SIGTERM/SIGUSR 系列信号,本文件与 shutdown.go 按
// build tag 互斥:全部注册函数原样返回、执行函数全部空操作,
// 让上层调用方无需关心平台差异。
// ————————————————————————————————————————————————————————————————————————————
//go:build windows

package proc

import "time"

// ShutdownConf is empty on windows.
// Windows 下无可配置项。
type ShutdownConf struct{}

// AddShutdownListener returns fn itself on windows, lets callers call fn on their own.
// Windows 下直接原样返回 fn,由调用方自行决定何时执行。
func AddShutdownListener(fn func()) func() {
	return fn
}

// AddWrapUpListener returns fn itself on windows, lets callers call fn on their own.
// 同上。
func AddWrapUpListener(fn func()) func() {
	return fn
}

// SetTimeToForceQuit does nothing on windows.
// 空实现。
func SetTimeToForceQuit(duration time.Duration) {
}

// Setup does nothing on windows.
// 空实现。
func Setup(conf ShutdownConf) {
}

// Shutdown does nothing on windows.
// 空实现。
func Shutdown() {
}

// WrapUp does nothing on windows.
// 空实现。
func WrapUp() {
}
