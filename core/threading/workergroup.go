// ————————————————————————————————————————————————————————————————————————————
// workergroup —— 固定数量的工人跑同一个任务 —— 文件总结
//
// 最简单的 worker 池形态:开 workers 个 goroutine,每个都执行
// 同一个 job 函数(job 自己内部循环取活,如从 channel/job 队列
// 消费),Start 阻塞直到所有 worker 跑完。
// 内部复用 RoutineGroup 的 RunSafe(带 panic 保护)计数等待。
//
// 与 TaskRunner 的区别:TaskRunner 是"任务多、限并发数",
// 每个任务一个 goroutine;WorkerGroup 是"工人固定、job 相同",
// 工人数恒等于 workers,job 内部自己决定怎么分活。
// ————————————————————————————————————————————————————————————————————————————
package threading

// A WorkerGroup is used to run given number of workers to process jobs.
// 工人组:workers 个 goroutine 执行同一个 job。
type WorkerGroup struct {
	// job 每个 worker 都执行的函数(通常内部循环消费任务队列)。
	job func()
	// workers worker 数量。
	workers int
}

// NewWorkerGroup returns a WorkerGroup with given job and workers.
// 创建工人组。
func NewWorkerGroup(job func(), workers int) WorkerGroup {
	return WorkerGroup{
		job:     job,
		workers: workers,
	}
}

// Start starts a WorkerGroup.
// 启动:全部 worker 以 RunSafe(带 panic 保护)启动并等待完成;
// 阻塞直到所有 worker 退出。
func (wg WorkerGroup) Start() {
	group := NewRoutineGroup()
	for i := 0; i < wg.workers; i++ {
		group.RunSafe(wg.job)
	}
	group.Wait()
}
