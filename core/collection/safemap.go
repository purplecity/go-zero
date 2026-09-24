// ————————————————————————————————————————————————————————————————————————————
// safemap —— 防 map 内存泄漏的并发 map(双 map 淘汰) —— 文件总结
//
// 背景:Go map 的 delete 只把桶位标记为 empty,不缩容,
// 大量增删后内存不降(golang/go#20135)。SafeMap 用
// dirtyOld/dirtyNew 两个 map 交替,删够阈值就把小的一方
// 复制合并、丢弃大骨架,让 runtime 回收旧 map 的内存:
//
//	写:Set 优先写 old(其删除计数未超限时);超限则写 new;
//	删:从 old 删则记 deletionOld,从 new 删记 deletionNew;
//	合并:某侧删满 maxDeletion 且另一侧够小(<1000 条)时,
//	      小的并进大的,重开一张空表 —— 旧表整体可回收。
//
// 历史包袱:Go 1.24 起官方 map 已内置清理机制,此实现
// 主要为兼容旧版本行为。
// ————————————————————————————————————————————————————————————————————————————
package collection

import (
	"maps"
	"sync"
)

const (
	// copyThreshold 合并时另一侧允许的最大条数(够小才值得复制)。
	copyThreshold = 1000
	// maxDeletion 触发合并的删除次数。
	maxDeletion = 10000
)

// SafeMap provides a map alternative to avoid memory leak.
// This implementation is not needed until issue below fixed.
// https://github.com/golang/go/issues/20135
// 并发安全 map:双表交替淘汰,防 delete 后内存不降。
type SafeMap struct {
	// lock 全局读写锁。
	lock sync.RWMutex
	// deletionOld/new 两张表各自的累计删除数(触发合并)。
	deletionOld int
	deletionNew int
	// dirtyOld 主表(读优先);dirtyNew 副表。
	dirtyOld map[any]any
	dirtyNew map[any]any
}

// NewSafeMap returns a SafeMap.
// 创建双表 map。
func NewSafeMap() *SafeMap {
	return &SafeMap{
		dirtyOld: make(map[any]any),
		dirtyNew: make(map[any]any),
	}
}

// Del deletes the value with the given key from m.
// 删除:先 old 后 new,删除数计入所在表;
// 任一侧删满 maxDeletion 且另一侧够小则合并重建,
// 让旧表骨架(可能巨大)整体被 GC。
func (m *SafeMap) Del(key any) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if _, ok := m.dirtyOld[key]; ok {
		delete(m.dirtyOld, key)
		m.deletionOld++
	} else if _, ok := m.dirtyNew[key]; ok {
		delete(m.dirtyNew, key)
		m.deletionNew++
	}
	// old 删够了:new 够小 → new 并进 old,old 变成新表重用;
	// 腾出的 dirtyNew 换成全新空表(旧骨架可回收)。
	if m.deletionOld >= maxDeletion && len(m.dirtyOld) < copyThreshold {
		maps.Copy(m.dirtyNew, m.dirtyOld)
		m.dirtyOld = m.dirtyNew
		m.deletionOld = m.deletionNew
		m.dirtyNew = make(map[any]any)
		m.deletionNew = 0
	}
	// new 删够了:同理反向合并。
	if m.deletionNew >= maxDeletion && len(m.dirtyNew) < copyThreshold {
		maps.Copy(m.dirtyOld, m.dirtyNew)
		m.dirtyNew = make(map[any]any)
		m.deletionNew = 0
	}
}

// Get gets the value with the given key from m.
// 查询:先 old 后 new。
func (m *SafeMap) Get(key any) (any, bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	if val, ok := m.dirtyOld[key]; ok {
		return val, true
	}

	val, ok := m.dirtyNew[key]
	return val, ok
}

// Range calls f sequentially for each key and value present in the map.
// If f returns false, range stops the iteration.
// 遍历两张表;f 返回 false 提前停止。
func (m *SafeMap) Range(f func(key, val any) bool) {
	m.lock.RLock()
	defer m.lock.RUnlock()

	for k, v := range m.dirtyOld {
		if !f(k, v) {
			return
		}
	}
	for k, v := range m.dirtyNew {
		if !f(k, v) {
			return
		}
	}
}

// Set sets the value into m with the given key.
// 写入:old 未删够时优先写 old(覆盖 new 里的同 key 时
// 顺带删 new 并计数);old 删够了则反向,写 new。
func (m *SafeMap) Set(key, value any) {
	m.lock.Lock()
	defer m.lock.Unlock()

	if m.deletionOld <= maxDeletion {
		if _, ok := m.dirtyNew[key]; ok {
			delete(m.dirtyNew, key)
			m.deletionNew++
		}
		m.dirtyOld[key] = value
	} else {
		if _, ok := m.dirtyOld[key]; ok {
			delete(m.dirtyOld, key)
			m.deletionOld++
		}
		m.dirtyNew[key] = value
	}
}

// Size returns the size of m.
// 两张表条数之和。
func (m *SafeMap) Size() int {
	m.lock.RLock()
	size := len(m.dirtyOld) + len(m.dirtyNew)
	m.lock.RUnlock()
	return size
}
