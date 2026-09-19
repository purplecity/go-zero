// ————————————————————————————————————————————————————————————————————————————
// richlogger —— Logger 接口的唯一实现:带状态的"增强日志器" —— 文件总结
//
// richLogger 携带三种状态,输出时全部自动合入日志字段:
//
//	ctx        —— 提取 trace/span id(链路追踪),以及经
//	              ContextWithFields 注入的请求级字段;
//	callerSkip —— 调用栈额外跳过层数(包装库校正 caller 用);
//	fields     —— 预置字段(WithDuration/WithFields 累积而来)。
//
// buildFields 的字段组装顺序(后加的覆盖先加的,map 语义):
//
//	预置 fields → caller → 进程级全局字段 → trace/span → ctx 请求字段
//
// 入口函数:WithCallerSkip / WithContext / WithDuration(全局函数),
// logc 包就是通过 WithContext(ctx).WithCallerSkip(1) 使用本实现。
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"context"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/timex"
	"github.com/zeromicro/go-zero/internal/trace"
)

// WithCallerSkip returns a Logger with given caller skip.
// 创建带调用栈偏移的日志器:skip <= 0 时等同默认日志器。
// 包装层(如 logc)传 1,让 caller 指向业务代码而非包装层。
func WithCallerSkip(skip int) Logger {
	if skip <= 0 {
		return new(richLogger)
	}

	return &richLogger{
		callerSkip: skip,
	}
}

// WithContext sets ctx to log, for keeping tracing information.
// 创建携带 ctx 的日志器:日志自动附 trace/span,可链路串联。
func WithContext(ctx context.Context) Logger {
	return &richLogger{
		ctx: ctx,
	}
}

// WithDuration returns a Logger with given duration.
// 创建预置耗时字段的日志器:duration 转成人性化字符串
// (如 "1.5s"),常用于记录 rpc/处理耗时。
func WithDuration(d time.Duration) Logger {
	return &richLogger{
		fields: []LogField{Field(durationKey, timex.ReprOfDuration(d))},
	}
}

// richLogger 是 Logger 接口的实现,三字段含义见文件头。
type richLogger struct {
	ctx        context.Context
	callerSkip int
	fields     []LogField
}

// —— 以下 Debug/Error/Info/Slow 各家族的实现模式完全一致:
//    先 shallLog 过滤(级别关闭时连 fmt.Sprint 都不执行),
//    再交给私有 debug/err/info/slow 组装输出 ——

func (l *richLogger) Debug(v ...any) {
	if shallLog(DebugLevel) {
		l.debug(fmt.Sprint(v...))
	}
}

func (l *richLogger) Debugf(format string, v ...any) {
	if shallLog(DebugLevel) {
		l.debug(fmt.Sprintf(format, v...))
	}
}

func (l *richLogger) Debugfn(fn func() any) {
	if shallLog(DebugLevel) {
		l.debug(fn())
	}
}

func (l *richLogger) Debugv(v any) {
	if shallLog(DebugLevel) {
		l.debug(v)
	}
}

func (l *richLogger) Debugw(msg string, fields ...LogField) {
	if shallLog(DebugLevel) {
		l.debug(msg, fields...)
	}
}

func (l *richLogger) Error(v ...any) {
	if shallLog(ErrorLevel) {
		l.err(fmt.Sprint(v...))
	}
}

func (l *richLogger) Errorf(format string, v ...any) {
	if shallLog(ErrorLevel) {
		l.err(fmt.Sprintf(format, v...))
	}
}

func (l *richLogger) Errorfn(fn func() any) {
	if shallLog(ErrorLevel) {
		l.err(fn())
	}
}

func (l *richLogger) Errorv(v any) {
	if shallLog(ErrorLevel) {
		l.err(v)
	}
}

func (l *richLogger) Errorw(msg string, fields ...LogField) {
	if shallLog(ErrorLevel) {
		l.err(msg, fields...)
	}
}

func (l *richLogger) Info(v ...any) {
	if shallLog(InfoLevel) {
		l.info(fmt.Sprint(v...))
	}
}

func (l *richLogger) Infof(format string, v ...any) {
	if shallLog(InfoLevel) {
		l.info(fmt.Sprintf(format, v...))
	}
}

func (l *richLogger) Infofn(fn func() any) {
	if shallLog(InfoLevel) {
		l.info(fn())
	}
}

func (l *richLogger) Infov(v any) {
	if shallLog(InfoLevel) {
		l.info(v)
	}
}

