// ————————————————————————————————————————————————————————————————————————————
// gauge —— Gauge 仪表盘(可增可减) —— 文件总结
//
// Gauge 与 Counter 的区别:值可以随意增减、也可以直接设置,
// 适合表达"当前状态量":当前连接数、队列长度、内存占用、
// 在线人数等。Prometheus 端直接取瞬时值,不需要算速率。
// ————————————————————————————————————————————————————————————————————————————
package metric

import (
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/proc"
)

type (
	// GaugeVecOpts is an alias of VectorOpts.
	// 仪表盘配置,复用通用配置。
	GaugeVecOpts VectorOpts

	// GaugeVec represents a gauge vector.
	// 仪表盘向量接口:比 Counter 多了 Set/Sub 等能力。
	GaugeVec interface {
		// Set sets v to labels.
		// 直接设置指定维度的值。
		Set(v float64, labels ...string)
		// Inc increments labels.
		// +1。
		Inc(labels ...string)
		// Dec decrements labels.
		// -1。
		Dec(labels ...string)
		// Add adds v to labels.
		// +v。
		Add(v float64, labels ...string)
		// Sub subtracts v to labels.
		// -v。
		Sub(v float64, labels ...string)
		close() bool
	}

	// promGaugeVec 官方 client 的封装。
	promGaugeVec struct {
		gauge *prom.GaugeVec
	}
)

// NewGaugeVec returns a GaugeVec.
// 创建仪表盘:构造 → 注册 → 注册退出清理回调(同 Counter)。
func NewGaugeVec(cfg *GaugeVecOpts) GaugeVec {
	if cfg == nil {
		return nil
	}

	vec := prom.NewGaugeVec(prom.GaugeOpts{
		Namespace: cfg.Namespace,
		Subsystem: cfg.Subsystem,
		Name:      cfg.Name,
		Help:      cfg.Help,
	}, cfg.Labels)
	prom.MustRegister(vec)
	gv := &promGaugeVec{
		gauge: vec,
	}
	proc.AddShutdownListener(func() {
		gv.close()
	})

	return gv
}

// Add 指定维度 +v。
func (gv *promGaugeVec) Add(v float64, labels ...string) {
	update(func() {
		gv.gauge.WithLabelValues(labels...).Add(v)
	})
}

// Dec 指定维度 -1。
func (gv *promGaugeVec) Dec(labels ...string) {
	update(func() {
		gv.gauge.WithLabelValues(labels...).Dec()
	})
}

// Inc 指定维度 +1。
func (gv *promGaugeVec) Inc(labels ...string) {
	update(func() {
		gv.gauge.WithLabelValues(labels...).Inc()
	})
}

// Set 直接设置指定维度的值(Gauge 特有能力)。
func (gv *promGaugeVec) Set(v float64, labels ...string) {
	update(func() {
		gv.gauge.WithLabelValues(labels...).Set(v)
	})
}

// Sub 指定维度 -v。
func (gv *promGaugeVec) Sub(v float64, labels ...string) {
	update(func() {
		gv.gauge.WithLabelValues(labels...).Sub(v)
	})
}

// close 从全局注册表反注册。
func (gv *promGaugeVec) close() bool {
	return prom.Unregister(gv.gauge)
}
