// ————————————————————————————————————————————————————————————————————————————
// periodicalexecutor —— 攒批 + 定时刷出的通用执行器 —— 文件总结
//
// 这是 Bulk/Chunk 批量执行器的引擎,业务通过实现 TaskContainer
// 接口(攒批策略)接入:
//
//	AddTask   存入任务,返回 true 表示"该刷了"(如攒够条数/字节);
//	Execute   真正执行一整批任务;
//	RemoveAll 取走全部攒下的任务。
//
// 一、运行机制
//
//	Add(业务调用):锁内攒任务;达到刷出条件 → 把整批扔进
//	  commander(容量 1)→ 等 confirmChan 确认后台已接手 → 返回;
//	  池满说明后台正忙,先返回,任务仍在容器里等下轮。
//	backgroundFlush(懒启动后台 goroutine):select 两路 ——
//	  commander 有货 → 确认 → 立即执行该批;ticker 到点 → Flush;
//	  连续 idleRound(10)轮空闲且无在飞任务 → 退出,
//	  下次 Add 时再懒启动(空闲不养闲 goroutine)。
//
// 二、防丢数据的三道保险
//  1. 先 Add 计数再入池等确认,Wait 不会漏等在飞任务;
//  2. 后台 goroutine 退出前 defer Flush(清空残批);
//  3. proc.AddShutdownListener 注册进程退出 Flush。
//
// 三、为什么需要 wgBarrier —— 对 Add/Done/Wait 的不对称保护
//
//	标准库规则:计数为 0 时刻发生的正增量 Add,必须
//	happens-before Wait,否则是数据竞争,Wait 可能提前返回。
//
//	竞态场景(无 Barrier 时):
//	  后台刚接手一批任务,即将 Add(1);
//	  调用方 Wait() 恰在 Add(1) 生效前读到计数 0 → 立即返回,
//	  而这批任务还在飞 —— 漏等(当年正是 -race 抓出后才加的)。
//
//	wgBarrier(本质是 Mutex)只把 Add(+1) 与 Wait 串行化:
//	enterExecution 在 Guard 内 Add,Wait 也在 Guard 内 Wait,
//	二者不再交错;要么 Add 先注册好,要么 Wait 先返回。
//
//	doneExecution 的 Done 刻意不进 Barrier,两个原因:
//	  1. 不需要 —— Done 是负增量,只会让计数更小,
//	     "边 Done 边 Wait" 本就是 WaitGroup 的正常用法;
//	  2. 会死锁 —— Wait 在 Guard 内持锁等计数归零,而归零恰恰
//	     依赖 Done;Done 若再抢同一把锁 → 互相等,永远卡死。
//
//	一句话:Barrier 只隔离 Add(+1) 与 Wait 这对有竞态的组合;
//	Done(-1) 必须留在 Barrier 外 —— 既无必要,也不允许。
//
// 四、两条刷出路径 —— Add 走 commander,Flush 直接执行
//
//	Add 触发(攒够阈值)→ 异步快路径:整批扔进 commander,
//	  等 confirmChan 确认后立即返回,业务方不等执行完 —— 管吞吐。
//	Flush 触发(到点/强制/收尾)→ 同步兜底路径:RemoveAll 取走
//	  残批,在当前 goroutine 当场执行完才返回 —— 管"碎任务
//	  最终一定被执行"。五个调用点:ticker 到点(周期执行器的
//	  "定时"二字就是它)、后台退出前 defer、Wait() 开头、
//	  进程退出 listener、Bulk/ChunkExecutor 的公开 Flush API。
//
//	Flush 为何刻意不走 commander(改了就错):
//	  1. 自死锁 —— backgroundFlush 自己就是 Flush 的调用方,
//	     而 commander 的消费者、confirmChan 的发送者只有它自己;
//	     自己发、自己等自己确认 → 永远等不到;
//	  2. commander 容量 1,是交接槽不是队列 —— 后台忙时它是满的,
//	     Flush 入队会阻塞;而 Wait/进程退出恰恰依赖 Flush 的
//	     同步性(返回即执行完),改异步则 Wait 语义被破坏;
//	  3. 生命周期 —— 进程退出/后台已退出时没有活着的消费者,
//	     任务会永远躺在通道里丢失;直接执行不依赖后台存活。
//
//	账目也分开:commander 路径记 inflight(shallQuit 靠它判断
//	后台能否退出);Flush 路径走 waitGroup。Flush 内先
//	enterExecution() 加计数、再 RemoveAll 取任务 —— 顺序保证
//	Wait 不会漏等这批(即第二节第 1 条保险)。
//
// ————————————————————————————————————————————————————————————————————————————
package executors

