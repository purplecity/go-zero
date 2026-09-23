// ————————————————————————————————————————————————————————————————————————————
// counter —— Counter 计数器(只增不减) —— 文件总结
//
// Counter 是 Prometheus 里"只增不减"的计数器,适合统计累计事件:
// 请求总数、错误总数、任务完成数等。进程重启会归零,
// Prometheus 端用 rate()/increase() 算速率。
//
// 用法:
//
//	cv := metric.NewCounterVec(&metric.CounterVecOpts{
//	    Name: "requests_total", Help: "...", Labels: []string{"method"},
//	})
//	cv.Inc("GET")               // 该维度 +1
//	cv.Add(10, "POST")          // 该维度 +10
//
// ————————————————————————————————————————————————————————————————————————————
package metric

import (
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/proc"
)

type (
	// A CounterVecOpts is an alias of VectorOpts.
	// 计数器配置,直接复用通用配置(计数器没有额外字段)。
	CounterVecOpts VectorOpts

	// CounterVec interface represents a counter vector.
	// 计数器向量接口:按 labels 维度打点。
	CounterVec interface {
		// Inc increments labels.
		// 指定维度 +1。
		Inc(labels ...string)
		// Add adds labels with v.
		// 指定维度 +v(负数会被 Prometheus 拒绝,计数器只增)。
		Add(v float64, labels ...string)
		close() bool
	}

	// promCounterVec 官方 client 的封装。
	promCounterVec struct {
		counter *prom.CounterVec
	}
)

// NewCounterVec returns a CounterVec.
// 创建计数器:构造 → 注册到全局注册表 → 注册进程退出时的
// 反注册回调(防止重启后重复注册报 duplicate 错误)。
func NewCounterVec(cfg *CounterVecOpts) CounterVec {
	if cfg == nil {
		return nil
	}

	vec := prom.NewCounterVec(prom.CounterOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      cfg.Name,
		Help:      cfg.Help,
	}, cfg.Labels)
	prom.MustRegister(vec)
	cv := &promCounterVec{
		counter: vec,
	}
	proc.AddShutdownListener(func() {
		cv.close()
	})

	return cv
}

// Add 指定维度累加 v;update 总闸未启用时直接跳过。
func (cv *promCounterVec) Add(v float64, labels ...string) {
	update(func() {
		cv.counter.WithLabelValues(labels...).Add(v)
	})
}

// Inc 指定维度 +1。
func (cv *promCounterVec) Inc(labels ...string) {
	update(func() {
		cv.counter.WithLabelValues(labels...).Inc()
	})
}

// close 从全局注册表反注册(进程退出清理用)。
func (cv *promCounterVec) close() bool {
	return prom.Unregister(cv.counter)
}
