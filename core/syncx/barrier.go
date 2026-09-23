// ————————————————————————————————————————————————————————————————————————————
// barrier —— 资源访问屏障 —— 文件总结
//
// 把"对某个资源的互斥访问"封装成一句话:
//
//	barrier.Guard(func() { /* 操作共享资源 */ })
//
// 等价于 加锁 → 执行 → 解锁 的样板代码。
//
// Guard(包级函数)接受任意 sync.Locker(实现了 Lock/Unlock 的都行:
// Mutex、RWMutex 等),Barrier 只是自带一把 Mutex 的便捷形态。
// 没有魔法,纯粹是消除锁样板代码的语法糖。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync"

// A Barrier is used to facility the barrier on a resource.
// 资源屏障:内置一把锁,Guard 即"锁内执行"。
type Barrier struct {
	lock sync.Mutex
}

// Guard guards the given fn on the resource.
// 持有内置锁执行 fn(与其它 Guard 调用互斥)。
func (b *Barrier) Guard(fn func()) {
	Guard(&b.lock, fn)
}

// Guard guards the given fn with lock.
// 用给定锁保护 fn:加锁 → 执行 → defer 解锁(panic 也安全)。
func Guard(lock sync.Locker, fn func()) {
	lock.Lock()
	defer lock.Unlock()
	fn()
}
