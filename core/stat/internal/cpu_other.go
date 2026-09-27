// ————————————————————————————————————————————————————————————————————————————
// cpu_other —— RefreshCpu 非 linux 空实现 —— 文件总结
//
// 非 linux(macOS/Windows…)没有 /proc 与 cgroup,恒返回 0
// —— usage.go 的 EMA 输入恒 0,CpuUsage() 恒 0。后果:
// adaptiveshedder 的 CPU 过载判断在这些平台永远不触发,
// 脱落只剩"飞行请求数"一路信号。
// ————————————————————————————————————————————————————————————————————————————
//go:build !linux

package internal

// RefreshCpu returns cpu usage, always returns 0 on systems other than linux.
// 非 linux 恒 0。
func RefreshCpu() uint64 {
	return 0
}
