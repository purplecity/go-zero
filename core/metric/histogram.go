// ————————————————————————————————————————————————————————————————————————————
// histogram —— Histogram 直方图(桶分布统计) —— 文件总结
//
// Histogram 把观测值落入预定义的桶(bucket)计数,服务端可据此
// 计算 avg、分位数(histogram_quantile)。适合统计延迟、请求大小
// 等分布。桶边界在创建时固定(Buckets),如
// []float64{.5, 1, 5, 10, 100} 表示 ≤0.5ms、≤1ms… 各一桶。
//
// 与 Summary 的区别:Histogram 的分位数在服务端聚合计算
// (多个实例可合并),Summary 在客户端算好(实例间不可合并)。
// ————————————————————————————————————————————————————————————————————————————
package metric

import (
	prom "github.com/prometheus/client_golang/prometheus"
	"github.com/zeromicro/go-zero/core/proc"
)

type (
	// A HistogramVecOpts is a histogram vector options.
	// 直方图配置:比通用配置多了桶边界与常量标签。
	HistogramVecOpts struct {
		Namespace string
		Subsystem string
		Name      string
		Help      string
		Labels    []string
		// Buckets 桶边界(升序),nil 时用官方默认桶。
		Buckets []float64
		// ConstLabels 所有维度序列共享的固定标签(如实例 id)。
		ConstLabels map[string]string
	}

	// A HistogramVec interface represents a histogram vector.
	// 直方图向量接口。
	HistogramVec interface {
		// Observe adds observation v to labels.
		// 记录一次 int 观测(如毫秒延迟)。
		Observe(v int64, labels ...string)
		// ObserveFloat allow to observe float64 values.
		// 记录一次 float 观测。
		ObserveFloat(v float64, labels ...string)
		close() bool
	}

	// promHistogramVec 官方 client 的封装。
	promHistogramVec struct {
		histogram *prom.HistogramVec
	}
)

// NewHistogramVec returns a HistogramVec.
// 创建直方图:构造 → 注册 → 注册退出清理回调。
func NewHistogramVec(cfg *HistogramVecOpts) HistogramVec {
	if cfg == nil {
		return nil
	}

	vec := prom.NewHistogramVec(prom.HistogramOpts{
		Namespace:   cfg.Namespace,
		Subsystem:   cfg.Subsystem,
		Name:        cfg.Name,
		Help:        cfg.Help,
		Buckets:     cfg.Buckets,
		ConstLabels: cfg.ConstLabels,
	}, cfg.Labels)
	prom.MustRegister(vec)
	hv := &promHistogramVec{
		histogram: vec,
	}
	proc.AddShutdownListener(func() {
		hv.close()
	})

	return hv
}

// Observe 记录一次 int 观测(内部转 float)。
func (hv *promHistogramVec) Observe(v int64, labels ...string) {
	update(func() {
		hv.histogram.WithLabelValues(labels...).Observe(float64(v))
	})
}

// ObserveFloat 记录一次 float 观测。
func (hv *promHistogramVec) ObserveFloat(v float64, labels ...string) {
	update(func() {
		hv.histogram.WithLabelValues(labels...).Observe(v)
	})
}

// close 从全局注册表反注册。
func (hv *promHistogramVec) close() bool {
	return prom.Unregister(hv.histogram)
}
