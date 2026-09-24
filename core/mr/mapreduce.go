// ————————————————————————————————————————————————————————————————————————————
// mapreduce —— 通用 MapReduce 并行编排框架 —— 文件总结
//
// 一、是什么
//
//	把一批数据(generate 产出)并行处理(mapper),结果汇聚后
//	归约(reducer)产出最终值。所有环节都支持取消:任意一个
//	mapper 调 cancel(err) 即终止整个流程并返回该错误。
//	最常用的两个封装:
//	  Finish(fns...)       —— 并行执行一批函数,任一失败即取消其余并返回错误
//	  MapReduceVoid        —— 只要 map 阶段,不要最终输出
//
// 二、数据流
//
//	generate ──产出──> source ──分发──> N 个 worker 并行 mapper
//	                                        │ writer.Write
//	                                        ▼
//	                                   collector(带缓冲)
//	                                        ▼
//	                                  reducer ──Write──> output ──> 调用方
//
// 三、关键机制
//
//	panicChan(onceChan)    任何 goroutine panic 只写一次,主流程
//	                       收到后原样 panic 给调用方(带原始栈);
//	cancel(once 包装)       只生效一次:记录错误 → drain source →
//	                       close(done+output),全部环节快速退出;
//	guardedWriter          done/ctx 关闭后 Write 变成空操作,防止
//	                       向已关闭 channel 发送 panic;
//	executeMappers 的
//	worker 池(pool)       限制并发 mapper 数(默认 16);
//	drain                  把 channel 喝干,避免生产者阻塞卡死。
//
// 四、channel 接收语义 —— item, ok := <-source 是流水线的"取料口"
//
//	空且未关闭 → 阻塞等待,只有两种唤醒:
//	  收到消息    item=该值, ok=true  —— 派给 mapper;
//	  关闭且取干  item=零值, ok=false —— 归还名额、触发收尾。
//	item/ok 总会被赋值,区别在取值 —— 先判 ok 再用 item,
//	零值 item 绝不使用;已关闭的 channel 永不阻塞。
//
//	两点结合本文件:
//	  1. source 无缓冲,"空"是常态 —— 生产者每发一个都要
//	     等消费方来接,阻塞就是这个流水线的节拍(汇合点);
//	  2. executeMappers 里这个接收不在 select 内:抢到 pool
//	     名额后是裸阻塞收,ctx/doneChan 取消信号叫不醒它,
//	     能唤醒它的只有"生产者来料"或"close(source)" ——
//	     buildSource 的 defer close 保证后者必然发生,
//	     所以不会永久卡死。
//
// 五、所有权协议 —— 每个 channel 恰好一个关闭者
//
//	并发收发本身永远安全;真正会 panic 的只有两件事:
//	向已关闭的 channel 发送、关闭已关闭的 channel。
//	两者都被"唯一 closer + 闸门"挡住:
//	  source      唯一 closer:buildSource defer(成败都关);
//	  collector   唯一 closer:executeMappers defer,且先
//	              wg.Wait(等全部 mapper 返回,不可能还有
//	              发送方)再 close —— send-on-closed 无从发生;
//	  done/output 唯一 closer:finish(closeOnce 幂等),
//	              reducer 侧与 executeMappers 侧都调也只关一次。
//	reducer 提前退出后 drain(collector) 与 mapper 的发送并发,
//	不仅安全,恰是为了放行卡在发送上的 mapper。
//
//	executeMappers 有四种退出触发(停了才走 defer 收尾链):
//	  ctx 取消 / doneChan 关闭(捷径)/ source 取干(保底)/
//	  mapper panic(failed)。等待图无环:drain(collector) 等
//	close(collector),后者等 executeMappers 退出,而退出不
//	依赖 done(有保底触发);drain(source) 只等生产者收工 ——
//	契约内不可能死锁。契约:generate 必须有限(或响应 ctx);
//	mapper 若卡死不返回,主流程仍可返回,仅泄漏该 goroutine。
//
// 六、close 语义 —— 关闭 ≠ 作废数据,buildSource 为何安全
//
//	close 只表示"不会再有发送":缓冲里未取走的值照样逐个
//	收到(ok=true),取干后才 ok=false;for range ch 唯一的
//	退出条件就是"已关闭且已取干"。所以"生产完就关、消费
//	还没跟上"并不丢数据。
//
//	source 更进一步是无缓冲:发送必须有人接走才返回 →
//	generate return 时每个值都已交付,不存在"没消费完就关";
//	无缓冲 = 天然背压,生产者永远等消费者(见第四节 1)。
//
//	真正的反向风险是"发送方送不出去卡死"(取消后没人收了),
//	这正是 drain 的使命:cancel 与 executeMappers defer 各有
//	一处 drain(source)(丢弃是有意的:流程已取消),
//	reducer goroutine 的 drain(collector) 同理放行 mapper。
//	defer close 还兜底 panic:生产者中途死了也保证关闭,
//	消费方不会对着死生产者无限等。
//
// 七、三层抽干对称 —— close 释放接收方,drain 释放发送方
//
//	每一层的消费者在"退出时"都会抽干它的上游通道
//	(取一个扔一个,陪生产者走到 close),三层完全对称:
//	  生产段  generate→source   消费者 executeMappers:
//	          defer drain(source)(cancel 里另有一处);
//	  映射段  mapper→collector  消费者 reducer:
//	          其 goroutine defer drain(collector);
//	  归约段  reducer→output    消费者 主 goroutine:
//	          defer for range output(防多写检查,本质即
//	          drain)+ panic 分支 drain(output)。
//
//	为什么必是"消费者抽干上游":唯一可能卡死的角色是发送方
//	(无缓冲/缓冲满时,发送必须有人接)。消费者退出是主动行为,
//	它一走,上游就没人接应了 —— 所以走之前必须陪到上游 close,
//	生产者才能跑到 return、执行 defer close、不泄漏。
//
//	口诀:close 让等在接收上的人立刻醒(ok=false);
//	drain 让卡在发送上的人走到收工。两者成对出现在每一层,
//	全链 goroutine 才有始有终。
//
// 八、ctx 与 done —— 外部控制的入口,内部广播的闸门
//
//	ctx(options.ctx) 外部传入(默认 Background,WithContext
//	                 可替换),表达"外面不要了":超时/上游取消,
//	                 生命周期比本次调用长。
//	done(doneChan)   每次调用内部临时创建,从不外部传入,
//	                 随本次 MapReduce 调用同生共死。
//
//	三个终点汇入同一闸门:
//	  外部 ctx 到期 → 主 goroutine 调 cancel(DeadlineExceeded)
//	  业务取消     → mapper/reducer 调 cancel(err)
//	  自然完成     → reducer 结束,其 defer 直接调 finish()
//	  前两者走 cancel(once):记 retErr → drain(source) →
//	  finish(closeOnce) —— 无论哪条路,最终都是
//	  close(done) + close(output) = 全员停止广播。
//
//	两个信号的观察者高度重合(任一触发即动作):
//	  executeMappers 循环:select 两路,停止派发;
//	  两处 guardedWriter:Write 直接丢弃;
//	  主 goroutine:直接看 ctx.Done;done 不直接看 ——
//	  finish 同时关 output,主 select 从 output 关闭感知它。
//	  generate 与 reducer goroutine 本身不看信号:前者靠
//	  用户契约(要可中断就自己响应 ctx),后者靠 collector
//	  关闭结束。
//
//	为什么有了 ctx 还要 done:
//	  1. ctx 只能表达"取消",表达不了"正常完成";
//	  2. 内部事件关不掉外部 ctx,广播只能用自己的通道;
//	  3. done 每次调用一个新实例,状态不跨调用泄漏。
//	方向口诀:ctx 外→内,done 内→全链。
//
// 九、cancel 总线 —— 唯一实例贯穿全链,第一票有效
//
//	cancel 定义在编排层(mapReduceWithPanicChan),定义后作为
//	参数发放给每个角色,是流水线里贯穿最长的东西:
//	  主 goroutine:ctx.Done 分支调 cancel(DeadlineExceeded)
//	  —— 外部取消也被翻译成一次 cancel 调用;
//	  reducer:签名第 3 参,业务判断失败即 cancel(err);
//	  每个 mapper:签名第 3 参(executeMappers 闭包包装传入);
//	  Finish 封装:fn() 出错即 cancel(err)。
//	即:取消有多条来路(ctx 超时/业务失败),入口只有一个。
//
//	once(sync.Once)保证:并发调用安全,全局恰好生效一次;
//	第一票的 err 原子存入 retErr,成为整个调用的返回错误
//	(主 goroutine 在 output 分支取走),后来者全部静默无效
//	—— first error wins。cancel(nil) 用 ErrCancelWithNil
//	占位,避免"取消了但错误为 nil"的歧义。
//
//	拉闸即收尾,三步:记 retErr → drain(source) 放行生产者
//	(第七节)→ finish() 关 done+output(第八节闸门),
//	不存在"取消了但没人知道"的中间态。
//
//	不对称:generate 不持有 cancel —— 拉闸权只发给"见过
//	数据的人"(mapper/reducer)与编排方;生产者卡住由
//	drain 解救,而非自己拉闸。
//
//	三线分工(与七、八节拼成完整图景):
//	  ctx/done 是"看"的(select 收听、writer 检查);
//	  cancel 是"做"的(唯一入口、第一票有效);
//	  finish 是"出"的(统一出口:关 done+output 广播停止)。
//
// ————————————————————————————————————————————————————————————————————————————
package mr

