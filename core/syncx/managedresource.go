// ————————————————————————————————————————————————————————————————————————————
// managedresource —— 可标记失效并自动重建的资源 —— 文件总结
//
// 典型场景:一条网络连接(如 rpc 连接)。使用中发现它坏了,
// 调 MarkBroken 标记;之后任何人 Take 都会拿到新建的资源。
//
// 关键设计:MarkBroken 只在"坏掉的那个"确实是当前资源时才置空
// (equals 比较)—— 防止误删别人已经重建的新资源:
// A 拿到资源 v1 用坏了,期间资源已被重建为 v2,A 再 MarkBroken(v1)
// 时 v1 != v2,不置空,v2 得以保留。
// Take 是双重检查的惰性生成(读锁快路径 → 写锁 double check),
// 与 immutableresource 的区别:资源可被主动作废重生成,无失败重试概念。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import "sync"

// A ManagedResource is used to manage a resource that might be broken and refetched, like a connection.
// 可标记失效、自动重建的资源(如网络连接)。
type ManagedResource struct {
	// resource 当前资源(nil 表示待生成)。
	resource any
	// lock 读写锁:Take 快路径走读锁。
	lock sync.RWMutex
	// generate 资源的生成函数。
	generate func() any
	// equals 判断两个资源是否为同一个(用于防误删)。
	equals func(a, b any) bool
}

// NewManagedResource returns a ManagedResource.
// 创建资源管理器:generate 生成资源,equals 比较资源同一性。
func NewManagedResource(generate func() any, equals func(a, b any) bool) *ManagedResource {
	return &ManagedResource{
		generate: generate,
		equals:   equals,
	}
}

// MarkBroken marks the resource broken.
// 标记资源坏掉:仅当坏的资源确实是当前资源时才置空,
// 下次 Take 会重新 generate;若当前资源已被重建,则不动。
func (mr *ManagedResource) MarkBroken(resource any) {
	mr.lock.Lock()
	defer mr.lock.Unlock()

	if mr.equals(mr.resource, resource) {
		mr.resource = nil
	}
}

// Take takes the resource, if not loaded, generates it.
// 获取资源:读锁快路径直接返回;未加载则写锁双重检查后生成。
func (mr *ManagedResource) Take() any {
	mr.lock.RLock()
	resource := mr.resource
	mr.lock.RUnlock()

	if resource != nil {
		return resource
	}

	// 慢路径:升级写锁。
	mr.lock.Lock()
	defer mr.lock.Unlock()
	// maybe another Take() call already generated the resource.
	// 双重检查:等锁期间可能已被其他 Take 生成。
	if mr.resource == nil {
		mr.resource = mr.generate()
	}
	return mr.resource
}
