// ————————————————————————————————————————————————————————————————————————————
// fields —— 日志字段的两种"全局"注入方式 —— 文件总结
//
//  1. 进程级全局字段(AddGlobalFields):一次添加,本进程之后产生的
//     所有日志都自动附带,存在 globalFields 里。并发安全设计:
//     读路径(每条日志的 mergeGlobalFields)用 atomic.Value 无锁读;
//     写路径用互斥锁 + 拷贝后整体替换(copy-on-write),读永不阻塞。
//
//  2. 请求级上下文字段(ContextWithFields):把字段塞进 ctx,
//     同一请求链路上的日志都会带上。典型用法是在中间件里
//     logx.ContextWithFields(r.Context(), logx.Field("uid", uid)),
//     后续 richLogger 输出时自动取出(见 richlogger.go 的 buildFields)。
//     重复调用是"追加"语义:新字段接在旧字段后面。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"context"
	"sync"
	"sync/atomic"
)

var (
	// globalFields 保存进程级全局字段。用 atomic.Value 而不是普通变量+锁,
	// 是因为每条日志输出都要读它,无锁读对高频日志路径零开销。
	globalFields atomic.Value
	// globalFieldsLock 只保护写入(AddGlobalFields),读写互斥、写写互斥。
	globalFieldsLock sync.Mutex
)

// fieldsKey 是 ctx 中存放字段的私有 key 类型(空结构体,零开销),
// 用自定义类型而非 string 作 key,避免与其他包的 ctx 值冲突。
type fieldsKey struct{}

// AddGlobalFields adds global fields.
// 添加进程级全局字段,之后所有日志自动附带。
// 加锁做"读取旧值 → 拷贝追加 → 整体替换"的原子更新,
// 保证并发添加不丢字段,同时读者要么看到旧集合要么看到新集合。
func AddGlobalFields(fields ...LogField) {
	globalFieldsLock.Lock()
	defer globalFieldsLock.Unlock()

	old := globalFields.Load()
	if old == nil {
		globalFields.Store(append([]LogField(nil), fields...))
	} else {
		globalFields.Store(append(old.([]LogField), fields...))
	}
}

// ContextWithFields returns a new context with the given fields.
// 把字段挂到 ctx 上(派生出新 ctx,原 ctx 不变)。
// 如果 ctx 上已有字段,做追加而不是覆盖 —— 跨多层中间件注入时
// 字段可以层层累积。
func ContextWithFields(ctx context.Context, fields ...LogField) context.Context {
	if val := ctx.Value(fieldsKey{}); val != nil {
		if arr, ok := val.([]LogField); ok {
			allFields := make([]LogField, 0, len(arr)+len(fields))
			allFields = append(allFields, arr...)
			allFields = append(allFields, fields...)
			return context.WithValue(ctx, fieldsKey{}, allFields)
		}
	}

	return context.WithValue(ctx, fieldsKey{}, fields)
}

// WithFields returns a new logger with the given fields.
// deprecated: use ContextWithFields instead.
// 已废弃,仅是 ContextWithFields 的别名,保留是为了兼容旧代码。
func WithFields(ctx context.Context, fields ...LogField) context.Context {
	return ContextWithFields(ctx, fields...)
}
