// ————————————————————————————————————————————————————————————————————————————
// timeoutlimit —— 带超时的并发限制 —— 文件总结
//
// Limit 只能阻塞等或立即失败,本类型补上第三种选择:
// 借不到就等,但最多等 timeout,等不到返回 ErrTimeout。
//
// 实现要点:Limit + Cond(条件变量)配合 ——
//
//	借用失败 → 在 cond 上带超时等待;有人 Return 时 Signal 唤醒;
//	唤醒后先 TryBorrow(可能被别人抢走),抢不到用剩余时间继续等,
//	剩余时间耗尽返回 ErrTimeout。for 循环是虚假唤醒的防御:
//	被唤醒不等于一定能借到,循环直到借到或超时。
//
// 典型用途:数据库/缓存连接池的"带超时借连接"。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"errors"
	"time"
)

// ErrTimeout is an error that indicates the borrow timeout.
// 借用超时错误。
var ErrTimeout = errors.New("borrow timeout")

// A TimeoutLimit is used to borrow with timeouts.
// 带超时的并发限制:Limit 负责计数,Cond 负责等待/唤醒。
type TimeoutLimit struct {
	limit Limit
	cond  *Cond
}

// NewTimeoutLimit returns a TimeoutLimit.
// 创建允许 n 个并发的带超时限制器。
func NewTimeoutLimit(n int) TimeoutLimit {
	return TimeoutLimit{
		limit: NewLimit(n),
		cond:  NewCond(),
	}
}

// Borrow borrows with given timeout.
// 带超时借用:先试一把;借不到就在 cond 上等待,
// 被唤醒后重试,剩余时间耗尽返回 ErrTimeout。
func (l TimeoutLimit) Borrow(timeout time.Duration) error {
	if l.TryBorrow() {
		return nil
	}

	var ok bool
	for {
		// 等待唤醒,拿回剩余可用时间;被唤醒不代表能借到,
		// 循环重试直到借到或超时(防御虚假唤醒)。
		timeout, ok = l.cond.WaitWithTimeout(timeout)
		if ok && l.TryBorrow() {
			return nil
		}

		if timeout <= 0 {
			return ErrTimeout
		}
	}
}

// Return returns a borrow.
// 归还:先还给 Limit,再 Signal 唤醒一个等待者。
// 注意顺序:先归还再唤醒,唤醒者才可能 TryBorrow 成功。
func (l TimeoutLimit) Return() error {
	if err := l.limit.Return(); err != nil {
		return err
	}

	l.cond.Signal()
	return nil
}

// TryBorrow tries a borrow.
// 非阻塞尝试借用,直接复用 Limit 的实现。
func (l TimeoutLimit) TryBorrow() bool {
	return l.limit.TryBorrow()
}
