// ————————————————————————————————————————————————————————————————————————————
// breakers —— 命名注册表:同名共享同一个断路器 —— 文件总结
//
// 包级 map[name]Breaker + 双检锁懒创建:同一下游(同名)的
// 所有调用共享熔断状态 —— 各处各自 New 的话,窗口被稀释,
// 谁也攒不够故障数,谁也熔不下去。
// 包级 Do* = GetBreaker(name) + 转发对应方法(一行胶水);
// NoBreakerFor(name) 往注册表塞 NopBreaker = 对该名关闭熔断。
// ————————————————————————————————————————————————————————————————————————————
package breaker

import (
	"context"
	"sync"
)

var (
	// lock 保护 breakers(读多写少,但 sync.Map 收益小,双检锁足够)。
	lock     sync.RWMutex
	breakers = make(map[string]Breaker)
)

// Do calls Breaker.Do on the Breaker with given name.
// 按名取断路器并执行(无 acceptable/无 fallback 的朴素形态)。
func Do(name string, req func() error) error {
	return do(name, func(b Breaker) error {
		return b.Do(req)
	})
}

// DoCtx calls Breaker.DoCtx on the Breaker with given name.
func DoCtx(ctx context.Context, name string, req func() error) error {
	return do(name, func(b Breaker) error {
		return b.DoCtx(ctx, req)
	})
}

// DoWithAcceptable calls Breaker.DoWithAcceptable on the Breaker with given name.
// 自定义"哪些错误算成功"。
func DoWithAcceptable(name string, req func() error, acceptable Acceptable) error {
	return do(name, func(b Breaker) error {
		return b.DoWithAcceptable(req, acceptable)
	})
}

// DoWithAcceptableCtx calls Breaker.DoWithAcceptableCtx on the Breaker with given name.
func DoWithAcceptableCtx(ctx context.Context, name string, req func() error,
	acceptable Acceptable) error {
	return do(name, func(b Breaker) error {
		return b.DoWithAcceptableCtx(ctx, req, acceptable)
	})
}

// DoWithFallback calls Breaker.DoWithFallback on the Breaker with given name.
// 自定义降级。
func DoWithFallback(name string, req func() error, fallback Fallback) error {
	return do(name, func(b Breaker) error {
		return b.DoWithFallback(req, fallback)
	})
}

// DoWithFallbackCtx calls Breaker.DoWithFallbackCtx on the Breaker with given name.
func DoWithFallbackCtx(ctx context.Context, name string, req func() error, fallback Fallback) error {
	return do(name, func(b Breaker) error {
		return b.DoWithFallbackCtx(ctx, req, fallback)
	})
}

// DoWithFallbackAcceptable calls Breaker.DoWithFallbackAcceptable on the Breaker with given name.
// 降级 + 可接受错误,两个扩展点全开。
func DoWithFallbackAcceptable(name string, req func() error, fallback Fallback,
	acceptable Acceptable) error {
	return do(name, func(b Breaker) error {
		return b.DoWithFallbackAcceptable(req, fallback, acceptable)
	})
}

// DoWithFallbackAcceptableCtx calls Breaker.DoWithFallbackAcceptableCtx on the Breaker with given name.
func DoWithFallbackAcceptableCtx(ctx context.Context, name string, req func() error,
	fallback Fallback, acceptable Acceptable) error {
	return do(name, func(b Breaker) error {
		return b.DoWithFallbackAcceptableCtx(ctx, req, fallback, acceptable)
	})
}

// GetBreaker returns the Breaker with the given name.
// 取(或首次创建)同名断路器:双检锁 —— 先读锁探,miss 后
// 上写锁再查一次(防并发下重复建),仍无才真正创建。
func GetBreaker(name string) Breaker {
	lock.RLock()
	b, ok := breakers[name]
	lock.RUnlock()
	if ok {
		return b
	}

	lock.Lock()
	b, ok = breakers[name]
	if !ok {
		b = NewBreaker(WithName(name))
		breakers[name] = b
	}
	lock.Unlock()

	return b
}

// NoBreakerFor disables the circuit breaker for the given name.
// 对该名字关闭熔断(塞空实现;已存在也会被覆盖)。
func NoBreakerFor(name string) {
	lock.Lock()
	breakers[name] = NopBreaker()
	lock.Unlock()
}

// do 统一胶水:按名取断路器,转发执行。
func do(name string, execute func(b Breaker) error) error {
	return execute(GetBreaker(name))
}