func (l *richLogger) Infow(msg string, fields ...LogField) {
	if shallLog(InfoLevel) {
		l.info(msg, fields...)
	}
}

func (l *richLogger) Slow(v ...any) {
	if shallLog(ErrorLevel) {
		l.slow(fmt.Sprint(v...))
	}
}

func (l *richLogger) Slowf(format string, v ...any) {
	if shallLog(ErrorLevel) {
		l.slow(fmt.Sprintf(format, v...))
	}
}

func (l *richLogger) Slowfn(fn func() any) {
	if shallLog(ErrorLevel) {
		l.slow(fn())
	}
}

func (l *richLogger) Slowv(v any) {
	if shallLog(ErrorLevel) {
		l.slow(v)
	}
}

func (l *richLogger) Sloww(msg string, fields ...LogField) {
	if shallLog(ErrorLevel) {
		l.slow(msg, fields...)
	}
}

// —— With* 链式派生:全部返回新 richLogger,不改原对象(不可变语义)——

// WithCallerSkip 派生带调用栈偏移的新日志器;skip <= 0 时原样返回。
func (l *richLogger) WithCallerSkip(skip int) Logger {
	if skip <= 0 {
		return l
	}

	return &richLogger{
		ctx:        l.ctx,
		callerSkip: skip,
		fields:     l.fields,
	}
}

// WithContext 派生携带新 ctx 的日志器。
func (l *richLogger) WithContext(ctx context.Context) Logger {
	return &richLogger{
		ctx:        ctx,
		callerSkip: l.callerSkip,
		fields:     l.fields,
	}
}

// WithDuration 派生追加了 duration 字段的日志器。
func (l *richLogger) WithDuration(duration time.Duration) Logger {
	fields := append(l.fields, Field(durationKey, timex.ReprOfDuration(duration)))

	return &richLogger{
		ctx:        l.ctx,
		callerSkip: l.callerSkip,
		fields:     fields,
	}
}

// WithFields 派生追加了业务字段的日志器;空字段时原样返回。
func (l *richLogger) WithFields(fields ...LogField) Logger {
	if len(fields) == 0 {
		return l
	}

	f := append(l.fields, fields...)

	return &richLogger{
		ctx:        l.ctx,
		callerSkip: l.callerSkip,
		fields:     f,
	}
}

// buildFields 汇总一条日志的全部字段(见文件头的组装顺序):
//  1. 调用位置 caller(深度加上 callerSkip);
//  2. 进程级全局字段;
//  3. ctx 里的 trace/span id;
//  4. ctx 里注入的请求级字段(ContextWithFields)。
func (l *richLogger) buildFields(fields ...LogField) []LogField {
	fields = append(l.fields, fields...)
	// caller field should always appear together with global fields
	fields = append(fields, Field(callerKey, getCaller(callerDepth+l.callerSkip)))
	fields = mergeGlobalFields(fields)

	if l.ctx == nil {
		return fields
	}

	// 从 ctx 提取链路追踪 id,有值才加字段。
	traceID := trace.TraceIDFromContext(l.ctx)
	if len(traceID) > 0 {
		fields = append(fields, Field(traceKey, traceID))
	}

	spanID := trace.SpanIDFromContext(l.ctx)
	if len(spanID) > 0 {
		fields = append(fields, Field(spanKey, spanID))
	}

	// 取出 ContextWithFields 注入的请求级字段。
	val := l.ctx.Value(fieldsKey{})
	if val != nil {
		if arr, ok := val.([]LogField); ok {
			fields = append(fields, arr...)
		}
	}

	return fields
}

// debug 私有输出:组装字段后交给 writer 的 debug 流。
// 这里的 shallLog 是二次防御(public 方法已检查过),开销可忽略。
func (l *richLogger) debug(v any, fields ...LogField) {
	if shallLog(DebugLevel) {
		getWriter().Debug(v, l.buildFields(fields...)...)
	}
}

// err 私有输出:error 流。
func (l *richLogger) err(v any, fields ...LogField) {
	if shallLog(ErrorLevel) {
		getWriter().Error(v, l.buildFields(fields...)...)
	}
}

// info 私有输出:access 流。
func (l *richLogger) info(v any, fields ...LogField) {
	if shallLog(InfoLevel) {
		getWriter().Info(v, l.buildFields(fields...)...)
	}
}

// slow 私有输出:slow 流。
func (l *richLogger) slow(v any, fields ...LogField) {
	if shallLog(ErrorLevel) {
		getWriter().Slow(v, l.buildFields(fields...)...)
	}
}
