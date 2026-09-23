// ————————————————————————————————————————————————————————————————————————————
// routines —— 安全 goroutine 启动 —— 文件总结
//
// 裸 go fn() 的隐患:fn 里一旦 panic,整个进程崩溃。
// 本文件提供"安全版"组合:
//
//	RunSafe / RunSafeCtx  当前 goroutine 里执行 fn,panic 被兜住记日志;
//	GoSafe / GoSafeCtx    再包一层 go,即"安全地开 goroutine"。
//
// 内部都是 rescue.Recover 兜底(记 error 日志 + 调用栈,带限频),
// 这是 go-zero 全库启动后台 goroutine 的标准姿势。
//
// RoutineId:从 runtime.Stack 的文本里解析 goroutine id
// (形如 "goroutine 123 [running]:..."),属非官方手段,仅供调试。
// ————————————————————————————————————————————————————————————————————————————
package threading

import (
	"bytes"
	"context"
	"runtime"
	"strconv"

	"github.com/zeromicro/go-zero/core/rescue"
)

// GoSafe runs the given fn using another goroutine, recovers if fn panics.
// 新开 goroutine 安全执行 fn:panic 不会打崩进程。
func GoSafe(fn func()) {
	go RunSafe(fn)
}

// GoSafeCtx runs the given fn using another goroutine, recovers if fn panics with ctx.
// 同 GoSafe,panic 日志携带 ctx 的链路追踪信息。
func GoSafeCtx(ctx context.Context, fn func()) {
	go RunSafeCtx(ctx, fn)
}

// RoutineId is only for debug, never use it in production.
// 获取当前 goroutine 的 id:解析 runtime.Stack 输出的头部文本
// ("goroutine 123 ..."),截出数字转 uint64。
// 解析失败返回 0。仅调试用 —— id 复用、无官方保证,别写业务逻辑。
func RoutineId() uint64 {
	b := make([]byte, 64)
	// 只取当前 goroutine 的栈(false 不取全部),通常 64 字节足够覆盖头部。
	b = b[:runtime.Stack(b, false)]
	// 去掉前缀 "goroutine ",再截到第一个空格,剩下的就是数字。
	b = bytes.TrimPrefix(b, []byte("goroutine "))
	b = b[:bytes.IndexByte(b, ' ')]
	// if error, just return 0
	n, _ := strconv.ParseUint(string(b), 10, 64)

	return n
}

// RunSafe runs the given fn, recovers if fn panics.
// 在当前 goroutine 安全执行 fn:panic 被 Recover 兜住记日志。
func RunSafe(fn func()) {
	defer rescue.Recover()

	fn()
}

// RunSafeCtx runs the given fn, recovers if fn panics with ctx.
// 同 RunSafe,panic 日志携带 ctx 的链路追踪信息。
func RunSafeCtx(ctx context.Context, fn func()) {
	defer rescue.RecoverCtx(ctx)

	fn()
}
