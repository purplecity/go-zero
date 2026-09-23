// ————————————————————————————————————————————————————————————————————————————
// lockedcalls —— 同 key 串行执行(不共享结果) —— 文件总结
//
// 与 SingleFlight 一字之差:
//
//	LockedCalls:同 key 的调用"排队串行",每个都真正执行一遍;
//	SingleFlight:同 key 的并发调用"合并成一个",共享结果。
//
// 实现:map[key]*WaitGroup 做排队登记 ——
//
//	Do 进来先抢 mu:发现同 key 已有人在执行 → 记下它的 wg,
//	放锁、等它 Done,然后 goto begin 从头再来(重新排队竞争);
//	没人执行 → 自己登记 wg 并执行;结束时**先删 key 再 Done**
//	(顺序不能反:反了的话后来的 Do 会 Wait 到一个永远不会
//	Done 的 wg —— 因为 key 已删,它看到的 m 里没有,但这里
//	真正的原因是:先 Done 后删会让新调用者 Wait 到已完成的
//	wg,或者错过通知,时序上产生竞态)。
//
// 典型用途:同一 key 的写操作必须串行(如对同一文件的互斥写)。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync"

type (
	// LockedCalls makes sure the calls with the same key to be called sequentially.
	// For example, A called F, before it's done, B called F, then B's call would not blocked,
	// after A's call finished, B's call got executed.
	// The calls with the same key are independent, not sharing the returned values.
	// A ------->calls F with key and executes<------->returns
	// B ------------------>calls F with key<--------->executes<---->returns
	// 同 key 串行执行接口:每个调用都真正执行,只是排队,不共享结果。
	LockedCalls interface {
		Do(key string, fn func() (any, error)) (any, error)
	}

	// lockedGroup 实现:mu 保护 key → WaitGroup 的登记表。
	lockedGroup struct {
		mu sync.Mutex
		m  map[string]*sync.WaitGroup
	}
)

// NewLockedCalls returns a LockedCalls.
// 创建同 key 串行执行器。
func NewLockedCalls() LockedCalls {
	return &lockedGroup{
		m: make(map[string]*sync.WaitGroup),
	}
}

// Do 同 key 串行执行 fn:
// 有同 key 在执行 → 等它完成,再从头竞争(可能又排到别人后面);
// 没有人执行 → 自己登记并执行。
func (lg *lockedGroup) Do(key string, fn func() (any, error)) (any, error) {
begin:
	lg.mu.Lock()
	if wg, ok := lg.m[key]; ok {
		// 有同 key 在执行:放锁等它(持锁等待会死锁),
		// 等完 goto 重来 —— 重新抢登记资格。
		lg.mu.Unlock()
		wg.Wait()
		goto begin
	}

	return lg.makeCall(key, fn)
}

// makeCall 登记自己的 wg 并执行 fn,收尾时先删登记再 Done。
func (lg *lockedGroup) makeCall(key string, fn func() (any, error)) (any, error) {
	var wg sync.WaitGroup
	wg.Add(1)
	lg.m[key] = &wg
	lg.mu.Unlock()

	defer func() {
		// delete key first, done later. can't reverse the order, because if reverse,
		// another Do call might wg.Wait() without get notified with wg.Done()
		// 先删登记再 Done:保证"等某个 wg"的调用者都在本 key
		// 登记期间拿到的 wg,删除后新调用者会重新排队,不会
		// 等到一个已 Done 的 wg 而错过时序。
		lg.mu.Lock()
		delete(lg.m, key)
		lg.mu.Unlock()
		wg.Done()
	}()

	return fn()
}