import (
	"context"
	"errors"
	"fmt"
	"runtime/debug"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/zeromicro/go-zero/core/errorx"
)

const (
	// defaultWorkers 默认并行 mapper 数。
	defaultWorkers = 16
	// minWorkers worker 数下限。
	minWorkers = 1
)

var (
	// ErrCancelWithNil is an error that mapreduce was cancelled with nil.
	// 调用 cancel(nil) 时替代 nil 的占位错误。
	ErrCancelWithNil = errors.New("mapreduce cancelled with nil")
	// ErrReduceNoOutput is an error that reduce did not output a value.
	// reducer 没有写出最终值时返回。
	ErrReduceNoOutput = errors.New("reduce not writing value")
)

type (
	// ForEachFunc is used to do element processing, but no output.
	// 只处理元素、无输出。
	ForEachFunc[T any] func(item T)
	// GenerateFunc is used to let callers send elements into source.
	// 生产者:把元素逐个写进 source channel(写完即返回,source 会被关闭)。
	GenerateFunc[T any] func(source chan<- T)
	// MapFunc is used to do element processing and write the output to writer.
	// 映射:处理一个元素,结果通过 writer 写出。
	MapFunc[T, U any] func(item T, writer Writer[U])
	// MapperFunc is used to do element processing and write the output to writer,
	// use cancel func to cancel the processing.
	// 带取消能力的映射:发现异常时调 cancel(err) 终止整个流程。
	MapperFunc[T, U any] func(item T, writer Writer[U], cancel func(error))
	// ReducerFunc is used to reduce all the mapping output and write to writer,
	// use cancel func to cancel the processing.
	// 归约:消费 mapper 的全部输出,通过 writer 写出唯一最终值。
	ReducerFunc[U, V any] func(pipe <-chan U, writer Writer[V], cancel func(error))
	// VoidReducerFunc is used to reduce all the mapping output, but no output.
	// Use cancel func to cancel the processing.
	// 无输出的归约(如聚合校验、落库)。
	VoidReducerFunc[U any] func(pipe <-chan U, cancel func(error))
	// Option defines the method to customize the mapreduce.
	// 函数式选项(WithContext/WithWorkers)。
	Option func(opts *mapReduceOptions)

	// mapperContext 传递给 mapper 调度器(executeMappers)的上下文。
	mapperContext[T, U any] struct {
		ctx       context.Context
		mapper    MapFunc[T, U]
		source    <-chan T
		panicChan *onceChan
		collector chan<- U
		doneChan  <-chan struct{}
		workers   int
	}

	// mapReduceOptions 运行时选项。
	mapReduceOptions struct {
		ctx     context.Context
		workers int
	}

	// Writer interface wraps Write method.
	// mapper/reducer 写结果的统一出口。
	Writer[T any] interface {
		Write(v T)
	}
)

