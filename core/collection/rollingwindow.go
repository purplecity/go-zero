// ————————————————————————————————————————————————————————————————————————————
// rollingwindow —— 时间滚动窗口(环形桶) —— 文件总结
//
// N 个固定时长的桶排成环,时间每过一个 interval 前进一步:
// 滑过的桶被清空重用。窗口 = 最近 N 个 interval 的累计。
// go-zero 里负载脱落(passCounter/rtCounter)、断路器统计
// 都基于它。
//
// 惰性推进:没有 Add 时不动指针;下次 Add 时按距上次
// 过了几个 interval(span)一次性清空跳过的桶并对齐。
// lastTime 对齐到 interval 边界(减去余数),保证 span
// 计算不受写入了时刻的毫秒偏差影响。
//
// Reduce 遍历有效桶做聚合;IgnoreCurrentBucket 选项可跳过
// 当前桶 —— 它只写了一部分,统计会低估(如脱落器用它
// 避免误判能力不足而错杀请求)。
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
		// offset 当前桶下标。
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
// 聚合遍历有效桶(时间从旧到新);span>0 时先推进窗口。
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
