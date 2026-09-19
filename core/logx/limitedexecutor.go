// ————————————————————————————————————————————————————————————————————————————
// limitedexecutor —— 限频执行器(限频机制的核心) —— 文件总结
//
// LessLogger 与 lessWriter 共用的限频内核,逻辑(logOrDiscard):
//   - 距上次放行 <= threshold 窗口 → 丢弃本次,原子计数 discarded+1;
//   - 距上次放行  >  threshold → 放行:先报告窗口内丢了几条
//     ("Discarded N error messages"),计数清零,再执行本次。
//
// 并发安全:lastTime 用 AtomicDuration,计数用 atomic uint32;
//
//	极端并发下计数/重置可能有微小竞争,但限频场景只求大致准确。
//	threshold <= 0 时不限频,全部直接执行。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/core/timex"
)

// limitedExecutor 限频执行器:threshold 时间窗口内只执行一次。
type limitedExecutor struct {
	// threshold 限频窗口时长。
	threshold time.Duration
	// lastTime 上次放行的时间点(原子,并发读写)。
	lastTime *syncx.AtomicDuration
	// discarded 窗口内被丢弃的次数(原子计数)。
	discarded uint32
}

// newLimitedExecutor 创建窗口为 milliseconds 毫秒的限频执行器。
func newLimitedExecutor(milliseconds int) *limitedExecutor {
	return &limitedExecutor{
		threshold: time.Duration(milliseconds) * time.Millisecond,
		lastTime:  syncx.NewAtomicDuration(),
	}
}

// logOrDiscard 决定 execute 是执行还是丢弃:
// 窗口内 → 丢弃并计数;窗口外 → 放行(先报告丢弃数再执行)。
// le 为 nil 或 threshold <= 0 时不限频直接执行。
func (le *limitedExecutor) logOrDiscard(execute func()) {
	if le == nil || le.threshold <= 0 {
		execute()
		return
	}

	now := timex.Now()
	if now-le.lastTime.Load() <= le.threshold {
		// 窗口内:丢弃,只计数。
		atomic.AddUint32(&le.discarded, 1)
	} else {
		// 窗口外:放行。先更新锚点、取走丢弃计数,
		// 有丢弃历史时先补报,再执行本次操作。
		le.lastTime.Set(now)
		discarded := atomic.SwapUint32(&le.discarded, 0)
		if discarded > 0 {
			Errorf("Discarded %d error messages", discarded)
		}

		execute()
	}
}
