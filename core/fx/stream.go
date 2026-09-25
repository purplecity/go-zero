// ————————————————————————————————————————————————————————————————————————————
// stream —— 基于 channel 的流式处理(仿 Rx 风格) —— 文件总结
//
// 核心模型:Stream 就是一个 <-chan any,每个算子起 goroutine
// 把上游 channel 变换成新的 channel,链起来就是流水线。
//
// 三类算子:
//  1. 惰性变换(返回 Stream):Walk/Map/Filter/Distinct/Skip/
//     Head/Tail/Split/Buffer/Concat/Group/Sort/Reverse/Merge ——
//     只是搭管道,不消费;真正的搬运发生在下游开始读时;
//  2. 终结求值(返回值):Reduce/First/Last/Count/Max/Min/
//     AllMatch/AnyMatch/NoneMatch —— 消费流并得出结果;
//  3. 终结副作用:ForEach/ForAll/Parallel/Done。
//
// 三个反复出现的关键细节:
//
//	A. 提前返回必须 drain:First/AnyMatch 等短路算子返回前
//	   `go drain(s.source)` —— 上游还阻塞在发送上,不喝干
//	   生产者 goroutine 就泄漏(同 mr 包的 drain 哲学);
//	B. 并发度:Walk 系(含 Map/Filter/Parallel)默认 16 个
//	   worker(pool 信号量限流),UnlimitedWorkers 可放开;
//	C. close 的合法性判据:唯一要求是"【不会再有发送】",
//	   与消费状态无关 —— 关闭后缓冲值照常逐个可收(每个
//	   ok=true),取干后零值+ok=false,故先关后读(Just)、
//	   边读边关都安全;唯一禁区是关了还有人发(panic)。
//	   多写者须先 Wait 确认全部退出才能关(见 Concat)。
//
// ————————————————————————————————————————————————————————————————————————————
package fx

import (
	"sort"
	"sync"

	"github.com/zeromicro/go-zero/core/collection"
	"github.com/zeromicro/go-zero/core/lang"
	"github.com/zeromicro/go-zero/core/threading"
)

const (
	// defaultWorkers 默认并发 worker 数。
	defaultWorkers = 16
	// minWorkers worker 数下限。
	minWorkers = 1
)

type (
	// rxOptions 流选项:并发 worker 配置。
	rxOptions struct {
		// unlimitedWorkers 不限并发(每元素一个 goroutine)。
		unlimitedWorkers bool
		// workers 并发上限(unlimited 生效时作缓冲大小)。
		workers int
	}

	// FilterFunc defines the method to filter a Stream.
	// 过滤谓词。
	FilterFunc func(item any) bool
	// ForAllFunc defines the method to handle all elements in a Stream.
	// 整体消费回调(直接拿底层 channel)。
	ForAllFunc func(pipe <-chan any)
	// ForEachFunc defines the method to handle each element in a Stream.
	// 逐元素回调。
	ForEachFunc func(item any)
	// GenerateFunc defines the method to send elements into a Stream.
	// 生产者回调:向 source 写元素。
	GenerateFunc func(source chan<- any)
	// KeyFunc defines the method to generate keys for the elements in a Stream.
	// 取键(去重/分组用)。
	KeyFunc func(item any) any
	// LessFunc defines the method to compare the elements in a Stream.
	// 比较函数(排序/最值用)。
	LessFunc func(a, b any) bool
	// MapFunc defines the method to map each element to another object in a Stream.
	// 映射函数(1:1)。
	MapFunc func(item any) any
	// Option defines the method to customize a Stream.
	// 流选项。
	Option func(opts *rxOptions)
	// ParallelFunc defines the method to handle elements parallelly.
	// 并发处理回调。
	ParallelFunc func(item any)
	// ReduceFunc defines the method to reduce all the elements in a Stream.
	// 归约回调(直接消费底层 channel)。
	ReduceFunc func(pipe <-chan any) (any, error)
	// WalkFunc defines the method to walk through all the elements in a Stream.
	// 遍历回调:可向 pipe 写 0~n 个元素(1:n 变换)。
	WalkFunc func(item any, pipe chan<- any)

	// A Stream is a stream that can be used to do stream processing.
	// 流:本质是只读 channel + 一组算子。
	Stream struct {
		// source 底层元素通道。
		source <-chan any
	}
)

