// ————————————————————————————————————————————————————————————————————————————
// bulkexecutor —— 按条数批量执行器 —— 文件总结
//
// 解决"逐条写库/发消息太慢"的问题:任务攒着,满足任一条件
// 就把整批交给 execute 回调:
//  1. 攒够 cachedTasks 条(默认 1000);
//  2. 距上次执行超过 flushInterval(默认 1 秒);
//  3. 手动 Flush();进程退出时也会自动 Flush(不丢数据)。
//
// 实现 = PeriodicalExecutor(攒批/定时/并发安全)+ bulkContainer
// (实现 TaskContainer 接口:计数判断是否该刷)。
// 典型用法:
//
//	be := executors.NewBulkExecutor(func(tasks []any) { 批量入库 })
//	be.Add(item)  // 业务循环里只管 Add
//	defer be.Wait()
//
// ————————————————————————————————————————————————————————————————————————————
package executors

import "time"

// defaultBulkTasks 默认攒批条数。
const defaultBulkTasks = 1000

type (
	// BulkOption defines the method to customize a BulkExecutor.
	// 函数式选项。
	BulkOption func(options *bulkOptions)

	// A BulkExecutor is an executor that can execute tasks on either requirement meets:
	// 1. up to given size of tasks
	// 2. flush interval time elapsed
	// 按条数/时间批量执行的执行器。
	BulkExecutor struct {
		// executor 底层的周期执行器(攒批、定时、并发安全都在这)。
		executor *PeriodicalExecutor
		// container 任务容器(实现 TaskContainer)。
		container *bulkContainer
	}

	// bulkOptions 配置。
	bulkOptions struct {
		// cachedTasks 攒批条数上限。
		cachedTasks int
		// flushInterval 强制刷出间隔。
		flushInterval time.Duration
	}
)

// NewBulkExecutor returns a BulkExecutor.
// 创建批量执行器,execute 为整批任务的业务回调。
func NewBulkExecutor(execute Execute, opts ...BulkOption) *BulkExecutor {
	options := newBulkOptions()
	for _, opt := range opts {
		opt(&options)
	}

	container := &bulkContainer{
		execute:  execute,
		maxTasks: options.cachedTasks,
	}
	executor := &BulkExecutor{
		executor:  NewPeriodicalExecutor(options.flushInterval, container),
		container: container,
	}

	return executor
}

// Add adds task into be.
// 添加一个任务(攒批逻辑由 PeriodicalExecutor 处理)。
func (be *BulkExecutor) Add(task any) error {
	be.executor.Add(task)
	return nil
}

// Flush forces be to flush and execute tasks.
// 手动强制刷出当前攒下的全部任务。
func (be *BulkExecutor) Flush() {
	be.executor.Flush()
}

// Wait waits be to done with the task execution.
// Flush 并等待全部任务执行完成(进程退出前调用,防丢数据)。
func (be *BulkExecutor) Wait() {
	be.executor.Wait()
}

// WithBulkTasks customizes a BulkExecutor with given tasks limit.
// 选项:设置攒批条数上限。
func WithBulkTasks(tasks int) BulkOption {
	return func(options *bulkOptions) {
		options.cachedTasks = tasks
	}
}

// WithBulkInterval customizes a BulkExecutor with given flush interval.
// 选项:设置强制刷出间隔。
func WithBulkInterval(duration time.Duration) BulkOption {
	return func(options *bulkOptions) {
		options.flushInterval = duration
	}
}

// newBulkOptions 默认配置:1000 条 / 1 秒。
func newBulkOptions() bulkOptions {
	return bulkOptions{
		cachedTasks:   defaultBulkTasks,
		flushInterval: defaultFlushInterval,
	}
}

// bulkContainer 任务容器:实现 TaskContainer 接口。
type bulkContainer struct {
	// tasks 攒下的任务切片。
	tasks []any
	// execute 业务回调。
	execute Execute
	// maxTasks 攒批条数上限。
	maxTasks int
}

// AddTask 追加任务;达到上限返回 true(通知立即刷出)。
func (bc *bulkContainer) AddTask(task any) bool {
	bc.tasks = append(bc.tasks, task)
	return len(bc.tasks) >= bc.maxTasks
}

// Execute 把攒下的任务整体交给业务回调。
func (bc *bulkContainer) Execute(tasks any) {
	vals := tasks.([]any)
	bc.execute(vals)
}

// RemoveAll 取走全部任务并清空容器。
func (bc *bulkContainer) RemoveAll() any {
	tasks := bc.tasks
	bc.tasks = nil
	return tasks
}
