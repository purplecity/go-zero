// ————————————————————————————————————————————————————————————————————————————
// taskrunner —— 带并发上限的任务执行器 —— 文件总结
//
// 语义:向里面扔任意多个任务,但同时运行的最多 concurrency 个 ——
// 就是"限并发的 goroutine 池":limitChan 容量即并发上限,
// 任务开始前塞入占位(满了阻塞),结束后取走占位腾位。
// WaitGroup 负责 Wait():等所有已提交任务(含排队中的)跑完。
//
// 两个易错点(注释里反复强调):
//  1. 先 waitGroup.Add(1) 再向 limitChan 塞占位 —— 顺序反了的话,
//     池满阻塞时 Wait 可能提前返回,任务却在之后才跑起来;
//  2. 任务的 defer 里"取占位 + Done"要放在 rescue.Recover 的
//     cleanup 里 —— 即使 task panic 也保证释放占位和计数,
//     否则一次 panic 就永久占掉一个并发名额。
//
// ————————————————————————————————————————————————————————————————————————————
package threading

import (
	"errors"
	"sync"

	"github.com/zeromicro/go-zero/core/lang"
	"github.com/zeromicro/go-zero/core/rescue"
)

// ErrTaskRunnerBusy is the error that indicates the runner is busy.
// ScheduleImmediately 在池满时返回此错误。
var ErrTaskRunnerBusy = errors.New("task runner is busy")

// A TaskRunner is used to control the concurrency of goroutines.
// 并发受控的任务执行器:limitChan 容量 = 最大并发数。
type TaskRunner struct {
	// limitChan 并发占位池:塞入=占名额,取走=释放名额。
	limitChan chan lang.PlaceholderType
	// waitGroup 追踪全部已提交任务,支撑 Wait。
	waitGroup sync.WaitGroup
}

// NewTaskRunner returns a TaskRunner.
// 创建并发上限为 concurrency 的执行器。
func NewTaskRunner(concurrency int) *TaskRunner {
	return &TaskRunner{
		limitChan: make(chan lang.PlaceholderType, concurrency),
	}
}

// Schedule schedules a task to run under concurrency control.
// 提交任务:池满时阻塞排队,有空位后并发执行。
func (rp *TaskRunner) Schedule(task func()) {
	// Why we add waitGroup first, in case of race condition on starting a task and wait returns.
	// For example, limitChan is full, and the task is scheduled to run, but the waitGroup is not added,
	// then the wait returns, and the task is then scheduled to run, but caller thinks all tasks are done.
	// the same reason for ScheduleImmediately.
	// 必须先计数再占池位:顺序反了,池满阻塞期间调用 Wait
	// 会误判"全部完成",而任务其实还没跑。
	rp.waitGroup.Add(1)
	rp.limitChan <- lang.Placeholder

	go func() {
		// panic 也必须释放名额和计数:放在 Recover 的 cleanup 里,
		// task 是否 panic 都会执行。
		defer rescue.Recover(func() {
			<-rp.limitChan
			rp.waitGroup.Done()
		})

		task()
	}()
}

// ScheduleImmediately schedules a task to run immediately under concurrency control.
// It returns ErrTaskRunnerBusy if the runner is busy.
// 非阻塞版提交:池满立即返回 ErrTaskRunnerBusy,不排队。
// 拒绝时要回滚 Add 的计数。
func (rp *TaskRunner) ScheduleImmediately(task func()) error {
	// Why we add waitGroup first, check the comment in Schedule.
	rp.waitGroup.Add(1)
	select {
	case rp.limitChan <- lang.Placeholder:
	default:
		// 池满拒绝:回滚计数,否则 Wait 会永远等这个任务。
		rp.waitGroup.Done()
		return ErrTaskRunnerBusy
	}

	go func() {
		defer rescue.Recover(func() {
			<-rp.limitChan
			rp.waitGroup.Done()
		})
		task()
	}()

	return nil
}

// Wait waits all running tasks to be done.
// 阻塞等待全部已提交任务(含排队中)完成。
func (rp *TaskRunner) Wait() {
	rp.waitGroup.Wait()
}