// Concat returns a concatenated Stream.
// 包级便捷方法:拼接多条流。
func Concat(s Stream, others ...Stream) Stream {
	return s.Concat(others...)
}

// From constructs a Stream from the given GenerateFunc.
// 从生成函数建流:goroutine 里跑 generate,结束关闭 source。
func From(generate GenerateFunc) Stream {
	source := make(chan any)

	threading.GoSafe(func() {
		defer close(source)
		generate(source)
	})

	return Range(source)
}

// Just converts the given arbitrary items to a Stream.
// 把已有元素变成流:急切版生产者 —— 数据全在手上,不需要
// goroutine,当前 goroutine 同步灌入后【提前 close】再交出
// 读端(对照 From 的惰性 goroutine 生产)。
//
//	缓冲必须恰好 len(items):再小,灌入会在无人消费时阻塞
//	死锁(尚未 return,读端还不存在);这是"灌入永不阻塞"
//	的最小值,再大只是白占内存。空参数则缓冲 0、立即关,
//	得到空流。
//
//	先 close 后消费为何安全 —— close 的合法性唯一判据是
//	"不会再有发送",与消费无关:关闭 ≠ 作废数据,缓冲里的
//	值照样逐个收到(每个 ok=true),取干后才零值+ok=false;
//	for range 的退出条件正是"已关闭且已取干"。Sort/Reverse/
//	Merge 收完全量后重发射,复用的正是本函数。
func Just(items ...any) Stream {
	// 缓冲恰好装下全部元素:灌入永不阻塞的最小值。
	source := make(chan any, len(items))
	for _, item := range items {
		source <- item
	}
	// 发送已全部完成,"不会再有发送"成立 —— 此刻关闭永远安全。
	close(source)

	return Range(source)
}

// Range converts the given channel to a Stream.
// 把现成 channel 包装成流。
func Range(source <-chan any) Stream {
	return Stream{
		source: source,
	}
}

// AllMatch returns whether all elements of this stream match the provided predicate.
// May not evaluate the predicate on all elements if not necessary for determining the result.
// If the stream is empty then true is returned and the predicate is not evaluated.
// 是否全部满足:遇到第一个不满足即短路返回 false。
func (s Stream) AllMatch(predicate func(item any) bool) bool {
	for item := range s.source {
		if !predicate(item) {
			// make sure the former goroutine not block, and current func returns fast.
			// 短路返回前喝干上游,放行还卡在发送上的生产者。
			go drain(s.source)
			return false
		}
	}

	return true
}

// AnyMatch returns whether any elements of this stream match the provided predicate.
// May not evaluate the predicate on all elements if not necessary for determining the result.
// If the stream is empty then false is returned and the predicate is not evaluated.
// 是否存在满足:遇到第一个满足即短路返回 true。
func (s Stream) AnyMatch(predicate func(item any) bool) bool {
	for item := range s.source {
		if predicate(item) {
			// make sure the former goroutine not block, and current func returns fast.
			// 同上:短路前 drain。
			go drain(s.source)
			return true
		}
	}

	return false
}

// Buffer buffers the items into a queue with size n.
// It can balance the producer and the consumer if their processing throughput don't match.
// 加缓冲:在上下游之间插一个容量 n 的队列,削峰填谷
// (生产快消费慢时平滑吞吐)。
func (s Stream) Buffer(n int) Stream {
	if n < 0 {
		n = 0
	}

	source := make(chan any, n)
	go func() {
		for item := range s.source {
			source <- item
		}
		close(source)
	}()

	return Range(source)
}

