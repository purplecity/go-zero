// ————————————————————————————————————————————————————————————————————————————
// signals+polyfill —— 信号处理的 Windows 占位实现 —— 文件总结
//
// Windows 没有 SIGUSR1/SIGUSR2/SIGTERM(Unix 专属信号),真实现
// 无从谈起;本文件用 build tag(windows)与 signals.go
// (linux||darwin||freebsd)互斥编译,任何平台恰好编译一份 ——
// 业务代码跨平台 import 本包都能编译通过。
//
// 文件名里的 "+polyfill" 只是给人看的约定("+"对编译器无特殊
// 含义,平台选择只认 //go:build):polyfill 即"垫片"——平台缺
// 能力时补一个行为等价的占位。包内同模式配对还有
// profile/profile+polyfill、shutdown/shutdown+polyfill 两组。
//
// Done() 返回 context.Background().Done(),严格说是 nil channel。
// 接收方其实有四态,nil 是第四态 —— 接收永远阻塞,select 里
// 永不就绪,与"永不关闭的 channel"可观察行为完全一致:
//   open+空+无发送方   阻塞 → 不就绪(有 default 则走 default)
//   open+有值          立刻拿走(每份只能一次)
//   closed             永远立刻返回零值 → 永远就绪(见 signals.go
//                     的 stopOnSignal,正是靠这一态做幂等)
//   nil                永远阻塞 → 永不就绪(本文件的用法)
// 语义即"Windows 下没有优雅退出通知",调用方 select 它永远等不到。
// ————————————————————————————————————————————————————————————————————————————
//go:build windows

package proc

import "context"

// Done returns the channel that notifies the process quitting.
// Windows 下返回永不关闭的 channel(context.Background().Done())。
func Done() <-chan struct{} {
	return context.Background().Done()
}