// Finish runs fns parallelly, cancelled on any error.
// 并行执行一批函数,任一返回错误即取消其余,返回第一个错误;
// 全部成功返回 nil。内部 = MapReduceVoid + WithWorkers(数量=函数个数)。
func Finish(fns ...func() error) error {
	if len(fns) == 0 {
		return nil
	}

	return MapReduceVoid(func(source chan<- func() error) {
		for _, fn := range fns {
			source <- fn
		}
	}, func(fn func() error, writer Writer[any], cancel func(error)) {
		if err := fn(); err != nil {
			cancel(err)
		}
	}, func(pipe <-chan any, cancel func(error)) {
	}, WithWorkers(len(fns)))
}

// FinishVoid runs fns parallelly.
// 并行执行一批无返回值的函数,不等错误(Finish 的 void 版)。
func FinishVoid(fns ...func()) {
	if len(fns) == 0 {
		return
	}

	ForEach(func(source chan<- func()) {
		for _, fn := range fns {
			source <- fn
		}
	}, func(fn func()) {
		fn()
	}, WithWorkers(len(fns)))
}

// ForEach maps all elements from given generate but no output.
// 对 generate 产出的每个元素并行执行 mapper,无输出;
// mapper panic 会原样抛给调用方。
func ForEach[T any](generate GenerateFunc[T], mapper ForEachFunc[T], opts ...Option) {
	options := buildOptions(opts...)
	panicChan := &onceChan{channel: make(chan any)}
	source := buildSource(generate, panicChan)
	collector := make(chan any)
	// done 是"哑信号":只为填满 mapperContext/guardedWriter 的
	// select 分支,ForEach 无人关它 —— 退出靠主循环收到
	// collector 关闭(对比第八节:完整流程里 done 才承载广播)。
	done := make(chan struct{})

	go executeMappers(mapperContext[T, any]{
		ctx: options.ctx,
		mapper: func(item T, _ Writer[any]) {
			mapper(item)
		},
		source:    source,
		panicChan: panicChan,
		collector: collector,
		doneChan:  done,
		workers:   options.workers,
	})

	// 主 goroutine 等 mapper 全部跑完(collector 关闭);
	// 期间任何 worker panic 都从这里原样抛出。
	for {
		select {
		case v := <-panicChan.channel:
			panic(v)
		case _, ok := <-collector:
			if !ok {
				return
			}
		}
	}
}