// Concat returns a Stream that concatenated other streams
// 合流(名字叫拼接,实际是并发合流,顺序不保证):本流与
// others 各起一个转发 goroutine 同时搬运,汇入同一条 channel。
//
// 读写端模式:返回的 Stream 只是【读端】,goroutine 是
// 【写端】,二者共享同一个 channel 完成接驳 —— 这是本文件
// 所有惰性算子的统一结构,Concat 的特殊之处仅是写端有多个。
//
// 两个要点:
//
//	顺序不保证 —— 所有转发器同时起跑(group.Run 不阻塞),
//	抢同一无缓冲 channel 的握手权,输出交错且每次运行可能
//	不同(实测 Just(1..5) 合流 Just(6..10) 可得
//	[6 1 7 8 9 2 3 4 5 10]);要顺序就得串行化
//	(先等前一条流搬完再启动后一条)。
//
//	close 时机 = Wait(所有写者退出)—— 多写者共享一个
//	channel,必须等全部写完才能 close(同 mr 包 wg.Wait 后
//	close(collector) 的手法,早 close 即 send on closed);
//	而"全部写完"能等价于"全部被消费",靠的是无缓冲的同步
//	握手:转发器退出前,最后一笔发送必须已被下游接走。
//	记法:Wait 管安全 close,无缓冲管"转发完成⟹消费完成"。
func (s Stream) Concat(others ...Stream) Stream {
	source := make(chan any)

	go func() {
		group := threading.NewRoutineGroup()
		// 转发器 1:本流的元素搬入 source(立刻开跑,不等别人)。
		group.Run(func() {
			for item := range s.source {
				source <- item
			}
		})

		for _, each := range others {
			each := each // 捕获各自流的循环变量(Go1.22 前必需)
			// 转发器 2..N:其余流的元素也同时搬入 —— 交错由此而来。
			group.Run(func() {
				for item := range each.source {
					source <- item
				}
			})
		}

		// 全部上游搬完才能关下游。
		group.Wait()
		close(source)
	}()

	return Range(source)
}

// Count counts the number of elements in the result.
// 数元素个数(必须消费完)。
func (s Stream) Count() (count int) {
	for range s.source {
		count++
	}
	return
}

// Distinct removes the duplicated items based on the given KeyFunc.
// 按键去重:map 记住见过的 key,首次出现的才放行。
func (s Stream) Distinct(fn KeyFunc) Stream {
	source := make(chan any)

	threading.GoSafe(func() {
		defer close(source)

		// 用 map[any]Placeholder 当集合(零大小值类型)。
		keys := make(map[any]lang.PlaceholderType)
		for item := range s.source {
			key := fn(item)
			if _, ok := keys[key]; !ok {
				source <- item
				keys[key] = lang.Placeholder
			}
		}
	})

	return Range(source)
}

// Done waits all upstreaming operations to be done.
// 喝干流:不关心内容,只等上游全部结束
// (不想消费但又不想泄漏生产者时用)。
func (s Stream) Done() {
	drain(s.source)
}

// Filter filters the items by the given FilterFunc.
// 过滤:谓词为 true 的才进入下游(Walk 的 0/1 特例)。
func (s Stream) Filter(fn FilterFunc, opts ...Option) Stream {
	return s.Walk(func(item any, pipe chan<- any) {
		if fn(item) {
			pipe <- item
		}
	}, opts...)
}

// First returns the first item, nil if no items.
// 取第一个元素:拿到即短路,drain 放行上游。
func (s Stream) First() any {
	for item := range s.source {
		// make sure the former goroutine not block, and current func returns fast.
		// 短路前喝干上游。
		go drain(s.source)
		return item
	}

	return nil
}

// ForAll handles the streaming elements from the source and no later streams.
// 整体消费:把底层 channel 直接交给 fn;
// fn 若没消费完就返回,补一个 drain 防上游泄漏。
func (s Stream) ForAll(fn ForAllFunc) {
	fn(s.source)
	// avoid goroutine leak on fn not consuming all items.
	// 防调用方没读完导致生产者卡死。
	go drain(s.source)
}

