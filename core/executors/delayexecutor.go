// ————————————————————————————————————————————————————————————————————————————
// delayexecutor —— 延迟去重执行器(防抖) —— 文件总结
//
// 语义:Trigger 多次,只在第一次触发后 delay 时间执行一次 fn。
// 执行前的重复 Trigger 直接忽略;执行开始时(triggered 复位)之后
// 的 Trigger 会再排下一轮 —— 即"冷却期内合并"。
// 典型场景:配置变更通知 —— 短时间大量变更只触发一次重载。
//
// 关键细节:fn 执行前先把 triggered 复位(注释原意:确保不漏
// 触发)—— 若先执行 fn 再复位,fn 执行期间的 Trigger 会被丢弃;
// 先复位意味着 fn 执行期间的 Trigger 会排下一轮执行,不丢失。
// ————————————————————————————————————————————————————————————————————————————
package executors

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/threading"
)

// A DelayExecutor delays a tasks on given delay interval.
// 延迟防抖执行器:triggered 标记冷却状态。
type DelayExecutor struct {
	// fn 到期执行的函数。
	fn func()
	// delay 冷却时长。
	delay time.Duration
	// triggered 是否处于冷却期(已触发、尚未执行)。
	triggered bool
	// lock 保护 triggered。
	lock sync.Mutex
}

// NewDelayExecutor returns a DelayExecutor with given fn and delay.
// 创建防抖执行器。
func NewDelayExecutor(fn func(), delay time.Duration) *DelayExecutor {
	return &DelayExecutor{
		fn:    fn,
		delay: delay,
	}
}

// Trigger triggers the task to be executed after given delay, safe to trigger more than once.
// 触发:冷却期内重复调用直接忽略;到点执行 fn。
func (de *DelayExecutor) Trigger() {
	de.lock.Lock()
	defer de.lock.Unlock()

	if de.triggered {
		// 冷却期内:忽略本次触发(防抖核心)。
		return
	}

	de.triggered = true
	threading.GoSafe(func() {
		timer := time.NewTimer(de.delay)
		defer timer.Stop()
		<-timer.C

		// set triggered to false before calling fn to ensure no triggers are missed.
		// 先复位再执行 fn:fn 执行期间来的 Trigger 能排下一轮,不丢失。
		de.lock.Lock()
		de.triggered = false
		de.lock.Unlock()
		de.fn()
	})
}
