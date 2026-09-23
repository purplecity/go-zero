// ————————————————————————————————————————————————————————————————————————————
// atomicfloat64 —— 原子浮点数 —— 文件总结
//
// CPU 对浮点数的 CAS 没有原生指令,标准做法是"位模式"技巧:
// float64 ↔ uint64 用 math.Float64bits / Float64frombits 无损互转,
// 底层实际原子操作的是 uint64 位模式。
// Add 用 CAS 自旋(读旧值 → 加 → CAS,失败重试)实现,
// 是无锁浮点累加的标准写法。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"math"
	"sync/atomic"
)

// An AtomicFloat64 is an implementation of atomic float64.
// 原子浮点:底层 uint64 存位模式,读写前后做位模式转换。
type AtomicFloat64 uint64

// NewAtomicFloat64 returns an AtomicFloat64.
// 创建一个初值为 0 的原子浮点。
func NewAtomicFloat64() *AtomicFloat64 {
	return new(AtomicFloat64)
}

// ForAtomicFloat64 returns an AtomicFloat64 with given val.
// 创建一个指定初值的原子浮点。
func ForAtomicFloat64(val float64) *AtomicFloat64 {
	f := NewAtomicFloat64()
	f.Set(val)
	return f
}

// Add adds val to current value.
// 原子加法:CAS 自旋 —— 读旧值算出新值,CAS 提交;
// 失败说明期间被别人改过,重读重算重试,直到成功。
func (f *AtomicFloat64) Add(val float64) float64 {
	for {
		old := f.Load()
		nv := old + val
		if f.CompareAndSwap(old, nv) {
			return nv
		}
	}
}

// CompareAndSwap compares current value with old, if equals, set to given val.
// CAS:比较的是 float64 的位模式。
func (f *AtomicFloat64) CompareAndSwap(old, val float64) bool {
	return atomic.CompareAndSwapUint64((*uint64)(f), math.Float64bits(old), math.Float64bits(val))
}

// Load loads the current value.
// 原子读取:位模式转回 float64。
func (f *AtomicFloat64) Load() float64 {
	return math.Float64frombits(atomic.LoadUint64((*uint64)(f)))
}

// Set sets the current value to val.
// 原子写入:float64 转位模式存储。
func (f *AtomicFloat64) Set(val float64) {
	atomic.StoreUint64((*uint64)(f), math.Float64bits(val))
}