// MapReduce maps all elements generated from given generate func,
// and reduces the output elements with given reducer.
// 完整的 generate → map → reduce 流程,返回最终值与错误。
func MapReduce[T, U, V any](generate GenerateFunc[T], mapper MapperFunc[T, U], reducer ReducerFunc[U, V],
	opts ...Option) (V, error) {
	panicChan := &onceChan{channel: make(chan any)}
	source := buildSource(generate, panicChan)
	return mapReduceWithPanicChan(source, panicChan, mapper, reducer, opts...)
}

// MapReduceChan maps all elements from source, and reduce the output elements with given reducer.
// 同 MapReduce,但数据源是现成的 channel(不由 generate 构造)。
func MapReduceChan[T, U, V any](source <-chan T, mapper MapperFunc[T, U], reducer ReducerFunc[U, V],
	opts ...Option) (V, error) {
	panicChan := &onceChan{channel: make(chan any)}
	return mapReduceWithPanicChan(source, panicChan, mapper, reducer, opts...)
}

// MapReduceVoid maps all elements generated from given generate,
// and reduce the output elements with given reducer.
// 无最终输出的 MapReduce:reducer 只消费不产出;
// ErrReduceNoOutput 是内部约定的哨兵错误,对外吞掉。
func MapReduceVoid[T, U any](generate GenerateFunc[T], mapper MapperFunc[T, U],
	reducer VoidReducerFunc[U], opts ...Option) error {
	_, err := MapReduce(generate, mapper, func(input <-chan U, writer Writer[any], cancel func(error)) {
		reducer(input, cancel)
	}, opts...)
	if errors.Is(err, ErrReduceNoOutput) {
		return nil
	}

	return err
}

// WithContext customizes a mapreduce processing accepts a given ctx.
// 选项:绑定 ctx,ctx 取消时整个流程取消。
func WithContext(ctx context.Context) Option {
	return func(opts *mapReduceOptions) {
		opts.ctx = ctx
	}
}

// WithWorkers customizes a mapreduce processing with given workers.
// 选项:设置并行 mapper 数(低于 1 按 1 计,默认 16)。
func WithWorkers(workers int) Option {
	return func(opts *mapReduceOptions) {
		if workers < minWorkers {
			opts.workers = minWorkers
		} else {
			opts.workers = workers
		}
	}
}

// buildOptions 从零值选项开始依次应用选项。
func buildOptions(opts ...Option) *mapReduceOptions {
	options := newOptions()
	for _, opt := range opts {
		opt(options)
	}

	return options
}

// buildPanicInfo 把 panic 值与调用栈拼成一条完整信息。
func buildPanicInfo(r any, stack []byte) string {
	return fmt.Sprintf("%+v\n\n%s", r, strings.TrimSpace(string(stack)))
}

