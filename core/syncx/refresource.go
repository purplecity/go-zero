// ————————————————————————————————————————————————————————————————————————————
// refresource —— 引用计数资源 —— 文件总结
//
// 语义:Use 使引用计数 +1,Clean 使 -1;计数归零时触发一次 clean
// 回调(真正的资源销毁/释放)。Clean 幂等:重复调用直接返回。
// 资源已被清理后再 Use 会返回 ErrUseOfCleaned,
// 防止"关闭后还在使用"的悬空引用。
//
// 适用场景:同一个资源(如一条连接、一个缓存对象)被多方共享,
// 只有等所有使用方都 Clean 后才能真正释放。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"errors"
	"sync"
)

// ErrUseOfCleaned is an error that indicates using a cleaned resource.
// 资源已被清理后仍尝试使用时报错。
var ErrUseOfCleaned = errors.New("using a cleaned resource")

// A RefResource is used to reference counting a resource.
// 引用计数资源:lock 保护 ref/cleaned 的复合状态变更。
type RefResource struct {
	lock sync.Mutex
	// ref 引用计数,初始为 0(先 Clean 把它减到 -1 以下不成立,
	// 所以调用约定是 Use/Clean 严格配对)。
	ref int32
	// cleaned 是否已触发过清理(幂等标记)。
	cleaned bool
	// clean 引用归零时的清理回调(如断开连接)。
	clean func()
}

// NewRefResource returns a RefResource.
// 创建引用计数资源,clean 为计数归零时的清理回调。
func NewRefResource(clean func()) *RefResource {
	return &RefResource{
		clean: clean,
	}
}

// Use uses the resource with reference count incremented.
// 使用资源:引用计数 +1;已清理的资源不允许再用。
func (r *RefResource) Use() error {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.cleaned {
		return ErrUseOfCleaned
	}

	r.ref++
	return nil
}

// Clean cleans a resource with reference count decremented.
// 释放一次使用:计数 -1,减到 0 时标记已清理并触发 clean 回调。
// 幂等:重复 Clean 直接返回,不会重复执行 clean。
func (r *RefResource) Clean() {
	r.lock.Lock()
	defer r.lock.Unlock()

	if r.cleaned {
		return
	}

	r.ref--
	if r.ref == 0 {
		r.cleaned = true
		r.clean()
	}
}
