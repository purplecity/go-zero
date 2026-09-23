// ————————————————————————————————————————————————————————————————————————————
// stablerunner —— 保序并行处理器 —— 文件总结
//
// 需求:消息要并行处理(快),但取出结果时必须严格按推送顺序(稳)。
// 典型场景:Kafka 消费者 —— 分区内消息有序,处理可以并行,
// 下游拿到的结果顺序不能乱。
//
// 核心设计:环形缓冲(ring,大小 = CPU 数 × 10)+ 双游标:
//
//	writtenIndex 推送计数:Push 时原子 +1,消息落在 ring[序号 % 容量];
//	consumedIndex 消费计数:Get 每取走一个 +1。
//
// 每个 ring 槽位是一个容量为 1 的 channel:Push 时把处理任务扔给
// TaskRunner 并行执行,结果写进自己的槽位 channel;Get 按序号
// 读对应槽位 —— 槽位没好就阻塞,天然保证取出顺序 == 推送顺序。
// 并行度由内部 TaskRunner(=CPU 数)控制,槽位 channel 互不阻塞。
//
// 收尾:Wait 关闭 done,Get 在 done 关闭后若还有未取的已写消息,
// 会继续正常取出(见 select 的 done 分支),取完才返回 ErrRunnerClosed。
//
// 并发窗口与契约(重要):
//
//  1. 「任务未执行完 + done 已关闭」的窗口真实存在:Push 先加
//     writtenIndex、任务异步执行;Wait 先 close done。窗口内 Get
//     的 select 只剩 done 就绪,走 done 分支后是裸阻塞接收 ——
//     它安全的原因:Wait 里先 runner.Wait() 再轮询,已调度的
//     任务必然执行完毕并完成槽位写入,值必然到达,阻塞必然解除。
//     这种阻塞是 Get「按序等待」的设计行为,不是死锁(close done
//     不是取消信号,不会叫停已调度的任务)。
//  2. 隐含契约:handle 不得 panic。若 handle panic,runner 的
//     rescue 会兜住 —— 进程不死,但 runner 从此卡死在这条消息上:
//     槽位 channel 永远不会被写入,该槽位的 Get 与 Wait 永久阻塞
//     (静默挂死:CPU 几乎为零,仅一条 error 栈日志)。危害会
//     随时间扩散:每绕环形缓冲一圈泄漏一个卡在发送上的任务
//     goroutine;绕两圈后槽位锁被占,Push 也开始阻塞,依赖本
//     runner 的下游(如消费循环)随之停摆。按序交付下毒消息
//     无正确出路(跳过破序、伪造污染下游),责任交给调用方:
//     在 handle 内部自行 rescue。
//
// ————————————————————————————————————————————————————————————————————————————
package threading

