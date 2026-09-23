// ————————————————————————————————————————————————————————————————————————————
// profiler —— 业务耗时剖析器(空实现/真实现可切换) —— 文件总结
//
// 用法三步:
//
//	point := prof.Start()          // 打点开始
//	... 干活 ...
//	prof.Report("queueName", point) // 上报耗时
//
// 设计:全局 profiler 变量默认是 nullProfiler(空实现,零开销),
// 调 EnableProfiling() 后切换为 realProfiler(把耗时上报给
// profileCenter)—— 未开启剖析时业务代码完全无感、无开销,
// 这是空对象模式(Null Object Pattern)的又一应用。
// ProfilePoint 内嵌 utils.ElapsedTimer,构造即开始计时。
// ————————————————————————————————————————————————————————————————————————————
package prof

import "github.com/zeromicro/go-zero/core/utils"

type (
	// A ProfilePoint is a profile time point.
	// 计时点:内嵌秒表(构造即开始计时)。
	ProfilePoint struct {
		*utils.ElapsedTimer
	}

	// A Profiler interface represents a profiler that used to report profile points.
	// 剖析器接口:开始打点 + 上报耗时。
	Profiler interface {
		Start() ProfilePoint
		Report(name string, point ProfilePoint)
	}

	// realProfiler 真实现:上报给全局 profileCenter。
	realProfiler struct{}

	// nullProfiler 空实现:Start 返回空点,Report 什么都不做。
	nullProfiler struct{}
)

// profiler 全局剖析器,默认空实现(未开启剖析)。
var profiler = newNullProfiler()

// EnableProfiling enables profiling.
// 开启剖析:全局切换为真实现。应在进程启动早期调用。
func EnableProfiling() {
	profiler = newRealProfiler()
}

// Start starts a Profiler, and returns a start profiling point.
// 开始一段计时,返回计时点。
func Start() ProfilePoint {
	return profiler.Start()
}

// Report reports a ProfilePoint with given name.
// 以 name(如队列名/阶段名)上报计时点的耗时。
func Report(name string, point ProfilePoint) {
	profiler.Report(name, point)
}

// newRealProfiler 真实现构造器。
func newRealProfiler() Profiler {
	return &realProfiler{}
}

// Start 真实现:创建真实秒表。
func (rp *realProfiler) Start() ProfilePoint {
	return ProfilePoint{
		ElapsedTimer: utils.NewElapsedTimer(),
	}
}

// Report 真实现:把耗时上报给 profileCenter 聚合统计。
func (rp *realProfiler) Report(name string, point ProfilePoint) {
	duration := point.Duration()
	report(name, duration)
}

// newNullProfiler 空实现构造器。
func newNullProfiler() Profiler {
	return &nullProfiler{}
}

// Start 空实现:返回没有秒表的空计时点。
func (np *nullProfiler) Start() ProfilePoint {
	return ProfilePoint{}
}

// Report 空实现:什么都不做。
func (np *nullProfiler) Report(string, ProfilePoint) {
}
