// ————————————————————————————————————————————————————————————————————————————
// metric —— Prometheus 指标的统一封装 —— 文件总结
//
// 一、包内 5 个文件的关系与设计要点:
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
// 二、普罗米修斯工作全景(新手导读)
//
//	Observe/Inc/Set 是"主动记账",不是"上报":
//	WithLabelValues 只是在进程内存里按标签组合选出/惰性创建
//	那条序列(纯本地,无网络);打点只是把 count/sum/桶计数/
//	分位数在内存里原子更新一笔 —— 此刻没有任何数据发给任何人。
//
//	完整时间线:
//	  ① 启动:New*Vec 创建指标并注册到本地 registry(内存);
//	  ② 运行:业务(通常在中间件里)每次调打点接口,
//	     两次抓取之间的观测全部累积在内存账本上;
//	  ③ Prometheus server 按抓取间隔(如 15s)GET /metrics,
//	     此刻才遍历 registry,把当下内存值渲染成文本带走;
//	  ④ server 存入时序库 TSDB,每条序列自动带 instance/job
//	     标签区分实例;
//	  ⑤ Grafana 查询的是 server 里的历史,"聚合"发生在
//	     查询时(PromQL,如 sum(rate(request_total[5m]))),
//	     不是 server 预先算好的。
//
//	进程重启:指标在内存,退出即丢、重启从 0 —— 但监控
//	基本不受影响:历史已在服务端;counter 回落由 rate()/
//	increase() 自动识别重置并把前后增量接起来;仅 Summary
//	的分位数估计需要重新积累样本"热身"。Shutdown 时的
//	Unregister 只是清理,不做持久化。
//
//	多实例(如 10 个副本):每个实例内存各存一份、各自被
//	抓取,TSDB 里按 instance 标签各存各的;要看合计还是
//	单个,由查询语句决定(与指标类型无关)。
//
//	Histogram 与 Summary 的选型:
//	  要跨实例聚合的全局分位数(如 10 个实例合计 P99)
//	    → 必须 Histogram:桶计数可相加,查询时
//	      histogram_quantile() 算出全局分位数;
//	  只看单实例的精确分位数 → Summary(不受桶边界影响),
//	    按 instance 过滤即可;注意 Summary 的分位数无法正确
//	    聚合(各实例 P99 求平均在数学上就是错的),能聚合的
//	    只有 _sum/_count。
//	  记法:默认 Histogram;"只关心单实例 + 要精确分位数"
//	  才用 Summary —— go-zero 内置监控(rest/handler 与
//	  zrpc 拦截器)全部用 Histogram,正因服务必然多实例。
//
//	典型接入点:HTTP/RPC 中间件计时打点,见
//	rest/handler/prometheushandler.go 与
//	zrpc/internal/serverinterceptors/prometheusinterceptor.go
//	(耗时 Histogram + 按 code 的请求总数 Counter);
//	业务自定义指标则在事件发生处直接调。
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
