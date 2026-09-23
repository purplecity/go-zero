// ————————————————————————————————————————————————————————————————————————————
// donechan —— 可安全多次关闭的完成通道 —— 文件总结
//
// 裸 channel 的痛点:close 一个已经 close 的 channel 会 panic。
// DoneChan 用 sync.Once 把 close 包装成幂等操作 —— 多个组件
// 都可以放心调用 Close 发出退出信号(典型场景:服务的优雅退出、
// 多个 producer 通知同一个 consumer 停止)。
//
// 用法:
//
//	dc := syncx.NewDoneChan()
//	<-dc.Done()   // 阻塞等待关闭信号
//	dc.Close()    // 任意次数调用都安全,只有第一次真正关闭
//
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"sync"

	"github.com/zeromicro/go-zero/core/lang"
)

// A DoneChan is used as a channel that can be closed multiple times and wait for done.
// 可多次关闭的完成通道:close 幂等由 once 保证。
type DoneChan struct {
	// done 通知用的 channel(零容量,关闭即广播)。
	done chan lang.PlaceholderType
	// once 保证 close 只真正执行一次,重复 Close 不 panic。
	once sync.Once
}

// NewDoneChan returns a DoneChan.
// 创建一个未关闭的 DoneChan。
func NewDoneChan() *DoneChan {
	return &DoneChan{
		done: make(chan lang.PlaceholderType),
	}
}

// Close closes dc, it's safe to close more than once.
// 关闭通道(广播给所有等待者);sync.Once 保证只关一次,
// 重复调用是安全的空操作 —— 解决裸 channel 二次 close panic 的问题。
func (dc *DoneChan) Close() {
	dc.once.Do(func() {
		close(dc.done)
	})
}

// Done returns a channel that can be notified on dc closed.
// 返回底层通道,调用方用 <-Done() 阻塞等待关闭信号。
func (dc *DoneChan) Done() chan lang.PlaceholderType {
	return dc.done
}
