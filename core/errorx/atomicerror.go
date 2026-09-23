// ————————————————————————————————————————————————————————————————————————————
// atomicerror —— 原子错误(并发安全的单个错误槽位) —— 文件总结
//
// 用 atomic.Value 承载一个 error,多个 goroutine 并发写(后者覆盖
// 前者)/读都安全。典型用途:并发任务里"保存第一个/最新的失败
// 原因"(如 mapreduce 的 retErr)。
// 注意 Set 对 nil 是空操作 —— 错误槽位一旦有值就不会被 nil 清掉。
// ————————————————————————————————————————————————————————————————————————————
package errorx

import "sync/atomic"

// AtomicError defines an atomic error.
// 原子错误:底层 atomic.Value,存 error 接口值。
type AtomicError struct {
	err atomic.Value // error
}

// Set sets the error.
// 写入错误;nil 是空操作(不清空已有值)。
func (ae *AtomicError) Set(err error) {
	if err != nil {
		ae.err.Store(err)
	}
}

// Load returns the error.
// 读取错误;从未写入过返回 nil。
func (ae *AtomicError) Load() error {
	if v := ae.err.Load(); v != nil {
		return v.(error)
	}
	return nil
}
