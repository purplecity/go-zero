// ————————————————————————————————————————————————————————————————————————————
// task —— 统计任务:一笔请求的画像 —— 文件总结
//
// Metrics 的最小记账单元(用户一般不直接构造,rest 的
// MetricHandler / sheddingstat 会上报):
//
//	Drop        该请求被丢弃(熔断/脱落拒绝,只计数不计时);
//	Duration    请求耗时(分位数统计的素材);
//	Description 描述(预留字段)。
//
// ————————————————————————————————————————————————————————————
package stat

import "time"

// A Task is a task reported to Metrics.
// 一笔请求的统计任务。
type Task struct {
	Drop        bool
	Duration    time.Duration
	Description string
}