// buildSource 启动生产者 goroutine 执行 generate:
// generate 结束(或 panic)后关闭 source,消费方的 range 自然结束;
// panic 信息经 panicChan 传给主流程。
// 关闭不丢数据:close 只表示"不再发送",缓冲值照样可取;
// 且 source 无缓冲 —— generate 返回时每个值都已被接走
// (详见文件头第六节);defer 保证 panic 时也关闭。
func buildSource[T any](generate GenerateFunc[T], panicChan *onceChan) chan T {
	source := make(chan T)
	go func() {
		defer func() {
			if r := recover(); r != nil {
				panicChan.write(buildPanicInfo(r, debug.Stack()))
			}
			// 无论成败都关闭 source:下游 range 才能结束。
			close(source)
		}()

		generate(source)
	}()

	return source
}

// drain drains the channel.
// 把 channel 里剩余的值全部喝干直到关闭:
// 取消后生产者可能还卡在发送上,不喝干它们就永远退不出来。
// 对偶口诀:close 释放接收方,drain 释放发送方(见文件头第七节);
// 三层流水线里每一层消费者退出时都靠它陪生产者走到 close。
func drain[T any](channel <-chan T) {
	// drain the channel
	for range channel {
	}
}

// executeMappers mapper 调度器:
// 按 workers 限流(pool 信号量)从 source 取元素,每个元素一个
// goroutine 并行执行 mapper;收到 ctx/doneChan 取消信号即退出;
// 退出前等全部 mapper 结束、关闭 collector、drain source。
func executeMappers[T, U any](mCtx mapperContext[T, U]) {
	var wg sync.WaitGroup
	defer func() {
		// 顺序是安全核心:先等全部 mapper 返回(不可能还有
		// 发送方)再 close —— send-on-closed 无从发生(第五节)。
		wg.Wait()             // 等所有在飞 mapper 结束
		close(mCtx.collector) // 通知 reducer:没有更多输出了(缓冲值仍可取)
		drain(mCtx.source)    // 喝干 source,让生产者能退出
	}()

	// failed 非 0 表示有 mapper panic 过,停止派发新任务。
	var failed int32
	// pool 是容量为 workers 的信号量,限制并发 mapper 数。
	pool := make(chan struct{}, mCtx.workers)
	writer := newGuardedWriter(mCtx.ctx, mCtx.collector, mCtx.doneChan)
	for atomic.LoadInt32(&failed) == 0 {
		select {
		case <-mCtx.ctx.Done():
			return
		case <-mCtx.doneChan:
			return
		case pool <- struct{}{}:
			// 占到一个 worker 名额,取下一个元素。
			// 裸阻塞收(不在 select 内,取消信号叫不醒):
			// source 空则等生产者来料(ok=true);
			// source 已关闭则 ok=false —— 归还名额、收尾退出。
			item, ok := <-mCtx.source
			if !ok {
				<-pool
				return
			}

			wg.Add(1)
			go func() {
				defer func() {
					if r := recover(); r != nil {
						atomic.AddInt32(&failed, 1)
						mCtx.panicChan.write(buildPanicInfo(r, debug.Stack()))
					}
					wg.Done()
					<-pool // 释放 worker 名额
				}()

				mCtx.mapper(item, writer)
			}()
		}
	}
}

