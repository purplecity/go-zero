// ————————————————————————————————————————————————————————————————————————————
// chunkexecutor —— 按字节量批量执行器 —— 文件总结
//
// 与 BulkExecutor(按条数)相对:按**字节数**攒批 —— 每个 Add
// 时附带该任务的"大小",累计达到 chunkSize(默认 1MB)即刷出。
// 典型场景:批量上传,请求体大小受限(如 es bulk 接口),按条数
// 攒批可能超限,必须按字节数控制。
// 同样具备超时刷出、手动 Flush、进程退出自动 Flush 的保障。
// ————————————————————————————————————————————————————————————————————————————
package executors

import "time"

// defaultChunkSize 默认攒批字节上限:1MB。
const defaultChunkSize = 1024 * 1024

type (
	// ChunkOption defines the method to customize a ChunkExecutor.
	// 函数式选项。
	ChunkOption func(options *chunkOptions)

	// A ChunkExecutor is an executor to execute tasks when either requirement meets:
	// 1. up to given chunk size
	// 2. flush interval elapsed
	// 按字节数/时间批量执行的执行器。
	ChunkExecutor struct {
		// executor 底层周期执行器。
		executor *PeriodicalExecutor
		// container 字节计数任务容器。
		container *chunkContainer
	}

	// chunkOptions 配置。
	chunkOptions struct {
		// chunkSize 攒批字节上限。
		chunkSize int
		// flushInterval 强制刷出间隔。
		flushInterval time.Duration
	}
)

// NewChunkExecutor returns a ChunkExecutor.
// 创建按字节攒批的执行器。
func NewChunkExecutor(execute Execute, opts ...ChunkOption) *ChunkExecutor {
	options := newChunkOptions()
	for _, opt := range opts {
		opt(&options)
	}

	container := &chunkContainer{
		execute:      execute,
		maxChunkSize: options.chunkSize,
	}
	executor := &ChunkExecutor{
		executor:  NewPeriodicalExecutor(options.flushInterval, container),
		container: container,
	}

	return executor
}

// Add adds task with given chunk size into ce.
// 添加任务并声明其字节大小(攒批按 size 累计,与任务本体无关)。
func (ce *ChunkExecutor) Add(task any, size int) error {
	ce.executor.Add(chunk{
		val:  task,
		size: size,
	})
	return nil
}

// Flush forces ce to flush and execute tasks.
// 手动强制刷出。
func (ce *ChunkExecutor) Flush() {
	ce.executor.Flush()
}

// Wait waits the execution to be done.
// Flush 并等待全部任务完成。
func (ce *ChunkExecutor) Wait() {
	ce.executor.Wait()
}

// WithChunkBytes customizes a ChunkExecutor with the given chunk size.
// 选项:设置攒批字节上限。
func WithChunkBytes(size int) ChunkOption {
	return func(options *chunkOptions) {
		options.chunkSize = size
	}
}

// WithFlushInterval customizes a ChunkExecutor with the given flush interval.
// 选项:设置强制刷出间隔。
func WithFlushInterval(duration time.Duration) ChunkOption {
	return func(options *chunkOptions) {
		options.flushInterval = duration
	}
}

// newChunkOptions 默认配置:1MB / 1 秒。
func newChunkOptions() chunkOptions {
	return chunkOptions{
		chunkSize:     defaultChunkSize,
		flushInterval: defaultFlushInterval,
	}
}

// chunkContainer 按字节计数攒批的任务容器。
type chunkContainer struct {
	// tasks 攒下的任务本体。
	tasks []any
	// execute 业务回调。
	execute Execute
	// size 当前累计字节数。
	size int
	// maxChunkSize 字节上限。
	maxChunkSize int
}

// AddTask 追加任务并累计字节数;达到上限返回 true(立即刷出)。
// 注意任务进来时被包成 chunk(值+大小),这里拆开只存值。
func (bc *chunkContainer) AddTask(task any) bool {
	ck := task.(chunk)
	bc.tasks = append(bc.tasks, ck.val)
	bc.size += ck.size
	return bc.size >= bc.maxChunkSize
}

// Execute 把攒下的任务整体交给业务回调。
func (bc *chunkContainer) Execute(tasks any) {
	vals := tasks.([]any)
	bc.execute(vals)
}

// RemoveAll 取走全部任务、字节数清零。
func (bc *chunkContainer) RemoveAll() any {
	tasks := bc.tasks
	bc.tasks = nil
	bc.size = 0
	return tasks
}

// chunk Add 时任务与字节数的捆绑结构。
type chunk struct {
	val  any
	size int
}
