// ————————————————————————————————————————————————————————————————————————————
// lessexecutor —— 限频执行器 —— 文件总结
//
// DoOrDiscard:给定时间窗口内只执行第一次,其余直接丢弃
// (返回 false)。与 syncx.LessLogger 的 limitedExecutor 同族,
// 区别:这里不计数、不补报丢弃数,纯节流。
// 典型场景:热路径上的低频动作,如"每秒最多刷一次盘/打一次点"。
// ————————————————————————————————————————————————————————————————————————————
package executors

import (
	"time"

	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/core/timex"
)

// A LessExecutor is an executor to limit execution once within given time interval.
// 限频执行器:threshold 窗口内只放行第一次。
type LessExecutor struct {
	// threshold 限频窗口。
	threshold time.Duration
	// lastTime 上次放行时刻(原子)。
	lastTime *syncx.AtomicDuration
}

// NewLessExecutor returns a LessExecutor with given threshold as time interval.
// 创建窗口为 threshold 的限频执行器。
func NewLessExecutor(threshold time.Duration) *LessExecutor {
	return &LessExecutor{
		threshold: threshold,
		lastTime:  syncx.NewAtomicDuration(),
	}
}

// DoOrDiscard executes or discards the task depends on if
// another task was executed within the time interval.
// 窗口内第一条执行并返回 true;其余直接丢弃返回 false。
// lastTime==0 表示从未执行过(首次必然放行)。
func (le *LessExecutor) DoOrDiscard(execute func()) bool {
	now := timex.Now()
	lastTime := le.lastTime.Load()
	if lastTime == 0 || lastTime+le.threshold < now {
		// 窗口外:更新锚点并执行。
		le.lastTime.Set(now)
		execute()
		return true
	}

	return false
}
