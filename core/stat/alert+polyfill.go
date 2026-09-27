// ————————————————————————————————————————————————————————————————————————————
// alert+polyfill —— alert 在非 linux 平台的空实现 —— 文件总结
//
// 文件名里的 "+" 只是合法文件名字符,习惯读作"alert 的
// polyfill(垫片)":Report/SetReporter 变空操作,报警只在
// linux 上生效(alert.go 的 build tag 是 linux)。
// ————————————————————————————————————————————————————————————————————————————
//go:build !linux

package stat

// Report reports given message.
// 非 linux 空实现(不报警)。
func Report(string) {
}

// SetReporter sets the given reporter.
// 非 linux 空实现。
func SetReporter(func(string)) {
}
