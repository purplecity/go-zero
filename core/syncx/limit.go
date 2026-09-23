// ————————————————————————————————————————————————————————————————————————————
// limit —— 并发数限制(信号量) —— 文件总结
//
// 经典的信号量实现:容量为 n 的 channel,Borrow 向里塞一个占位、
// Return 从里取一个占位 —— channel 满了 Borrow 自然阻塞,
// 从而把同时在跑的"借用者"限制在 n 个以内。
//
// 三种用法:
//
//	Borrow()    阻塞式借用(拿不到就等);
//	TryBorrow() 非阻塞尝试(拿不到立即返回 false);
//	Return()    归还;若没有借过却归还(channel 空),返回
//	            ErrLimitReturn —— 用于捕获"多次归还"的 bug。
//
// 底层被 timeoutlimit.go 的 TimeoutLimit 组合出"带超时的借用"。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"errors"

	"github.com/zeromicro/go-zero/core/lang"
)

// ErrLimitReturn indicates that the more than borrowed elements were returned.
// 归还数超过借出数(有人多次归还)时报错。
var ErrLimitReturn = errors.New("discarding limited token, resource pool is full, someone returned multiple times")

// Limit controls the concurrent requests.
// 并发限制器:pool 的容量就是允许的最大并发数。
// 注意方法集是值接收者(拷贝 Limit 也共享同一个 channel),可安全传值。
type Limit struct {
	pool chan lang.PlaceholderType
}

// NewLimit creates a Limit that can borrow n elements from it concurrently.
// 创建允许 n 个并发的限制器。
func NewLimit(n int) Limit {
	return Limit{
		pool: make(chan lang.PlaceholderType, n),
	}
}

// Borrow borrows an element from Limit in blocking mode.
// 阻塞式借用:池满时在此等待,直到有人归还。
func (l Limit) Borrow() {
	l.pool <- lang.Placeholder
}

// Return returns the borrowed resource, returns error only if returned more than borrowed.
// 归还:从池里取走一个占位;
// select+default 非阻塞探测:池是空的说明归还多于借出,报错。
func (l Limit) Return() error {
	select {
	case <-l.pool:
		return nil
	default:
		return ErrLimitReturn
	}
}

// TryBorrow tries to borrow an element from Limit, in non-blocking mode.
// If success, true returned, false for otherwise.
// 非阻塞尝试借用:池满立即返回 false,不等待。
func (l Limit) TryBorrow() bool {
	select {
	case l.pool <- lang.Placeholder:
		return true
	default:
		return false
	}
}
