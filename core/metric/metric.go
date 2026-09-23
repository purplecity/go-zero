// ————————————————————————————————————————————————————————————————————————————
// metric —— Prometheus 指标的统一封装 —— 文件总结
//
// 包内 5 个文件的关系:
//
//	metric.go    公共配置(VectorOpts)与总开关(update)
//	counter.go   Counter(只增计数器:请求数、错误数)
//	gauge.go     Gauge(可增可减的仪表盘:当前连接数、队列长度)
//	histogram.go Histogram(直方图:按桶统计分布,如延迟分布)
//	summary.go   Summary(摘要:按分位数统计,如 P99)
//
// 设计要点:
//  1. 每个指标都是 prometheus 官方 client 的薄封装,统一了
//     配置结构、注册流程和"未启用时零开销跳过"的开关;
//  2. update() 总闸:prometheus 未启用(prometheus.Enabled()==false)
//     时所有打点直接 return,连构造 label 参数的开销都没有;
//  3. 创建时自动注册到全局注册表,并通过 proc.AddShutdownListener
//     注册清理回调(进程退出时 Unregister,避免重复初始化时
//     "duplicate metrics collector" 报错)。
//
// ————————————————————————————————————————————————————————————————————————————
package metric

import "github.com/zeromicro/go-zero/core/prometheus"

// A VectorOpts is a general configuration.
// 通用指标配置,Counter/Gauge 共用(Histogram/Summary 因有额外
// 字段单独定义,但字段含义一致)。
// 生成的指标全名 = Namespace_Subsystem_Name(下划线拼接)。
type VectorOpts struct {
	// Namespace 命名空间,通常放公司/组织名。
	Namespace string
	// Subsystem 子系统,通常放服务名或模块名。
	Subsystem string
	// Name 指标名,如 request_total。
	Name string
	// Help 指标说明文字,展示在 Prometheus UI 上。
	Help string
	// Labels 维度标签名列表,打点时按顺序传入对应的值,
	// 每种标签值组合是独立的一条时间序列。
	Labels []string
}

// update 指标总闸:prometheus 未启用时直接跳过打点,
// 启用与否由 prometheus.StartAgent/Enable 决定(原子开关)。
func update(fn func()) {
	if !prometheus.Enabled() {
		return
	}

	fn()
}
