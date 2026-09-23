// ————————————————————————————————————————————————————————————————————————————
// routinegroup —— goroutine 分组等待 —— 文件总结
//
// 封装 sync.WaitGroup 的"开一组 goroutine 并等它们全部完成":
//
//	Run     开一个普通 goroutine(panic 会打崩进程,慎用);
//	RunSafe 开一个安全 goroutine(panic 被兜住记日志,推荐);
//	Wait    阻塞等这一组全部跑完。
//
// 典型用法:并行拆分任务 ——
//
//	g := threading.NewRoutineGroup()
//	g.RunSafe(taskA); g.RunSafe(taskB); g.RunSafe(taskC)
//	g.Wait() // 三个都完成后才继续
//
// 原注释的提醒:fn 里不要引用会被其他 goroutine 修改的外部变量,
// 并发读写外部变量是数据竞争,不是本类型的职责范围。
// ————————————————————————————————————————————————————————————————————————————
package threading

import "sync"

// A RoutineGroup is used to group goroutines together and all wait all goroutines to be done.
// goroutine 分组:内部就一个 WaitGroup。
type RoutineGroup struct {
	waitGroup sync.WaitGroup
}

// NewRoutineGroup returns a RoutineGroup.
// 创建分组。
func NewRoutineGroup() *RoutineGroup {
	return new(RoutineGroup)
}

// Run runs the given fn in RoutineGroup.
// Don't reference the variables from outside,
// because outside variables can be changed by other goroutines
// 开一个 goroutine 执行 fn 并纳入分组计数(无 panic 保护)。
func (g *RoutineGroup) Run(fn func()) {
	// 先 Add 再 go:保证 Wait 不会在 goroutine 还没起来时就返回。
	g.waitGroup.Add(1)

	go func() {
		defer g.waitGroup.Done()
		fn()
	}()
}

// RunSafe runs the given fn in RoutineGroup, and avoid panics.
// Don't reference the variables from outside,
// because outside variables can be changed by other goroutines
// 同 Run,但通过 GoSafe 执行,panic 被兜住不会打崩进程。
func (g *RoutineGroup) RunSafe(fn func()) {
	g.waitGroup.Add(1)

	GoSafe(func() {
		defer g.waitGroup.Done()
		fn()
	})
}

// Wait waits all running functions to be done.
// 阻塞等待分组内全部 goroutine 完成。
func (g *RoutineGroup) Wait() {
	g.waitGroup.Wait()
}