// ForEach seals the Stream with the ForEachFunc on each item, no successive operations.
// 逐元素消费(终结算子):读完全部元素。
func (s Stream) ForEach(fn ForEachFunc) {
	for item := range s.source {
		fn(item)
	}
}

// Group groups the elements into different groups based on their keys.
// 按键分组:先全部按 key 聚成 []any,再把每个组作为
// 一个元素发出(下游收到的是 []any)。
func (s Stream) Group(fn KeyFunc) Stream {
	// 先同步消费完(分组必须看全量数据)。
	groups := make(map[any][]any)
	for item := range s.source {
		key := fn(item)
		groups[key] = append(groups[key], item)
	}

	// 再把各组懒发射出去。
	source := make(chan any)
	go func() {
		for _, group := range groups {
			source <- group
		}
		close(source)
	}()

	return Range(source)
}

// Head returns the first n elements in p.
// 取前 n 个。注意这里同时操作【两条】channel,容易看混
// (名字只差两个字符):source 是新建的下游管道,s.source
// 是上游。n==0 时刻起,两件事并行推进:
//
//	对下游 = "提前打烊"的店主:close(source) 让等在下游的
//	  消费者立刻看到"已关闭且取干",它们的 for range 退出、
//	  后继算子 ASAP 开工(close 只影响等在这条 channel 上
//	  的人,与上游循环无关 —— 循环退出条件是 s.source 被
//	  【上游生产者】关闭且取干);
//	对上游 = "陪跑到关门"的最后一位客人:循环【不会停】,
//	  继续收上游元素但全部丢弃(n 已为负,两个 if 都不命中),
//	  直到上游关闭取干才自然退出 —— 这就是 drain 的化身,
//	  显式 drain(s.source) 与循环自弃等价,都是防止上游
//	  生产者卡死在发送上(break 会 → goroutine 泄漏)。
func (s Stream) Head(n int64) Stream {
	if n < 1 {
		panic("n must be greater than 0")
	}

	source := make(chan any)

	go func() {
		for item := range s.source {
			n--
			if n >= 0 {
				source <- item
			}
			if n == 0 {
				// let successive method go ASAP even we have more items to skip
				// 角色 1(对下游):提前打烊 —— 后继算子立即知道"没有了"。
				close(source)
				// why we don't just break the loop, and drain to consume all items.
				// because if breaks, this former goroutine will block forever,
				// which will cause goroutine leak.
				// 角色 2(对上游):陪跑到关门 —— 不 break 而是 drain,
				// break 会让上游生产者永久阻塞 → goroutine 泄漏。
				drain(s.source)
			}
		}
		// not enough items in s.source, but we need to let successive method to go ASAP.
		// 元素不足 n(循环耗尽上游而 n 未到 0):
		// 全部转发后也要关闭下游,别让后继干等。
		if n > 0 {
			close(source)
		}
	}()

	return Range(source)
}

// Last returns the last item, or nil if no items.
// 取最后一个元素(必须读完整个流)。
func (s Stream) Last() (item any) {
	for item = range s.source {
	}
	return
}

// Map converts each item to another corresponding item, which means it's a 1:1 model.
// 映射(1:1):每个元素变换后进入下游(Walk 的 1:1 特例)。
func (s Stream) Map(fn MapFunc, opts ...Option) Stream {
	return s.Walk(func(item any, pipe chan<- any) {
		pipe <- fn(item)
	}, opts...)
}

// Max returns the maximum item from the underlying source.
// 取最大元素(按 less 比较)。
func (s Stream) Max(less LessFunc) any {
	var max any
	for item := range s.source {
		if max == nil || less(max, item) {
			max = item
		}
	}

	return max
}

// Merge merges all the items into a slice and generates a new stream.
// 全量收进一个切片,再作为单元素流发出。
func (s Stream) Merge() Stream {
	var items []any
	for item := range s.source {
		items = append(items, item)
	}

	source := make(chan any, 1)
	source <- items
	close(source)

	return Range(source)
}