import (
	"reflect"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/lang"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/core/threading"
	"github.com/zeromicro/go-zero/core/timex"
)

// idleRound 空闲多少个间隔后后台 goroutine 自杀。
const idleRound = 10

type (
	// TaskContainer interface defines a type that can be used as the underlying
	// container that used to do periodical executions.
	// 任务容器接口:业务的攒批策略通过它接入执行器。
	TaskContainer interface {
		// AddTask adds the task into the container.
		// Returns true if the container needs to be flushed after the addition.
		// 存入任务;返回 true 表示达到刷出条件(如攒够条数)。
		AddTask(task any) bool
		// Execute handles the collected tasks by the container when flushing.
		// 刷出时执行整批任务。
		Execute(tasks any)
		// RemoveAll removes the contained tasks, and return them.
		// 取走全部任务并清空容器。
		RemoveAll() any
	}

	// A PeriodicalExecutor is an executor that periodically execute tasks.
	// 周期执行器:攒批 + 定时刷出 + 并发安全。
	PeriodicalExecutor struct {
		// commander 后台消费的任务批通道(容量 1,让 Add 快速返回)。
		commander chan any
		// interval 刷出间隔。
		interval time.Duration
		// container 业务的任务容器。
		container TaskContainer
		// waitGroup 追踪在飞的任务批,支撑 Wait。
		waitGroup sync.WaitGroup
		// avoid race condition on waitGroup when calling wg.Add/Done/Wait(...)
		// wgBarrier 串行化 waitGroup 的 Add 与 Wait(见文件头第三节);
		// 注意:Done 并不在 Guard 内 —— 它并发安全,包进来反而死锁。
		wgBarrier syncx.Barrier
		// confirmChan Add → 后台接手确认:保证 Add 返回时批已交接。
		confirmChan chan lang.PlaceholderType
		// inflight 已交给后台、尚未执行完的批数(0 才允许后台退出)。
		inflight int32
		// guarded 后台 goroutine 是否已启动(懒启动标记)。
		guarded bool
		// newTicker ticker 工厂(可注入 FakeTicker 供测试)。
		newTicker func(duration time.Duration) timex.Ticker
		// lock 保护 container 与 guarded。
		lock sync.Mutex
	}
)

// NewPeriodicalExecutor returns a PeriodicalExecutor with given interval and container.
// 创建周期执行器:interval 为刷出间隔,container 为攒批容器;
// 并注册进程退出时的 Flush(不丢数据)。
func NewPeriodicalExecutor(interval time.Duration, container TaskContainer) *PeriodicalExecutor {
	executor := &PeriodicalExecutor{
		// buffer 1 to let the caller go quickly
		// 容量 1:后台正忙时,Add 也能把批放进通道立即返回。
		commander:   make(chan any, 1),
		interval:    interval,
		container:   container,
		confirmChan: make(chan lang.PlaceholderType),
		newTicker: func(d time.Duration) timex.Ticker {
			return timex.NewTicker(d)
		},
	}
	proc.AddShutdownListener(func() {
		executor.Flush()
	})

	return executor
}

// Add adds tasks into pe.
// 添加任务:达到刷出条件时把整批交给后台并等确认;
// 未达到则只是攒着,由后台的 ticker 定时刷出。
func (pe *PeriodicalExecutor) Add(task any) {
	if vals, ok := pe.addAndCheck(task); ok {
		pe.commander <- vals // 交给后台(池满则等,最多一个批)
		<-pe.confirmChan     // 等后台确认接手,保证交接完成
	}
}

// Flush forces pe to execute tasks.
// 强制刷出:取走容器里全部任务,在当前 goroutine 同步执行完才返回
// (即使只有一条也执行)。刻意不走 commander —— 三个原因见文件头第四节;
// 调用方(ticker/Wait/退出收尾)都依赖"返回即执行完"这一同步语义。
func (pe *PeriodicalExecutor) Flush() bool {
	pe.enterExecution()
	return pe.executeTasks(func() any {
		pe.lock.Lock()
		defer pe.lock.Unlock()
		return pe.container.RemoveAll()
	}())
}

// Sync lets caller run fn thread-safe with pe, especially for the underlying container.
// 让调用方与执行器互斥地执行 fn(比如直接读容器做统计)。
func (pe *PeriodicalExecutor) Sync(fn func()) {
	pe.lock.Lock()
	defer pe.lock.Unlock()
	fn()
}

