// ————————————————————————————————————————————————————————————————————————————
// once —— 保证只执行一次 —— 文件总结
//
// 已废弃:直接用标准库 sync.OnceFunc(语义相同)。
// 保留此函数是为了兼容旧调用方。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync"

// Once returns a func that guarantees fn can only called once.
// Deprecated: use sync.OnceFunc instead.
// 返回一个包装函数:无论调用多少次,fn 只会真正执行一次
// (内部就是标准库 sync.OnceFunc 的直通)。
func Once(fn func()) func() {
	return sync.OnceFunc(fn)
}