// mapReduceWithPanicChan maps all elements from source, and reduce the output elements with given reducer.
// MapReduce 的核心实现,串起整条流水线:
//
//	source → executeMappers(并行 mapper)→ collector → reducer → output
//
// 主 goroutine 在 output/panicChan/ctx 上四选一等待返回。
func mapReduceWithPanicChan[T, U, V any](source <-chan T, panicChan *onceChan, mapper MapperFunc[T, U],
	reducer ReducerFunc[U, V], opts ...Option) (val V, err error) {
	options := buildOptions(opts...)
	// output is used to write the final result
	// reducer 写最终结果的通道(约定只写一次)。
	output := make(chan V)
	defer func() {
		// reducer can only write once, if more, panic
		// 防御:reducer 写了多于一个值 → 这里检测到并 panic,
		// 提前暴露使用错误(协议就是"最终值只有一个")。
		for range output {
			panic("more than one element written in reducer")
		}
	}()

	// collector is used to collect data from mapper, and consume in reducer
	// mapper → reducer 的缓冲通道(容量=并行数,削峰)。
	collector := make(chan U, options.workers)
	// if done is closed, all mappers and reducer should stop processing
	// done 关闭 = 全员停止的广播信号:内部临时创建,从不外部传入,
	// 随本次调用同生共死;唯一关闭点是 finish(见文件头第八节)。
	done := make(chan struct{})
	writer := newGuardedWriter(options.ctx, output, done)
	var closeOnce sync.Once
	// use atomic type to avoid data race
	// retErr 原子保存 cancel 传入的错误。
	var retErr errorx.AtomicError
	// finish 全流程收尾:只执行一次(close(done) + close(output))。
	finish := func() {
		closeOnce.Do(func() {
			close(done)
			close(output)
		})
	}
	// cancel 取消总线:唯一实例发放给主 goroutine/reducer/
	// 每个 mapper(见第九节);once 保证恰好生效一次、
	// 第一票的错误胜出。拉闸三步:记录错误 → 喝干 source
	// (放行卡住的生产者)→ finish 全员收尾。
	cancel := once(func(err error) {
		if err != nil {
			retErr.Set(err)
		} else {
			retErr.Set(ErrCancelWithNil)
		}

		drain(source)
		finish()
	})

	// 归约 goroutine:消费 collector,最终值写 output;
	// 结束(或 panic)后 drain collector 并触发收尾。
	go func() {
		defer func() {
			drain(collector)
			if r := recover(); r != nil {
				panicChan.write(buildPanicInfo(r, debug.Stack()))
			}
			finish()
		}()

		reducer(collector, writer, cancel)
	}()

	// mapper 调度器:并行消费 source,写入 collector。
	go executeMappers(mapperContext[T, U]{
		ctx: options.ctx,
		mapper: func(item T, w Writer[U]) {
			mapper(item, w, cancel)
		},
		source:    source,
		panicChan: panicChan,
		collector: collector,
		doneChan:  done,
		workers:   options.workers,
	})

	// 主 goroutine 四选一:ctx 取消 / panic / 拿到最终值。
	select {
	case <-options.ctx.Done():
		cancel(context.DeadlineExceeded)
		err = context.DeadlineExceeded
	case v := <-panicChan.channel:
		// drain output here, otherwise for loop panic in defer
		// 先喝干 output,避免 defer 里"多于一个值"的防御误触发。
		drain(output)
		panic(v)
	case v, ok := <-output:
		if e := retErr.Load(); e != nil {
			err = e
		} else if ok {
			val = v
		} else {
			err = ErrReduceNoOutput
		}
	}

	return
}

// newOptions 默认选项:Background ctx + 16 个并行 worker。
func newOptions() *mapReduceOptions {
	return &mapReduceOptions{
		ctx:     context.Background(),
		workers: defaultWorkers,
	}
}

// once 把一个 func(error) 包装成只生效一次的取消函数:
// 并发安全,第一票生效(其错误胜出),后来者静默丢弃。
func once(fn func(error)) func(error) {
	once := new(sync.Once)
	return func(err error) {
		once.Do(func() {
			fn(err)
		})
	}
}

// guardedWriter 带护栏的 writer:取消后 Write 变成空操作,
// 防止 mapper 向已关闭的 output/collector 发送导致 panic。
type guardedWriter[T any] struct {
	ctx     context.Context
	channel chan<- T
	done    <-chan struct{}
}

func newGuardedWriter[T any](ctx context.Context, channel chan<- T, done <-chan struct{}) guardedWriter[T] {
	return guardedWriter[T]{
		ctx:     ctx,
		channel: channel,
		done:    done,
	}
}

// Write 三选一:ctx 取消 / 已 done → 丢弃;否则写出。
// select 保证取消后永远不会阻塞在发送上。
func (gw guardedWriter[T]) Write(v T) {
	select {
	case <-gw.ctx.Done():
	case <-gw.done:
	default:
		gw.channel <- v
	}
}

// onceChan 只允许写一次的 channel:第一次写入生效,
// 后续写入静默丢弃 —— 保证"只有第一个 panic 被上报"。
type onceChan struct {
	channel chan any
	wrote   int32
}

// write CAS 抢写权:多个 goroutine 同时 panic 时只有第一个写入,
// 其余丢弃(避免向无消费的 channel 发送而卡死)。
func (oc *onceChan) write(val any) {
	if atomic.CompareAndSwapInt32(&oc.wrote, 0, 1) {
		oc.channel <- val
	}
}
