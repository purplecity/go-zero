// ————————————————————————————————————————————————————————————————————————————
// timeout —— 带超时执行任意函数 —— 文件总结
//
// DoWithTimeout 把"不可中断的普通函数"放进 goroutine,
// 主流程 select 三选一:正常返回 / 超时(或外部 ctx 取消)/
// panic 转发。两个防泄漏细节:
//  1. done/panicChan 容量 1 —— 超时返回后迟到的写入不阻塞、
//     goroutine 不泄漏;
//  2. panic 不在 worker 里裸抛,而是带栈转发给调用方
//     (跨 goroutine 栈会丢,先拼好再抛)。
//
// 注意:超时只是"不再等",函数本身仍在跑(Go 无法强杀 goroutine)。
// ————————————————————————————————————————————————————————————————————————————
package fx

import (
	"context"
	"fmt"
	"runtime/debug"
	"strings"
	"time"
)

var (
	// ErrCanceled is the error returned when the context is canceled.
	// 外部 ctx 取消时返回(即 context.Canceled)。
	ErrCanceled = context.Canceled
	// ErrTimeout is the error returned when the context's deadline passes.
	// 超时时返回(即 context.DeadlineExceeded)。
	ErrTimeout = context.DeadlineExceeded
)

// DoOption defines the method to customize a DoWithTimeout call.
// 父 ctx 注入选项。
type DoOption func() context.Context

// DoWithTimeout runs fn with timeout control.
// 带超时执行 fn:超时/取消返回 ctx.Err(),panic 原样转发,
// 正常结束返回 fn 的结果。
func DoWithTimeout(fn func() error, timeout time.Duration, opts ...DoOption) error {
	// 默认 Background,可用 WithContext 换成外部 ctx。
	parentCtx := context.Background()
	for _, opt := range opts {
		parentCtx = opt()
	}
	ctx, cancel := context.WithTimeout(parentCtx, timeout)
	defer cancel()

	// create channel with buffer size 1 to avoid goroutine leak
	// 容量 1:超时先走后,迟到的 done <- fn() 不会卡死 worker。
	done := make(chan error, 1)
	panicChan := make(chan any, 1)
	go func() {
		defer func() {
			if p := recover(); p != nil {
				// attach call stack to avoid missing in different goroutine
				// 跨 goroutine 的 panic 会丢栈,先拼好完整信息再转发。
				panicChan <- fmt.Sprintf("%+v\n\n%s", p, strings.TrimSpace(string(debug.Stack())))
			}
		}()
		// fn 正常返回(含 nil)就是一次普通发送:error 接口的
		// nil 也是合法值,channel 只管"有个值进来了"、不关心
		// 是不是 nil —— 成功路径即 done <- nil,下方 select 的
		// done 分支照常就绪;case 不就绪的唯一情形是"没有发送
		// 发生"(如 fn panic 时改走 panicChan,done 永无发送)。
		done <- fn()
	}()

	// 三选一:panic 优先转发,其次正常结果(成功时 err 为 nil,
	// 收 nil 与收非 nil 对 select 完全等价),最后超时/取消。
	select {
	case p := <-panicChan:
		panic(p)
	case err := <-done:
		return err
	case <-ctx.Done():
		return ctx.Err()
	}
}

// WithContext customizes a DoWithTimeout call with given ctx.
// 注入外部父 ctx(其取消会同样生效)。
func WithContext(ctx context.Context) DoOption {
	return func() context.Context {
		return ctx
	}
}