// Wait waits the execution to be done.
// 先 Flush 清残批,再等全部在飞任务批完成。
func (pe *PeriodicalExecutor) Wait() {
	pe.Flush()
	pe.wgBarrier.Guard(func() {
		pe.waitGroup.Wait()
	})
}

// addAndCheck 攒一个任务并判断是否达到刷出条件:
// 达到 → 返回整批任务(true);同时懒启动后台 goroutine。
func (pe *PeriodicalExecutor) addAndCheck(task any) (any, bool) {
	pe.lock.Lock()
	defer func() {
		if !pe.guarded {
			pe.guarded = true
			// defer to unlock quickly
			// defer 启动后台:先解锁再起 goroutine,减少持锁时间。
			defer pe.backgroundFlush()
		}
		pe.lock.Unlock()
	}()

	// 攒任务;容器说该刷了 → 取出整批交给后台。
	if pe.container.AddTask(task) {
		atomic.AddInt32(&pe.inflight, 1)
		return pe.container.RemoveAll(), true
	}

	return nil, false
}

// backgroundFlush 后台 goroutine:
//
//	commander 分支 —— 立即执行交接来的批(确认后),并刷新活跃时间;
//	ticker 分支    —— 到点:有交接批就歇一轮;否则 Flush 残批;
//	                  连续空闲 idleRound 轮且无在飞批 → 退出。
//
// 退出前 defer Flush:把最后攒着的任务刷出去,不丢数据。
func (pe *PeriodicalExecutor) backgroundFlush() {
	go func() {
		// flush before quit goroutine to avoid missing tasks
		defer pe.Flush()

		ticker := pe.newTicker(pe.interval)
		defer ticker.Stop()

		var commanded bool
		last := timex.Now()
		for {
			select {
			case vals := <-pe.commander:
				commanded = true
				atomic.AddInt32(&pe.inflight, -1)
				pe.enterExecution()
				pe.confirmChan <- lang.Placeholder // 确认:批已接手
				pe.executeTasks(vals)
				last = timex.Now()
			case <-ticker.Chan():
				if commanded {
					// 上一轮刚消费过 commander,这轮再等等新任务。
					commanded = false
				} else if pe.Flush() {
					last = timex.Now()
				} else if pe.shallQuit(last) {
					// 长时间空闲且无在飞任务:退出,等下次 Add 懒启动。
					return
				}
			}
		}
	}()
}

// doneExecution 一个任务批执行完毕,计数减一。
// Done 刻意不进 wgBarrier:负增量与 Wait 并发安全(标准用法),
// 若进 Barrier 会与持锁等计数归零的 Wait 互相等待而死锁。
func (pe *PeriodicalExecutor) doneExecution() {
	pe.waitGroup.Done()
}

// enterExecution 登记一个在飞任务批(Barrier 内 Add,防 Wait 竞态)。
// Add(+1) 必须与 Wait 互斥:否则计数为 0 时,Wait 可能在 Add 生效前
// 读到 0 直接返回,漏等这批任务(标准库文档明确要求)。
func (pe *PeriodicalExecutor) enterExecution() {
	pe.wgBarrier.Guard(func() {
		pe.waitGroup.Add(1)
	})
}

// executeTasks 执行一批任务(有内容才执行;RunSafe 防 panic 逃逸),
// 返回这批是否非空。
func (pe *PeriodicalExecutor) executeTasks(tasks any) bool {
	defer pe.doneExecution()

	ok := pe.hasTasks(tasks)
	if ok {
		threading.RunSafe(func() {
			pe.container.Execute(tasks)
		})
	}

	return ok
}

// hasTasks 判断批次是否非空:集合类型看长度;
// 未知类型(自定义容器)交给业务执行器自行判断。
func (pe *PeriodicalExecutor) hasTasks(tasks any) bool {
	if tasks == nil {
		return false
	}

	val := reflect.ValueOf(tasks)
	switch val.Kind() {
	case reflect.Array, reflect.Chan, reflect.Map, reflect.Slice:
		return val.Len() > 0
	default:
		// unknown type, let caller execute it
		return true
	}
}

// shallQuit 判断后台是否该退出:空闲超过 idleRound 个间隔,
// 且无在飞任务批(检查与 guarded 复位必须同锁)才允许退出。
func (pe *PeriodicalExecutor) shallQuit(last time.Duration) (stop bool) {
	if timex.Since(last) <= pe.interval*idleRound {
		return
	}

	// checking pe.inflight and setting pe.guarded should be locked together
	pe.lock.Lock()
	if atomic.LoadInt32(&pe.inflight) == 0 {
		pe.guarded = false // 复位,下次 Add 会重新懒启动
		stop = true
	}
	pe.lock.Unlock()

	return
}
