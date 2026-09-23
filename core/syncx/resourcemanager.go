// ————————————————————————————————————————————————————————————————————————————
// resourcemanager —— 按名管理的一组资源 —— 文件总结
//
// 管理一组 "key → 可关闭资源"(如多个下游连接)的容器:
//
//	GetResource —— 按 key 取资源,没有则用 create 创建并缓存;
//	Inject      —— 外部直接注入资源(如测试注入 mock);
//	Close       —— 关闭全部资源并清空(之后不可再使用)。
//
// 防缓存击穿:GetResource 内部套了 SingleFlight —— 同一个 key
// 并发首建时只有一个 goroutine 真正执行 create,其余共享结果,
// 避免并发创建多份资源(如同时建多条相同连接)。
// Close 用 BatchError 聚合所有资源的关闭错误,不会因单个失败中断。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"io"
	"sync"

	"github.com/zeromicro/go-zero/core/errorx"
)

// A ResourceManager is a manager that used to manage resources.
// 资源管理器:map 存资源,singleFlight 防并发重复创建。
type ResourceManager struct {
	// resources key → 资源表(Close 后置为 nil,防 Close 后继续使用)。
	resources map[string]io.Closer
	// singleFlight 保证同 key 并发创建只有一个真正执行。
	singleFlight SingleFlight
	// lock 读写锁:查找走读锁,写入/关闭走写锁。
	lock sync.RWMutex
}

// NewResourceManager returns a ResourceManager.
// 创建空的管理器。
func NewResourceManager() *ResourceManager {
	return &ResourceManager{
		resources:    make(map[string]io.Closer),
		singleFlight: NewSingleFlight(),
	}
}

// Close closes the manager.
// Don't use the ResourceManager after Close() called.
// 关闭全部资源(逐个 Close,错误聚合返回),并清空资源表;
// 之后不要再使用本管理器。
func (manager *ResourceManager) Close() error {
	manager.lock.Lock()
	defer manager.lock.Unlock()

	var be errorx.BatchError
	for _, resource := range manager.resources {
		if err := resource.Close(); err != nil {
			be.Add(err)
		}
	}

	// release resources to avoid using it later
	// 置 nil 而不是重建 map:之后任何 GetResource 都会暴露问题。
	manager.resources = nil

	return be.Err()
}

// GetResource returns the resource associated with given key.
// 按 key 获取资源,不存在则调用 create 创建并缓存。
// 整个"查缓存/创建/写缓存"包在 SingleFlight 里:
// 同 key 并发调用只有一个真正执行,其余等待共享同一结果。
func (manager *ResourceManager) GetResource(key string, create func() (io.Closer, error)) (
	io.Closer, error) {
	val, err := manager.singleFlight.Do(key, func() (any, error) {
		// 先查缓存(读锁)。
		manager.lock.RLock()
		resource, ok := manager.resources[key]
		manager.lock.RUnlock()
		if ok {
			return resource, nil
		}

		// 缓存没有,创建;失败则不写缓存。
		resource, err := create()
		if err != nil {
			return nil, err
		}

		// 写缓存(写锁)。
		manager.lock.Lock()
		defer manager.lock.Unlock()
		manager.resources[key] = resource

		return resource, nil
	})
	if err != nil {
		return nil, err
	}

	return val.(io.Closer), nil
}

// Inject injects the resource associated with given key.
// 直接注入资源(覆盖同名 key),常用于测试或外部托管的资源。
func (manager *ResourceManager) Inject(key string, resource io.Closer) {
	manager.lock.Lock()
	manager.resources[key] = resource
	manager.lock.Unlock()
}
