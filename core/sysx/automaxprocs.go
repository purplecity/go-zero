// ————————————————————————————————————————————————————————————————————————————
// automaxprocs —— 容器环境下的 GOMAXPROCS 自动校正 —— 文件总结
//
// 问题背景:Go 默认 GOMAXPROCS = 机器 CPU 核数。但在容器(k8s)里,
// 机器可能是 64 核,而容器的 CPU 配额(cgroup quota)可能只有 2 核 ——
// Go 运行时感知不到配额,调度 64 个 P 抢 2 核,导致频繁线程切换、
// CPU 节流(throttling),延迟抖动明显。
//
// 本包在 init 时调用 uber 的 automaxprocs,读取 cgroup 配额,
// 把 GOMAXPROCS 校正为容器实际可用的 CPU 数。go-zero 服务引入
// 该包(import _ 触发 init)即可生效,无需任何配置。
// Logger(nil) 是为了关掉 automaxprocs 自己的日志输出,保持安静。
// ————————————————————————————————————————————————————————————————————————————
package sysx

import "go.uber.org/automaxprocs/maxprocs"

// Automatically set GOMAXPROCS to match Linux container CPU quota.
// 包初始化即自动校正 GOMAXPROCS;Logger(nil) 关闭其内部日志。
func init() {
	maxprocs.Set(maxprocs.Logger(nil))
}