import (
	"errors"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

const factor = 10

var (
	// ErrRunnerClosed runner 已关闭后继续 Push/Get 的错误。
	ErrRunnerClosed = errors.New("runner closed")

	// bufSize 环形缓冲槽位数 = CPU 数 × factor。
	bufSize = runtime.NumCPU() * factor
)

// StableRunner is a runner that guarantees messages are taken out with the pushed order.
// This runner is typically useful for Kafka consumers with parallel processing.
// 保序并行处理器:并行处理,按推送顺序取出。
type StableRunner[I, O any] struct {
	// handle 用户的处理函数(输入 → 输出)。
	// 契约:不得 panic —— 否则 runner 卡死在该条消息上,
	// 该槽位的 Get/Wait 永久阻塞(进程不崩,静默挂死)。
	handle func(I) O
	// consumedIndex 已消费(取出)的消息计数,也是下一个待取的序号。
	consumedIndex uint64
	// writtenIndex 已推送的消息计数。
	writtenIndex uint64
	// ring 环形缓冲:每槽一个容量 1 的结果 channel + 槽位锁。
	ring []*struct {
		value chan O
		lock  sync.Mutex
	}
	// runner 内部并行执行器(并行度 = CPU 数)。
	runner *TaskRunner
	// done 关闭信号:Wait 时关闭,Get/Push 据此判断收尾状态。
	done chan struct{}
}

// NewStableRunner returns a new StableRunner with given message processor fn.
// 创建处理器,fn 为消息处理函数。
func NewStableRunner[I, O any](fn func(I) O) *StableRunner[I, O] {
	// 预建全部槽位:每个槽位一个容量 1 的结果 channel。
	ring := make([]*struct {
		value chan O
		lock  sync.Mutex
	}, bufSize)
	for i := 0; i < bufSize; i++ {
		ring[i] = &struct {
			value chan O
			lock  sync.Mutex
		}{
			value: make(chan O, 1),
		}
	}

	return &StableRunner[I, O]{
		handle: fn,
		ring:   ring,
		runner: NewTaskRunner(runtime.NumCPU()),
		done:   make(chan struct{}),
	}
}

// Get returns the next processed message in order.
// This method should be called in one goroutine.
// 按推送顺序取出下一个已处理结果。结果未就绪时阻塞等待 ——
// 这是「按序」的设计行为,不是死锁(见文件头的并发窗口说明)。
//
// 单 goroutine 是正确性前提,不是建议。消费模式 = 单消费者循环:
//
//	for {
//	    out, err := runner.Get()   // 取到一条,处理完再取下一条
//	    ...
//	}
//
// 原因:consumedIndex 的读(下方 Load)与写(defer Add)是两个
// 独立的原子操作,中间隔着整个阻塞等待,不是原子的读改写 ——
// 并发调用 Get 会读到同一个 index、对同一槽位接收:一个拿到值,
// 另一个挂在槽位上,等 bufSize 条之后同槽位的新结果写入时被它
// 偷走,拿到不属于它的消息,顺序错乱。需要并行交付时,把 Get
// 的结果交给下游 goroutine 处理,Get 本身永远只有一个调用者
// (按序由 Get 保证,并行由下游做,同 Kafka 单分区消费形态)。
//
// 与 handle panic 的联动(见文件头契约):Get 卡死时 defer 不
// 执行 → consumedIndex 冻结 → 后续所有 Get 读到同一个 index、
// 堆积阻塞在同一个毒槽位上;Wait 的轮询也因此永远追不平。
func (r *StableRunner[I, O]) Get() (O, error) {
	// 取走一个,消费游标前移。
	// 注意:只有本函数成功返回(含 ErrRunnerClosed 路径)才 +1;
	// 若永久阻塞(如毒消息),游标就此冻结,后续 Get 全部堵死
	// 在同一槽位,Wait 的轮询也永远追不平。
	defer atomic.AddUint64(&r.consumedIndex, 1)

	index := atomic.LoadUint64(&r.consumedIndex)
	offset := index % uint64(bufSize)
	holder := r.ring[offset]

	select {
	case o := <-holder.value:
		// 正常路径:该槽位的处理结果已就绪。
		return o, nil
	case <-r.done:
		// runner 已关闭:若还有已推送未取走的消息,继续正常取;
		// 否则宣告关闭。保证 Wait 之前 push 的消息不丢。
		// 注意这里是裸阻塞接收而非再套 select:值必然会在稍后
		// 写入 —— Wait 先 runner.Wait(),已调度任务必然执行完毕
		// 并完成槽位写入,即使任务此刻还没跑完,也一定会跑完。
		if atomic.LoadUint64(&r.consumedIndex) < atomic.LoadUint64(&r.writtenIndex) {
			return <-holder.value, nil
		}

		var o O
		return o, ErrRunnerClosed
	}
}

// Push pushes the message v into the runner and to be processed concurrently,
// after processed, it will be cached to let caller take it in pushing order.
// 推送消息:分配下一个序号,把处理任务扔给 TaskRunner 并行执行,
// 结果写进序号对应的槽位,等待 Get 按序取走。
// 契约:handle 不得 panic —— panic 会被 runner 的 rescue 兜住
// (进程不死),但 runner 卡死在这条消息上:槽位永不写入,
// 该槽位的 Get/Wait 永久阻塞,后续同槽位消息会逐渐泄漏 goroutine。
func (r *StableRunner[I, O]) Push(v I) error {
	select {
	case <-r.done:
		return ErrRunnerClosed
	default:
		// 先原子自增拿序号(从 1 开始),减 1 后对容量取模得槽位下标。
		index := atomic.AddUint64(&r.writtenIndex, 1)
		offset := (index - 1) % uint64(bufSize)
		holder := r.ring[offset]
		// 槽位锁:同一槽位上一条消息没被取走前(容量 1 的 channel
		// 已满),这里会阻塞,天然实现背压,防止覆盖未消费的结果。
		holder.lock.Lock()
		r.runner.Schedule(func() {
			defer holder.lock.Unlock()
			o := r.handle(v)
			holder.value <- o
		})

		return nil
	}
}

// Wait waits all the messages to be processed and taken from inner buffer.
// 收尾:关闭 done 停止接收 → 等内部并行任务全部跑完 →
// 轮询等消费游标追平写入游标(缓冲里的结果全被取走)。
func (r *StableRunner[I, O]) Wait() {
	close(r.done)
	r.runner.Wait()
	for atomic.LoadUint64(&r.consumedIndex) < atomic.LoadUint64(&r.writtenIndex) {
		time.Sleep(time.Millisecond)
	}
}
