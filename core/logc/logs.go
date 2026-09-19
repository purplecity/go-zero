// ————————————————————————————————————————————————————————————————————————————
// logc —— 带 context 上下文的日志门面(facade)—— 文件总结
//
// 一、logc 是什么?
//
//	logc 是 logx 的薄封装,唯一区别:所有写日志函数都要求第一个参数
//	传入 context.Context。logx 的全局函数(如 logx.Info)拿不到 ctx,
//	无法自动携带链路追踪信息;而 logc.Info(ctx, ...) 会:
//	  1. 通过 logx.WithContext(ctx) 生成携带 trace/span 信息的 logger;
//	  2. 再 WithCallerSkip(1) 校正调用位置 —— 因为中间多了一层
//	     logc → logx 的转发,不加 skip 的话日志里的 caller(调用位置)
//	     会指向 logc 内部而不是用户代码。
//
// 二、使用方式
//
//	业务代码推荐直接使用 logc,例如:
//	  logc.Errorw(ctx, "db query failed", logc.Field("sql", sql))
//	这样日志会自动带上 trace、span 以及 ctx 中注入的自定义字段
//	(见 logx.ContextWithFields),方便在日志系统中串联一次请求。
//
// 三、函数命名规范(与 logx 完全一致)
//
//	Info      基础版(fmt.Sprint 拼接)      Infof   格式化版
//	Infov     结构体/对象版(JSON 编码)     Infow   结构化字段版
//	Infofn    惰性求值版(仅当日志级别开启时才调用 fn,省去无谓计算)
//	另有 Debug*/Error*/Slow* 三大家族,以及不带 ctx 的全局操作
//	(AddGlobalFields/Must/MustSetup/SetLevel/SetUp/Close/Field)。
//
// ————————————————————————————————————————————————————————————————————————————
package logc

import (
	"context"
	"fmt"

	"github.com/zeromicro/go-zero/core/logx"
)

// LogConf、LogField 是 logx 对应类型的别名,方便只 import logc 的
// 业务代码完成配置与构造字段,不需要再引入 logx。
type (
	LogConf  = logx.LogConf
	LogField = logx.LogField
)

// AddGlobalFields adds global fields.
// 添加全局字段:之后所有日志(任何级别)都会自动附带这些 key-value,
// 适合放 app 名称、机房、版本号等所有日志都需要的公共信息。
func AddGlobalFields(fields ...LogField) {
	logx.AddGlobalFields(fields...)
}

// Alert alerts v in alert level, and the message is written to error log.
// 告警级别:写入 error 日志(level 字段为 alert),
// 通常对接告警系统,用于需要人工介入的严重事件。
func Alert(_ context.Context, v string) {
	logx.Alert(v)
}

// Close closes the logging.
// 关闭日志系统(刷盘并关闭底层文件),进程退出前建议调用。
func Close() error {
	return logx.Close()
}

// Debug writes v into access log.
// 调试级别,生产环境一般不开启。
func Debug(ctx context.Context, v ...interface{}) {
	getLogger(ctx).Debug(v...)
}

// Debugf writes v with format into access log.
// 调试级别,格式化版本。
func Debugf(ctx context.Context, format string, v ...interface{}) {
	getLogger(ctx).Debugf(format, v...)
}

// Debugfn writes fn result into access log.
// This is useful when the function is expensive to compute,
// and we want to log it only when necessary.
// 调试级别,惰性求值版:fn 只有在 debug 级别开启时才会被调用,
// 避免级别关闭时白白消耗 CPU 去构造日志内容。
func Debugfn(ctx context.Context, fn func() any) {
	getLogger(ctx).Debugfn(fn)
}

// Debugv writes v into access log with json content.
// 调试级别,对象版:整个对象会被 JSON 编码进 content 字段。
func Debugv(ctx context.Context, v interface{}) {
	getLogger(ctx).Debugv(v)
}

// Debugw writes msg along with fields into the access log.
// 调试级别,结构化字段版:msg + 任意多个 key-value 字段。
func Debugw(ctx context.Context, msg string, fields ...LogField) {
	getLogger(ctx).Debugw(msg, fields...)
}

// Error writes v into error log.
// 错误级别,写入 error 日志。
func Error(ctx context.Context, v ...any) {
	getLogger(ctx).Error(v...)
}

// Errorf writes v with format into error log.
// 错误级别,格式化版本。
func Errorf(ctx context.Context, format string, v ...any) {
	getLogger(ctx).Errorf(fmt.Errorf(format, v...).Error())
}

// Errorfn writes fn result into error log.
// This is useful when the function is expensive to compute,
// and we want to log it only when necessary.
// 错误级别,惰性求值版。
func Errorfn(ctx context.Context, fn func() any) {
	getLogger(ctx).Errorfn(fn)
}

