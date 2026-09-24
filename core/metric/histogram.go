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
// 实现是一条两步链(新手导读,与 summary.go 的 Observe 同构):
//
//	hv.histogram.WithLabelValues(labels...).Observe(v)
//
//	第一步 WithLabelValues —— 纯本地动作,与 Prometheus
//	服务端无关:HistogramVec 是一族同构直方图,labels 每种
//	取值组合对应一条独立序列;这一步只是查进程内存里的
//	map 选出/惰性创建那个"孩子",没有任何网络 IO。
//
//	第二步 .Observe(v) —— 主动记账,同样是纯内存:
//	v 落进哪个预定义桶(le),该桶计数+1,同时 count+1、
//	sum+=v。此刻没有任何数据发给任何人 —— Observe 是
//	"记账",不是"上报"。
//
//	与 Prometheus 定期 pull 的关系(完整时间线):
//	  ① NewHistogramVec:创建并注册到本地 registry(内存);
//	  ② 业务运行:每次 Observe 只是往内存账本记一笔,
//	     两次抓取之间的观测全部累积在内存里;
//	  ③ Prometheus server 按抓取间隔(如 15s)GET /metrics,
//	     此刻才遍历 registry,把当下内存值渲染成文本带走
//	     (_bucket/_sum/_count);服务端用相邻两次 count 的
//	     差值算增量,PromQL 的 rate() 干的就是这件事;
//	  ④ 分位数在【查询时】才由服务端用 histogram_quantile()
//	     从桶算出 —— 这是"服务端参与"的唯一环节,也是
//	     Histogram 能跨实例聚合的原因;对比 Summary:分位数
//	     在客户端随 Observe 流式算好,拉走什么就是什么,
//	     不可跨实例聚合。
//
//	注意:
//	  1. labels 顺序必须与声明的 Labels 一致、数量相同,
//	     否则 panic;
//	  2. 外层 update() 是指标总闸:prometheus 未启用时
//	     (未 StartAgent / 进程退出清理后)整段跳过(见 metric.go)。
func (hv *promHistogramVec) Observe(v int64, labels ...string) {
	update(func() {
		hv.histogram.WithLabelValues(labels...).Observe(float64(v))
	})
}

// ObserveFloat 记录一次 float 观测(语义同 Observe,见其注释)。
func (hv *promHistogramVec) ObserveFloat(v float64, labels ...string) {
	update(func() {
		hv.histogram.WithLabelValues(labels...).Observe(v)
	})
}

// close 从全局注册表反注册。
func (hv *promHistogramVec) close() bool {
	return prom.Unregister(hv.histogram)
}
