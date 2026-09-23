// ————————————————————————————————————————————————————————————————————————————
// spinlock —— 自旋锁 —— 文件总结
//
// 与 sync.Mutex 的区别:拿不到锁时不休眠等待,而是原地循环重试
// (自旋),每轮循环 runtime.Gosched() 主动让出一次 CPU,
// 避免占着 CPU 空转饿死其他 goroutine。
//
// 适用场景:临界区极短、锁竞争不激烈的场合 —— 省去
// Mutex 的休眠/唤醒开销反而更快;临界区长或竞争激烈时
// 自旋会白白烧 CPU,请用 Mutex。
//
// 实现:1 个 uint32 + CAS,0=未锁、1=已锁;
// 注意:不是可重入锁,也不能被未持锁者 Unlock(无持有者校验)。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"runtime"
	"sync/atomic"
)

// A SpinLock is used as a lock a fast execution.
// 自旋锁:底层 uint32,0=未锁、1=已锁。
type SpinLock struct {
	lock uint32
}

// Lock locks the SpinLock.
// 加锁:不断尝试 CAS 抢锁,抢不到就 Gosched 让出 CPU 再试。
func (sl *SpinLock) Lock() {
	for !sl.TryLock() {
		// 主动让出一次执行权,进入下一轮调度再重试,
		// 避免空转烧 CPU、也让持锁者更快跑完临界区。
		runtime.Gosched()
	}
}

// TryLock tries to lock the SpinLock.
// 尝试加锁:CAS 从 0 改成 1,成功即拿到锁;非阻塞,立即返回。
func (sl *SpinLock) TryLock() bool {
	return atomic.CompareAndSwapUint32(&sl.lock, 0, 1)
}

// Unlock unlocks the SpinLock.
// 解锁:直接写回 0。
func (sl *SpinLock) Unlock() {
	atomic.StoreUint32(&sl.lock, 0)
}
