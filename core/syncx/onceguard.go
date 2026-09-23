// ————————————————————————————————————————————————————————————————————————————
// onceguard —— 一次性资源守卫 —— 文件总结
//
// 用一个 uint32 + CAS 实现"只能占用一次"的守卫:
//
//	Take  —— CAS(0→1):第一个调用者抢占成功返回 true,
//	         之后所有调用者都返回 false;
//	Taken —— 查询当前是否已被占用。
//
// 与 Once(保证函数只执行一次)不同,OnceGuard 表达的是
// "资源/资格只能被领取一次",抢占结果由调用方自行处理,
// 典型场景:定时任务的首次触发、一次性初始化资格的争抢。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync/atomic"

// An OnceGuard is used to make sure a resource can be taken once.
// 一次性守卫:done 为 0 表示未占用,1 表示已占用。
type OnceGuard struct {
	done uint32
}

// Taken checks if the resource is taken.
// 查询资源是否已被占用。
func (og *OnceGuard) Taken() bool {
	return atomic.LoadUint32(&og.done) == 1
}

// Take takes the resource, returns true on success, false for otherwise.
// 抢占资源:CAS 从 0 改成 1,并发下只有一个调用者返回 true。
func (og *OnceGuard) Take() bool {
	return atomic.CompareAndSwapUint32(&og.done, 0, 1)
}
