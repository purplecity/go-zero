// ————————————————————————————————————————————————————————————————————————————
// atomicbool —— 原子布尔值 —— 文件总结
//
// 标准库没有 atomic.Bool 的年代(go 1.19 之前)的自制原子布尔:
// 底层用 uint32(0=false, 1=true)+ atomic 操作实现。
// 新代码建议直接用标准库 atomic.Bool,本类型保留用于兼容
// 既有代码(如 logx.ExitOnFatal)。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync/atomic"

// An AtomicBool is an atomic implementation for boolean values.
// 原子布尔:底层 uint32,0=false、1=true;
// 直接对 *AtomicBool 做 (*uint32) 指针转换即可原子读写。
type AtomicBool uint32

// NewAtomicBool returns an AtomicBool.
// 创建一个初值为 false 的原子布尔。
func NewAtomicBool() *AtomicBool {
	return new(AtomicBool)
}

// ForAtomicBool returns an AtomicBool with given val.
// 创建一个指定初值的原子布尔。
func ForAtomicBool(val bool) *AtomicBool {
	b := NewAtomicBool()
	b.Set(val)
	return b
}

// CompareAndSwap compares current value with given old, if equals, set to given val.
// CAS:当前值等于 old 时才设置成 val,返回是否设置成功。
// bool → uint32 的转换在栈上完成,不影响原子性。
func (b *AtomicBool) CompareAndSwap(old, val bool) bool {
	var ov, nv uint32

	if old {
		ov = 1
	}
	if val {
		nv = 1
	}

	// 指针强转成 *uint32,复用标准库的原子 CAS。
	return atomic.CompareAndSwapUint32((*uint32)(b), ov, nv)
}

// Set sets the value to v.
// 原子写入。
func (b *AtomicBool) Set(v bool) {
	if v {
		atomic.StoreUint32((*uint32)(b), 1)
	} else {
		atomic.StoreUint32((*uint32)(b), 0)
	}
}

// True returns true if current value is true.
// 原子读取,判断当前是否为 true。
func (b *AtomicBool) True() bool {
	return atomic.LoadUint32((*uint32)(b)) == 1
}