// Errorv writes v into error log with json content.
// No call stack attached, because not elegant to pack the messages.
// 错误级别,对象版(JSON 编码)。注意:与 logx.Errorv 一致,
// 不自动附带调用栈(对结构化对象拼栈信息不优雅)。
func Errorv(ctx context.Context, v any) {
	getLogger(ctx).Errorv(v)
}

// Errorw writes msg along with fields into the error log.
// 错误级别,结构化字段版,推荐的业务错误记录方式。
func Errorw(ctx context.Context, msg string, fields ...LogField) {
	getLogger(ctx).Errorw(msg, fields...)
}

// Field returns a LogField for the given key and value.
// 构造一个日志字段,配合 Infow/Errorw 等 *w 系列使用:
// 例如 logc.Infow(ctx, "cache miss", logc.Field("key", k))。
func Field(key string, value any) LogField {
	return logx.Field(key, value)
}

// Info writes v into access log.
// 信息级别,日常业务日志。
func Info(ctx context.Context, v ...any) {
	getLogger(ctx).Info(v...)
}

// Infof writes v with format into access log.
// 信息级别,格式化版本。
func Infof(ctx context.Context, format string, v ...any) {
	getLogger(ctx).Infof(format, v...)
}

// Infofn writes fn result into access log.
// This is useful when the function is expensive to compute,
// and we want to log it only when necessary.
// 信息级别,惰性求值版。
func Infofn(ctx context.Context, fn func() any) {
	getLogger(ctx).Infofn(fn)
}

// Infov writes v into access log with json content.
// 信息级别,对象版(JSON 编码)。
func Infov(ctx context.Context, v any) {
	getLogger(ctx).Infov(v)
}

// Infow writes msg along with fields into the access log.
// 信息级别,结构化字段版,推荐的业务日志记录方式。
func Infow(ctx context.Context, msg string, fields ...LogField) {
	getLogger(ctx).Infow(msg, fields...)
}

// Must checks if err is nil, otherwise logs the error and exits.
// err 不为 nil 时记录错误并退出进程(或 panic,取决于 ExitOnFatal),
// 用于"出错就无法继续"的启动期校验。
func Must(err error) {
	logx.Must(err)
}

// MustSetup sets up logging with given config c. It exits on error.
// 用给定配置初始化日志,失败直接退出进程,
// 常用于服务启动阶段(ServiceConf 内部也会调用)。
func MustSetup(c logx.LogConf) {
	logx.MustSetup(c)
}

// SetLevel sets the logging level. It can be used to suppress some logs.
// 动态调整日志级别,可用于运行期临时屏蔽低级别日志。
func SetLevel(level uint32) {
	logx.SetLevel(level)
}

// SetUp sets up the logx.
// If already set up, return nil.
// We allow SetUp to be called multiple times, because, for example,
// we need to allow different service frameworks to initialize logx respectively.
// The same logic for SetUp
// 初始化日志配置。允许重复调用(多个服务框架各自初始化),
// 但真正生效的只有第一次(logx 内部用 sync.Once 保证),
// 后续调用直接返回 nil,不会覆盖之前的配置。
func SetUp(c LogConf) error {
	return logx.SetUp(c)
}

// Slow writes v into slow log.
// 慢日志级别,记录耗时过长的操作(如慢 SQL),写入 slow.log。
func Slow(ctx context.Context, v ...any) {
	getLogger(ctx).Slow(v...)
}

// Slowf writes v with format into slow log.
// 慢日志级别,格式化版本。
func Slowf(ctx context.Context, format string, v ...any) {
	getLogger(ctx).Slowf(format, v...)
}

// Slowfn writes fn result into slow log.
// This is useful when the function is expensive to compute,
// and we want to log it only when necessary.
// 慢日志级别,惰性求值版。
func Slowfn(ctx context.Context, fn func() any) {
	getLogger(ctx).Slowfn(fn)
}

// Slowv writes v into slow log with json content.
// 慢日志级别,对象版(JSON 编码)。
func Slowv(ctx context.Context, v any) {
	getLogger(ctx).Slowv(v)
}

// Sloww writes msg along with fields into slow log.
// 慢日志级别,结构化字段版,推荐的慢日志记录方式。
func Sloww(ctx context.Context, msg string, fields ...LogField) {
	getLogger(ctx).Sloww(msg, fields...)
}

// getLogger returns the logx.Logger with the given ctx and correct caller.
// 核心:把 ctx 转成带追踪信息的 logger,并跳过一层调用栈,
// 让日志里的 caller 字段指向真正的业务调用位置而不是 logc 自身。
func getLogger(ctx context.Context) logx.Logger {
	return logx.WithContext(ctx).WithCallerSkip(1)
}
