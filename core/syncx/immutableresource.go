// ————————————————————————————————————————————————————————————————————————————
// immutableresource —— 可自动刷新的惰性单例资源 —— 文件总结
//
// 语义:资源(如配置、令牌)第一次 Get 时才真正 fetch;
// 之后直接返回缓存。特殊之处:**fetch 失败不永久缓存错误** ——
// 失败后过了 refreshInterval 会自动重试,成功则替换缓存。
//
// 并发模式是教科书式的双重检查(double-check):
//  1. 读锁快速路径:已加载直接返回(绝大多数请求走这里);
//  2. 升级写锁后再次检查(防止多个请求同时穿过读锁重复 fetch);
//  3. 之前失败过(err != nil)且未到刷新间隔 → 返回旧错误,不重试。
//
// 典型用途:接入第三方系统时获取/刷新访问令牌。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/timex"
)

// defaultRefreshInterval 失败后的默认重试间隔。
const defaultRefreshInterval = time.Second

type (
	// ImmutableResourceOption defines the method to customize an ImmutableResource.
	// 函数式选项(目前只有失败重试间隔)。
	ImmutableResourceOption func(resource *ImmutableResource)

	// An ImmutableResource is used to manage an immutable resource.
	// 惰性加载、失败自动重试的单例资源。
	ImmutableResource struct {
		// fetch 资源的加载函数(业务提供,如请求 token 接口)。
		fetch func() (any, error)
		// resource 加载成功的资源缓存(nil 表示尚未加载成功)。
		resource any
		// err 最近一次 fetch 的错误(成功后清零)。
		err error
		// lock 读写锁:读多写少,快路径走读锁。
		lock sync.RWMutex
		// refreshInterval 失败后的重试间隔。
		refreshInterval time.Duration
		// lastTime 上次 fetch 尝试时刻(原子,判断是否到重试时间)。
		lastTime *AtomicDuration
	}
)

// NewImmutableResource returns an ImmutableResource.
// 创建资源,fn 为加载函数;默认失败重试间隔 1 秒。
func NewImmutableResource(fn func() (any, error), opts ...ImmutableResourceOption) *ImmutableResource {
	// cannot use executors.LessExecutor because of cycle imports
	ir := ImmutableResource{
		fetch:           fn,
		refreshInterval: defaultRefreshInterval,
		lastTime:        NewAtomicDuration(),
	}
	for _, opt := range opts {
		opt(&ir)
	}
	return &ir
}

// Get gets the immutable resource, fetches automatically if not loaded.
// 获取资源:未加载时自动 fetch;失败后在刷新间隔内返回旧错误,
// 超过间隔自动重试,成功后缓存替换、错误清零。
func (ir *ImmutableResource) Get() (any, error) {
	// 快路径:读锁下已加载,直接返回(无写竞争,可高并发)。
	ir.lock.RLock()
	resource := ir.resource
	ir.lock.RUnlock()
	if resource != nil {
		return resource, nil
	}

	// 慢路径:升级为写锁。
	ir.lock.Lock()
	defer ir.lock.Unlock()

	// double check
	// 双重检查:等写锁期间可能已被别的请求加载成功。
	if ir.resource != nil {
		return ir.resource, nil
	}
	// 之前失败过,且还没到重试时间 → 直接返回旧错误,不浪费请求。
	if ir.err != nil && !ir.shouldRefresh() {
		return ir.resource, ir.err
	}

	res, err := ir.fetch()
	// 无论成败都记录尝试时刻,作为下次重试间隔的起点。
	ir.lastTime.Set(timex.Now())
	if err != nil {
		ir.err = err
		return nil, err
	}

	ir.resource, ir.err = res, nil
	return res, nil
}

// shouldRefresh 判断是否到了重试时间:
// 间隔 <=0 表示每次都重试;从未 fetch 过(lastTime==0)或已超过间隔则重试。
func (ir *ImmutableResource) shouldRefresh() bool {
	if ir.refreshInterval <= 0 {
		return true
	}

	lastTime := ir.lastTime.Load()
	return lastTime == 0 || lastTime+ir.refreshInterval < timex.Now()
}

// WithRefreshIntervalOnFailure sets refresh interval on failure.
// Set interval to 0 to enforce refresh every time if not succeeded, default is time.Second.
// 选项:设置失败后的重试间隔;0 表示每次都立即重试,默认 1 秒。
func WithRefreshIntervalOnFailure(interval time.Duration) ImmutableResourceOption {
	return func(resource *ImmutableResource) {
		resource.refreshInterval = interval
	}
}
