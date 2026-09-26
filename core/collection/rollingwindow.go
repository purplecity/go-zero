// ————————————————————————————————————————————————————————————————————————————
// rollingwindow —— 时间滚动窗口(环形桶) —— 文件总结
//
// 【列车模型】size 个桶 = 环形轨道上的 size 个站,时间 =
// 列车,每站停靠 interval,停靠期间发生的事件(Add)记在
// 当前停靠站;列车绕一圈(W = size×interval)回到同一站,
// 上圈留下的账已超出"最近一圈",擦掉重记。窗口 = 列车刚
// 驶过的那一整圈,即"最近 W 秒"。没有任何东西在物理上
// "滑动" —— 动的是"最近 N 秒"这条统计口径线,列车(时间)
// 是它唯一的动力;熔断器因此能自愈:故障停止后,其事件随
// 列车转圈被淘汰,错误率自然回落。
//
// 【核心一句话】每过一个 interval,淘汰最老的一段;因为
// 是环,新一段恰好落在被淘汰的那格上 —— 淘汰与新增共用
// 同一个座位,擦干净接着记(updateOffset 清 offset+1、
// 指针前移一格,一个动作的两半)。环上没有固定的"最老
// 编号":最老永远是列车身后那站(offset+1),刚记完的
// offset 桶还要在窗口里被统计 (size-1) 个 interval,等
// 下一圈列车回来才轮到擦。相邻两次统计因此重叠
// (size-1)/size(区别于整窗跳档、无重叠的"翻页窗口"),
// 精度 = interval(步进一格)。go-zero 里负载脱落
// (passCounter/rtCounter)、断路器统计(10s×40 桶)都基于它。
//
// 【下标方向 = 时间反方向】沿环正走一格,时间回退一个
// interval:offset 是现在,offset+1 是窗口末尾(最老),
// offset+2 倒数第二……直到 offset-1(次新)兜回 offset
// (当前)。所以 Reduce 起点 (offset+span+1) 正是最老的
// 有效桶,沿环正走一圈,时间自然"从旧到新";反着读则是在
// 回放最近的历史(offset-1 上一格、offset-2 上上格…)。
//
// 【惰性推进】没人记账时列车照跑(时间不停),但不动指针;
// 下次 Add 按距上次过了几个 interval(span)一次性补擦
// 途中各站上圈的旧账并对齐。lastTime 对齐到 interval 边界
// (减去余数),保证 span 计算不受写入时刻毫秒偏差影响
// (否则偏差会累积漂移)。
//
// 【Reduce】遍历有效桶做聚合(旧→新);本函数不推进也不
// 清桶 —— 过期桶只是被排除在遍历之外,清零留给下次 Add。
// IgnoreCurrentBucket 选项可跳过当前桶 —— 它只写了一部分,
// 统计会低估(如脱落器用它避免误判能力不足而错杀请求)。
// ————————————————————————————————————————————————————————————————————————————
package collection

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/mathx"
	"github.com/zeromicro/go-zero/core/timex"
)

type (
	// BucketInterface is the interface that defines the buckets.
	// 桶接口:可累加、可清零(自定义桶实现它接入窗口)。
	BucketInterface[T Numerical] interface {
		Add(v T)
		Reset()
	}

	// Numerical is the interface that restricts the numerical type.
	// 数值类型约束(复用 mathx.Numerical)。
	Numerical = mathx.Numerical

	// RollingWindowOption let callers customize the RollingWindow.
	// 窗口选项。
	RollingWindowOption[T Numerical, B BucketInterface[T]] func(rollingWindow *RollingWindow[T, B])

	// RollingWindow defines a rolling window to calculate the events in buckets with the time interval.
	// 滚动窗口:size 个桶 × interval 时长。
	RollingWindow[T Numerical, B BucketInterface[T]] struct {
		// lock Add 写 / Reduce 读。
		lock sync.RWMutex
		// size 桶数。
		size int
		// win 桶数组。
		win *window[T, B]
		// interval 单桶时长。
		interval time.Duration
		// offset 当前桶下标:永远是"最新位"(正在记账的当前桶)。
		// 环上没有固定编号的最老桶 —— 最老永远是 offset+1(列车
		// 身后那站,上一圈旧账所在;首圈时为空位)。指针停摆 span
		// 格后,最老的【有效】桶是 offset+span+1(见 Reduce 起点)。
		offset int
		// ignoreCurrent Reduce 是否跳过当前桶(未写满,防低估)。
		ignoreCurrent bool
		// lastTime start time of the last bucket 当前桶起始时刻
		//(对齐到 interval 边界)。
		lastTime time.Duration
	}
)

// NewRollingWindow returns a RollingWindow that with size buckets and time interval,
// use opts to customize the RollingWindow.
// 创建滚动窗口:newBucket 决定桶的聚合结构。
func NewRollingWindow[T Numerical, B BucketInterface[T]](newBucket func() B, size int,
	interval time.Duration, opts ...RollingWindowOption[T, B]) *RollingWindow[T, B] {
	if size < 1 {
		panic("size must be greater than 0")
	}

	w := &RollingWindow[T, B]{
		size:     size,
		win:      newWindow[T, B](newBucket, size),
		interval: interval,
		lastTime: timex.Now(),
	}
	for _, opt := range opts {
		opt(w)
	}
	return w
}

