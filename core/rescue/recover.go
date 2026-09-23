// ————————————————————————————————————————————————————————————————————————————
// rescue —— panic 兜底恢复 —— 文件总结
//
// 与 defer 搭配使用,捕获 goroutine 内的 panic,防止一个 panic
// 打崩整个进程,同时执行调用方传入的清理函数。go-zero 的
// threading.GoSafe、RoutineGroup 等安全启动 goroutine 的工具
// 内部都用它兜底。
//
// 典型用法:
//
//	go func() {
//	    defer rescue.Recover(func() { conn.Close() }) // 先清理
//	    doSomething()                                 // panic 在这里被吃掉
//	}()
//
// 两个要点:
//  1. cleanups 先于 recover 执行 —— 即使没有 panic 也会执行清理,
//     语义等同于 defer 注册的资源释放;
//  2. recover() 必须在 defer 的函数内直接调用才有效。
//
// ————————————————————————————————————————————————————————————————————————————
package rescue

import (
	"context"
	"runtime/debug"

	"github.com/zeromicro/go-zero/core/logx"
)

// Recover is used with defer to do cleanup on panics.
// Use it like:
//
//	defer Recover(func() {})
//
// panic 兜底:先依次执行清理函数,再捕获 panic 记入 error 日志
// (附调用栈,ErrorStack 自带限频),进程继续存活。
func Recover(cleanups ...func()) {
	// 清理函数先执行:无论是否发生 panic 都会跑(资源释放语义)。
	for _, cleanup := range cleanups {
		cleanup()
	}

	// recover() 只有在 defer 的函数里直接调用才能拦住 panic;
	// p 非 nil 说明确实发生了 panic。
	if p := recover(); p != nil {
		// 记 error 日志 + 调用栈(writeStack 内部有 lessWriter 限频)。
		logx.ErrorStack(p)
	}
}

// RecoverCtx is used with defer to do cleanup on panic.
// 带 ctx 版本:日志自动携带链路追踪信息(trace/span),
// panic 值用 %+v 打印,手动附上 debug.Stack() 调用栈。
// 其余行为与 Recover 完全一致。
func RecoverCtx(ctx context.Context, cleanups ...func()) {
	for _, cleanup := range cleanups {
		cleanup()
	}

	if p := recover(); p != nil {
		logx.WithContext(ctx).Errorf("%+v\n%s", p, debug.Stack())
	}
}
