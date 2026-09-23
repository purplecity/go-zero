// ————————————————————————————————————————————————————————————————————————————
// cond —— 带超时的条件变量(channel 版) —— 文件总结
//
// 与标准库 sync.Cond 的区别:
//
//	sync.Cond 无法带超时等待(Wait 只能死等),本实现用 channel
//	+ select + timer 实现"等到信号或超时,并告知剩余时间"。
//
// 语义:
//
//	Signal           发一个信号(非阻塞,没人等就丢弃);
//	Wait             死等信号;
//	WaitWithTimeout  等到信号 → 返回(剩余时间, true);
//	                 超时 → 返回 (0, false)。
//
// 内部 channel 零容量:Signal 只有在有人正 Wait 时才送达,
// 天然实现"唤醒一个等待者";配合 timex 计算剩余等待时长。
// 典型用途:timeoutlimit.go 的带超时借用。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"time"

	"github.com/zeromicro/go-zero/core/lang"
	"github.com/zeromicro/go-zero/core/timex"
)

// A Cond is used to wait for conditions.
// 条件变量:signal 是零容量 channel,一次 Signal 唤醒一个等待者。
type Cond struct {
	signal chan lang.PlaceholderType
}

// NewCond returns a Cond.
// 创建条件变量。
func NewCond() *Cond {
	return &Cond{
		signal: make(chan lang.PlaceholderType),
	}
}

// WaitWithTimeout wait for signal return remain wait time or timed out.
// 带超时等待:等到信号返回(剩余可用时间, true);
// 超时返回 (0, false)。剩余时间 = 总超时 - 已等待时长,
// 供调用方在外层循环里继续等待使用。
func (cond *Cond) WaitWithTimeout(timeout time.Duration) (time.Duration, bool) {
	timer := time.NewTimer(timeout)
	defer timer.Stop()

	begin := timex.Now()
	select {
	case <-cond.signal:
		// 收到信号:扣掉已经等掉的时间,返回剩余部分。
		elapsed := timex.Since(begin)
		remainTimeout := timeout - elapsed
		return remainTimeout, true
	case <-timer.C:
		return 0, false
	}
}

// Wait waits for signals.
// 死等信号(不带超时)。
func (cond *Cond) Wait() {
	<-cond.signal
}

// Signal wakes one goroutine waiting on c, if there is any.
// 发送信号:select+default 非阻塞 —— 没有等待者就丢弃信号,
// 绝不阻塞发送方;零容量 channel 保证只唤醒一个等待者。
func (cond *Cond) Signal() {
	select {
	case cond.signal <- lang.Placeholder:
	default:
	}
}
