// ————————————————————————————————————————————————————————————————————————————
// lesslogger —— 限频日志器 —— 文件总结
//
// 场景:下游故障时,每个请求都打一条 error 日志 → 错误风暴刷屏、
// 日志量爆炸。LessLogger 在给定时间窗口内只放行第一条日志,
// 其余丢弃并计数;下一条放行前会补一条 "Discarded N error messages",
// 既保护日志系统,又不丢失"发生过 N 次"的信息。
//
// 用法:全局创建一个(如 var errLog = logx.NewLessLogger(time.Minute)),
// 之后用 errLog.Error(...) 代替 logx.Error(...)。
// 限频核心逻辑见 limitedexecutor.go 的 logOrDiscard。
// ————————————————————————————————————————————————————————————————————————————
package logx

// A LessLogger is a logger that controls to log once during the given duration.
// LessLogger 限频日志器:给定毫秒窗口内只打一条,其余丢弃计数。
type LessLogger struct {
	// 复用 limitedExecutor 的限频能力(与 lessWriter 共用)。
	*limitedExecutor
}

// NewLessLogger returns a LessLogger.
// 创建窗口为 milliseconds 毫秒的限频日志器。
func NewLessLogger(milliseconds int) *LessLogger {
	return &LessLogger{
		limitedExecutor: newLimitedExecutor(milliseconds),
	}
}

// Error logs v into error log or discard it if more than once in the given duration.
// 窗口内的第一条 Error 照常输出,其余丢弃计数。
func (logger *LessLogger) Error(v ...any) {
	logger.logOrDiscard(func() {
		Error(v...)
	})
}

// Errorf logs v with format into error log or discard it if more than once in the given duration.
// 同 Error 的格式化版本。
func (logger *LessLogger) Errorf(format string, v ...any) {
	logger.logOrDiscard(func() {
		Errorf(format, v...)
	})
}
