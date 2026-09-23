// ————————————————————————————————————————————————————————————————————————————
// singleflight —— 同 key 并发调用合并,共享结果 —— 文件总结
//
// 解决"缓存击穿":某个 key 过期/未加载的瞬间,成百上千个请求
// 同时去回源(打垮数据库)。SingleFlight 保证同一个 key 并发
// 只有第一个真正执行 fn,其余等待并共享它的结果。
//
// 与 LockedCalls 的区别(一字之差):
//
//	SingleFlight:同 key 合并成一次执行,大家共享结果(依赖型);
//	LockedCalls:同 key 逐个串行执行,各有各的结果(独立型)。
//
// 实现:calls: map[key]*call,call 内含 WaitGroup + 结果。
// createCall:同 key 已存在 → Wait 等待,done=true;
// 不存在 → 登记 call 并负责执行。makeCall 结束时先删 key
// 再 wg.Done(顺序反了会让等待者拿到已完成的 call 却错过结果)。
// DoEx 额外返回 fresh 标志:自己是执行者(fresh)还是共享者。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync"

type (
	// SingleFlight lets the concurrent calls with the same key to share the call result.
	// For example, A called F, before it's done, B called F. Then B would not execute F,
	// and shared the result returned by F which called by A.
	// The calls with the same key are dependent, concurrent calls share the returned values.
	// A ------->calls F with key<------------------->returns val
	// B --------------------->calls F with key------>returns val
	// 同 key 合并执行接口:并发调用共享第一个执行者的结果。
	SingleFlight interface {
		// Do 执行 fn:同 key 已有执行中的调用则等待并共享其结果。
		Do(key string, fn func() (any, error)) (any, error)
		// DoEx 额外返回 fresh:true 表示自己是执行者,
		// false 表示是等待共享者(用于区分"亲手算的"和"搭车的")。
		DoEx(key string, fn func() (any, error)) (any, bool, error)
	}

	// call 一次执行的代表:wg 用于等待者挂起,val/err 共享结果。
	call struct {
		wg  sync.WaitGroup
		val any
		err error
	}

	// flightGroup 实现:calls 登记表 + 保护它的互斥锁。
	flightGroup struct {
		calls map[string]*call
		lock  sync.Mutex
	}
)

// NewSingleFlight returns a SingleFlight.
// 创建合并执行器。
func NewSingleFlight() SingleFlight {
	return &flightGroup{
		calls: make(map[string]*call),
	}
}

// Do 同 key 合并执行:done=true 表示搭了别人的车(等待共享)。
func (g *flightGroup) Do(key string, fn func() (any, error)) (any, error) {
	c, done := g.createCall(key)
	if done {
		// 已经有同 key 在执行:结果已被共享进 c,直接读。
		return c.val, c.err
	}

	g.makeCall(c, key, fn)
	return c.val, c.err
}

// DoEx 同 Do,额外返回 fresh:true=自己是执行者,false=共享者。
func (g *flightGroup) DoEx(key string, fn func() (any, error)) (val any, fresh bool, err error) {
	c, done := g.createCall(key)
	if done {
		return c.val, false, c.err
	}

	g.makeCall(c, key, fn)
	return c.val, true, c.err
}

// createCall 查/登记调用:
// 同 key 已在执行 → Wait 等待其完成,返回 done=true(共享者);
// 否则登记新 call 并返回 done=false(自己是执行者)。
func (g *flightGroup) createCall(key string) (c *call, done bool) {
	g.lock.Lock()
	if c, ok := g.calls[key]; ok {
		g.lock.Unlock()
		// 挂起等待执行者 makeCall 里的 wg.Done 唤醒。
		c.wg.Wait()
		return c, true
	}

	c = new(call)
	c.wg.Add(1)
	g.calls[key] = c
	g.lock.Unlock()

	return c, false
}

// makeCall 执行者路径:执行 fn 写入共享的 c,收尾先删登记再 Done。
func (g *flightGroup) makeCall(c *call, key string, fn func() (any, error)) {
	defer func() {
		// 顺序很重要:先删 key 再 Done。反过来的话,
		// 等待者被唤醒后看到 key 还在,可能读到未写入完成的
		// 结果,或新的调用者错误地等待一个已结束的 call。
		g.lock.Lock()
		delete(g.calls, key)
		g.lock.Unlock()
		c.wg.Done()
	}()

	c.val, c.err = fn()
}
