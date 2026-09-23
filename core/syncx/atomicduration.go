// ————————————————————————————————————————————————————————————————————————————
// atomicduration —— 原子时长 —— 文件总结
//
// time.Duration 的底层是 int64,因此直接用 int64 承载 +
// atomic.Int64 系列操作,即可实现无锁的原子时长读写。
// 典型用途:限频器的"上次放行时间"(logx 的 limitedExecutor)、
// 动态超时阈值等需要被多个 goroutine 读写的时间量。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"sync/atomic"
	"time"
)

// An AtomicDuration is an implementation of atomic duration.
// 原子时长:底层 int64,Duration 与 int64 可无损互转。
type AtomicDuration int64

// NewAtomicDuration returns an AtomicDuration.
// 创建一个初值为 0 的原子时长。
func NewAtomicDuration() *AtomicDuration {
	return new(AtomicDuration)
}

// ForAtomicDuration returns an AtomicDuration with given value.
// 创建一个指定初值的原子时长。
func ForAtomicDuration(val time.Duration) *AtomicDuration {
	d := NewAtomicDuration()
	d.Set(val)
	return d
}

// CompareAndSwap compares current value with old, if equals, set the value to val.
// CAS:当前值等于 old 时才设置成 val,返回是否成功。
func (d *AtomicDuration) CompareAndSwap(old, val time.Duration) bool {
	return atomic.CompareAndSwapInt64((*int64)(d), int64(old), int64(val))
}

// Load loads the current duration.
// 原子读取,int64 转回 time.Duration。
func (d *AtomicDuration) Load() time.Duration {
	return time.Duration(atomic.LoadInt64((*int64)(d)))
}

// Set sets the value to val.
// 原子写入,Duration 转成 int64 存储。
func (d *AtomicDuration) Set(val time.Duration) {
	atomic.StoreInt64((*int64)(d), int64(val))
}