// Min returns the minimum item from the underlying source.
// 取最小元素(按 less 比较)。
func (s Stream) Min(less LessFunc) any {
	var min any
	for item := range s.source {
		if min == nil || less(item, min) {
			min = item
		}
	}

	return min
}

// NoneMatch returns whether all elements of this stream don't match the provided predicate.
// May not evaluate the predicate on all elements if not necessary for determining the result.
// If the stream is empty then true is returned and the predicate is not evaluated.
// 是否全部不满足:遇到第一个满足即短路返回 false。
func (s Stream) NoneMatch(predicate func(item any) bool) bool {
	for item := range s.source {
		if predicate(item) {
			// make sure the former goroutine not block, and current func returns fast.
			// 短路前 drain。
			go drain(s.source)
			return false
		}
	}

	return true
}

// Parallel applies the given ParallelFunc to each item concurrently with given number of workers.
// 并发处理每个元素(Walk 后喝干):Map 无返回版的并发终结算子。
func (s Stream) Parallel(fn ParallelFunc, opts ...Option) {
	s.Walk(func(item any, pipe chan<- any) {
		fn(item)
	}, opts...).Done()
}

// Reduce is a utility method to let the caller deal with the underlying channel.
// 归约:把底层 channel 交给调用方自己消费(最大自由度)。
func (s Stream) Reduce(fn ReduceFunc) (any, error) {
	return fn(s.source)
}

// Reverse reverses the elements in the stream.
// 反转:全量收集后首尾对调,再以 Just 重新发射 —— 流式反转
// 必须先看到最后一个元素才知道谁排第一,故与 Sort/Merge 同类,
// 只能全量收集。
func (s Stream) Reverse() Stream {
	var items []any
	for item := range s.source {
		items = append(items, item)
	}
	// reverse, official method
	// 首尾对调反转切片。正确性:i 取遍前半区 [0, n/2),
	// 镜像位 n-1-i 恰取遍后半区,每对恰好交换一次;
	// 奇数长度时中位元素被整数除法(len/2 向下取整)自然排除。
	// Go 1.21+ 可直接用标准库 slices.Reverse 替换本循环
	// (与它逐位等价,n=0..9 已对照验证);保留手写仅为与
	// 上游保持一致,属 Go 1.21 之前的历史写法。
	for i := len(items)/2 - 1; i >= 0; i-- {
		opp := len(items) - 1 - i
		items[i], items[opp] = items[opp], items[i]
	}

	return Just(items...)
}

// Skip returns a Stream that skips size elements.
// 跳过前 n 个,其余透传。
func (s Stream) Skip(n int64) Stream {
	if n < 0 {
		panic("n must not be negative")
	}
	if n == 0 {
		return s
	}

	source := make(chan any)

	go func() {
		for item := range s.source {
			n--
			if n >= 0 {
				continue
			} else {
				source <- item
			}
		}
		close(source)
	}()

	return Range(source)
}

// Sort sorts the items from the underlying source.
// 排序:全量收集 → sort.Slice → Just 重发
// (排序注定要看到全部数据,无法流式)。
func (s Stream) Sort(less LessFunc) Stream {
	var items []any
	for item := range s.source {
		items = append(items, item)
	}
	sort.Slice(items, func(i, j int) bool {
		return less(items[i], items[j])
	})

	return Just(items...)
}

// Split splits the elements into chunk with size up to n,
// might be less than n on tailing elements.
// 分块:每凑满 n 个元素打包成 []any 发出,末尾不足 n 的
// 残块也发出(下游收到的是切片)。
func (s Stream) Split(n int) Stream {
	if n < 1 {
		panic("n should be greater than 0")
	}

	source := make(chan any)
	go func() {
		var chunk []any
		for item := range s.source {
			chunk = append(chunk, item)
			if len(chunk) == n {
				source <- chunk
				chunk = nil
			}
		}
		// 不足 n 的尾巴也发出去。
		if chunk != nil {
			source <- chunk
		}
		close(source)
	}()

	return Range(source)
}

