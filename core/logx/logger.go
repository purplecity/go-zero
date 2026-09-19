// ————————————————————————————————————————————————————————————————————————————
// logger —— Logger 接口定义 —— 文件总结
//
// Logger 是"面向对象"风格的日志器抽象,与 logs.go 的全局函数一一对应
// (方法名完全一致),区别是 Logger 可以携带状态:
//
//	WithContext  —— 携带 ctx(自动带 trace/span 及 ctx 注入字段);
//	WithCallerSkip —— 校正调用位置(包装层需要);
//	WithDuration —— 预置耗时字段;
//	WithFields   —— 预置业务字段。
//
// 这些 With* 都是链式派生:返回新 Logger,原 Logger 不受影响。
// 唯一实现是 richlogger.go 的 richLogger;
// logc 就是通过 WithContext + WithCallerSkip(1) 使用它的。
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"context"
	"time"
)

// A Logger represents a logger.
// Logger 是可携带上下文状态的日志器接口。
type Logger interface {
	// Debug logs a message at debug level.
	// 调试级别(基础版,fmt.Sprint 拼接)。
	Debug(...any)
	// Debugf logs a message at debug level.
	// 调试级别(格式化版)。
	Debugf(string, ...any)
	// Debugfn logs a message at debug level.
	// 调试级别(惰性求值版,级别关闭时 fn 不执行)。
	Debugfn(func() any)
	// Debugv logs a message at debug level.
	// 调试级别(对象版,JSON 编码)。
	Debugv(any)
	// Debugw logs a message at debug level.
	// 调试级别(结构化字段版)。
	Debugw(string, ...LogField)
	// Error logs a message at error level.
	// 错误级别(基础版)。
	Error(...any)
	// Errorf logs a message at error level.
	// 错误级别(格式化版)。
	Errorf(string, ...any)
	// Errorfn logs a message at error level.
	// 错误级别(惰性求值版)。
	Errorfn(func() any)
	// Errorv logs a message at error level.
	// 错误级别(对象版,不自动附调用栈)。
	Errorv(any)
	// Errorw logs a message at error level.
	// 错误级别(结构化字段版)。
	Errorw(string, ...LogField)
	// Info logs a message at info level.
	// 信息级别(基础版)。
	Info(...any)
	// Infof logs a message at info level.
	// 信息级别(格式化版)。
	Infof(string, ...any)
	// Infofn logs a message at info level.
	// 信息级别(惰性求值版)。
	Infofn(func() any)
	// Infov logs a message at info level.
	// 信息级别(对象版)。
	Infov(any)
	// Infow logs a message at info level.
	// 信息级别(结构化字段版)。
	Infow(string, ...LogField)
	// Slow logs a message at slow level.
	// 慢日志(基础版)。
	Slow(...any)
	// Slowf logs a message at slow level.
	// 慢日志(格式化版)。
	Slowf(string, ...any)
	// Slowfn logs a message at slow level.
	// 慢日志(惰性求值版)。
	Slowfn(func() any)
	// Slowv logs a message at slow level.
	// 慢日志(对象版)。
	Slowv(any)
	// Sloww logs a message at slow level.
	// 慢日志(结构化字段版)。
	Sloww(string, ...LogField)
	// WithCallerSkip returns a new logger with the given caller skip.
	// 派生:调用栈额外跳过 skip 层,包装库用它让 caller 指向用户代码。
	WithCallerSkip(skip int) Logger
	// WithContext returns a new logger with the given context.
	// 派生:携带 ctx,日志自动附 trace/span 及 ctx 注入字段。
	WithContext(ctx context.Context) Logger
	// WithDuration returns a new logger with the given duration.
	// 派生:预置 duration 耗时字段(人性化格式,见 timex.ReprOfDuration)。
	WithDuration(d time.Duration) Logger
	// WithFields returns a new logger with the given fields.
	// 派生:预置业务字段。
	WithFields(fields ...LogField) Logger
}
