// ————————————————————————————————————————————————————————————————————————————
// summary —— Summary 摘要(客户端分位数) —— 文件总结
//
// Summary 在客户端流式计算分位数(quantile),Objectives 指定
// 要哪些分位及其允许误差,如 {0.9: 0.01, 0.99: 0.001} 表示
// P90(误差 1%)与 P99(误差 0.1%)。
//
// 与 Histogram 的取舍:Summary 分位数精度高、无需预定义桶,
// 但各实例独立计算,Prometheus 端无法跨实例聚合求全局分位数;
// 需要全局分位数时选 Histogram。
// ————————————————————————————————————————————————————————————————————————————
package metric

import (
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/proc"
)

type (
	// A SummaryVecOpts is a summary vector options
	// 摘要配置:通用配置 + 分位数目标。
	SummaryVecOpts struct {
		// VecOpt 通用配置(名称/标签等)。
		VecOpt VectorOpts
		// Objectives 分位数目标:分位 → 允许误差,如 {0.99: 0.001}。
		Objectives map[float64]float64
	}

	// A SummaryVec interface represents a summary vector.
	// 摘要向量接口。
	SummaryVec interface {
		// Observe adds observation v to labels.
		// 记录一次观测。
		Observe(v float64, labels ...string)
		close() bool
	}

	// promSummaryVec 官方 client 的封装。
	promSummaryVec struct {
		summary *prom.SummaryVec
	}
)

// NewSummaryVec return a SummaryVec
// 创建摘要:构造 → 注册 → 注册退出清理回调。
func NewSummaryVec(cfg *SummaryVecOpts) SummaryVec {
	if cfg == nil {
		return nil
	}

	vec := prom.NewSummaryVec(
		prom.SummaryOpts{
			Namespace:  cfg.VecOpt.Namespace,
			Subsystem:  cfg.VecOpt.Subsystem,
			Name:       cfg.VecOpt.Name,
			Help:       cfg.VecOpt.Help,
			Objectives: cfg.Objectives,
		},
		cfg.VecOpt.Labels,
	)
	prom.MustRegister(vec)
	sv := &promSummaryVec{
		summary: vec,
	}
	proc.AddShutdownListener(func() {
		sv.close()
	})

	return sv
}

// Observe 记录一次观测。
func (sv *promSummaryVec) Observe(v float64, labels ...string) {
	update(func() {
		sv.summary.WithLabelValues(labels...).Observe(v)
	})
}

// close 从全局注册表反注册。
func (sv *promSummaryVec) close() bool {
	return prom.Unregister(sv.summary)
}