// Tail returns the last n elements in p.
// 取最后 n 个:Ring 环形缓冲只保留最近 n 个,
// 读完流后把环里的元素发出(无需全量保存)。
func (s Stream) Tail(n int64) Stream {
	if n < 1 {
		panic("n should be greater than 0")
	}

	source := make(chan any)

	go func() {
		ring := collection.NewRing(int(n))
		for item := range s.source {
			ring.Add(item)
		}
		for _, item := range ring.Take() {
			source <- item
		}
		close(source)
	}()

	return Range(source)
}

// Walk lets the callers handle each item, the caller may write zero, one or more items based on the given item.
// 遍历变换(最通用算子):fn 对每个元素可向 pipe 写 0~n 个
// 元素,Map/Filter/Parallel 都是它的特例;可选并发度。
func (s Stream) Walk(fn WalkFunc, opts ...Option) Stream {
	option := buildOptions(opts...)
	if option.unlimitedWorkers {
		return s.walkUnlimited(fn, option)
	}

	return s.walkLimited(fn, option)
}

// walkLimited 限并发版:pool 信号量(容量 workers)限制
// 在飞 goroutine 数;全部跑完才关 pipe。
func (s Stream) walkLimited(fn WalkFunc, option *rxOptions) Stream {
	pipe := make(chan any, option.workers)

	go func() {
		var wg sync.WaitGroup
		pool := make(chan lang.PlaceholderType, option.workers)

		for item := range s.source {
			pool <- lang.Placeholder // 占一个 worker 名额(满则等)
			wg.Add(1)

			// better to safely run caller defined method
			// GoSafe:fn panic 不打崩进程。
			threading.GoSafe(func() {
				defer func() {
					wg.Done()
					<-pool // 归还名额
				}()

				fn(item, pipe)
			})
		}

		// 等全部在飞 worker 结束再关下游。
		wg.Wait()
		close(pipe)
	}()

	return Range(pipe)
}

// walkUnlimited 不限并发版:每个元素一个 goroutine
// (元素量大时慎用)。
func (s Stream) walkUnlimited(fn WalkFunc, option *rxOptions) Stream {
	pipe := make(chan any, option.workers)

	go func() {
		var wg sync.WaitGroup

		for item := range s.source {
			wg.Add(1)
			// better to safely run caller defined method
			// GoSafe 防单元素 panic 打崩进程。
			threading.GoSafe(func() {
				defer wg.Done()
				fn(item, pipe)
			})
		}

		wg.Wait()
		close(pipe)
	}()

	return Range(pipe)
}

// UnlimitedWorkers lets the caller use as many workers as the tasks.
// 选项:不限并发(每元素一个 goroutine)。
func UnlimitedWorkers() Option {
	return func(opts *rxOptions) {
		opts.unlimitedWorkers = true
	}
}

// WithWorkers lets the caller customize the concurrent workers.
// 选项:并发 worker 数(下限 1)。
func WithWorkers(workers int) Option {
	return func(opts *rxOptions) {
		if workers < minWorkers {
			opts.workers = minWorkers
		} else {
			opts.workers = workers
		}
	}
}

// buildOptions returns a rxOptions with given customizations.
// 从默认选项开始依次应用选项。
func buildOptions(opts ...Option) *rxOptions {
	options := newOptions()
	for _, opt := range opts {
		opt(options)
	}

	return options
}

// drain drains the given channel.
// 喝干 channel:丢弃剩余全部元素直到关闭
// (短路/弃流时放行上游生产者,防 goroutine 泄漏)。
func drain(channel <-chan any) {
	for range channel {
	}
}

// newOptions returns a default rxOptions.
// 默认选项:16 个 worker。
func newOptions() *rxOptions {
	return &rxOptions{
		workers: defaultWorkers,
	}
}