// Add adds value to current bucket.
// 累加一个值到当前桶(先惰性推进窗口再写)。
func (rw *RollingWindow[T, B]) Add(v T) {
	rw.lock.Lock()
	defer rw.lock.Unlock()
	rw.updateOffset()
	rw.win.add(rw.offset, v)
}

// Reduce runs fn on all buckets, ignore current bucket if ignoreCurrent was set.
// 聚合遍历有效桶(时间从旧到新)。注意:本函数不推进也不清桶
// —— 过期桶只是被排除在遍历之外;清零留给下次 Add 的
// updateOffset 顺手做(读锁下本就不该改写)。起点
// (offset+span+1) = 虚拟指针(offset+span)身后一站,即最老
// 的有效桶 —— 下标往前一格、时间回退一格(见文件头【下标
// 方向】),沿环正走自然旧→新。
//
// 例:size=4、offset=0(桶0=[T₀,T₀+1))、span=3(现在
// T₀+3.x,三秒没 Add)。被排除的桶 1/2/3 装的是【上一圈】
// 的旧账 [T₀−3,T₀),已在窗口 [T₀,T₀+4) 之外;而窗口内的
// [T₀+1,T₀+4) 这三秒没人记过账、本就无账。故起点
// (0+3+1)%4=0、diff=4−3=1,只查桶 0 —— "最近 4 秒只有
// 第一秒有数据"就是正确答案;若把 1/2/3 也查了,等于拿
// 3~4 秒前的旧账冒充现状(updateOffset 来 Add 时清的也正是
// 这三个桶)。
func (rw *RollingWindow[T, B]) Reduce(fn func(b B)) {
	rw.lock.RLock()
	defer rw.lock.RUnlock()

	var diff int
	span := rw.span()
	// ignore the current bucket, because of partial data
	// 当前桶只写了半截,统计会低估 —— 可选跳过。
	if span == 0 && rw.ignoreCurrent {
		diff = rw.size - 1
	} else {
		diff = rw.size - span
	}
	if diff > 0 {
		// 从最旧的【完整】桶开始(当前桶的下一个)。
		offset := (rw.offset + span + 1) % rw.size
		rw.win.reduce(offset, diff, fn)
	}
}

// span 距上次推进过了几个 interval:
// 超过一个完整窗口(空闲太久)则返回 size(整窗失效)。
func (rw *RollingWindow[T, B]) span() int {
	offset := int(timex.Since(rw.lastTime) / rw.interval)
	if 0 <= offset && offset < rw.size {
		return offset
	}

	return rw.size
}

// updateOffset 惰性推进:清空跳过的 span 个桶,指针前移,
// lastTime 对齐到 interval 边界(去掉毫秒余数,
// 保证下次 span 计算基于整齐的桶边界)。
func (rw *RollingWindow[T, B]) updateOffset() {
	span := rw.span()
	if span <= 0 {
		return
	}

	offset := rw.offset
	// reset expired buckets
	// 清空被时间跳过的桶(它们的数据已过期)。
	for i := 0; i < span; i++ {
		rw.win.resetBucket((offset + i + 1) % rw.size)
	}

	rw.offset = (offset + span) % rw.size
	now := timex.Now()
	// align to interval time boundary
	// 对齐桶边界:lastTime 减去不足一个 interval 的余数。
	rw.lastTime = now - (now-rw.lastTime)%rw.interval
}

// Bucket defines the bucket that holds sum and num of additions.
// 内置桶:累计和 + 次数(可求平均,如响应时间)。
type Bucket[T Numerical] struct {
	// Sum 累加值总和。
	Sum T
	// Count 累加次数。
	Count int64
}

// Add 累加一个值。
func (b *Bucket[T]) Add(v T) {
	b.Sum += v
	b.Count++
}

// Reset 清零(桶被时间轮转重用时调用)。
func (b *Bucket[T]) Reset() {
	b.Sum = 0
	b.Count = 0
}

// window 桶容器:裸数组 + 取模环转(下标由外层管理)。
type window[T Numerical, B BucketInterface[T]] struct {
	// buckets 桶数组。
	buckets []B
	// size 桶数。
	size int
}

// newWindow 建 size 个桶(newBucket 决定桶类型)。
func newWindow[T Numerical, B BucketInterface[T]](newBucket func() B, size int) *window[T, B] {
	buckets := make([]B, size)
	for i := 0; i < size; i++ {
		buckets[i] = newBucket()
	}
	return &window[T, B]{
		buckets: buckets,
		size:    size,
	}
}

// add 向指定桶累加。
func (w *window[T, B]) add(offset int, v T) {
	w.buckets[offset%w.size].Add(v)
}

// reduce 从 start 起按环遍历 count 个桶执行 fn。
func (w *window[T, B]) reduce(start, count int, fn func(b B)) {
	for i := 0; i < count; i++ {
		fn(w.buckets[(start+i)%w.size])
	}
}

// resetBucket 清零指定桶。
func (w *window[T, B]) resetBucket(offset int) {
	w.buckets[offset%w.size].Reset()
}

// IgnoreCurrentBucket lets the Reduce call ignore current bucket.
// 选项:Reduce 跳过当前未写满的桶。
func IgnoreCurrentBucket[T Numerical, B BucketInterface[T]]() RollingWindowOption[T, B] {
	return func(w *RollingWindow[T, B]) {
		w.ignoreCurrent = true
	}
}
