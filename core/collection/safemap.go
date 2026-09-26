// ————————————————————————————————————————————————————————————————————————————
// safemap —— 防 map 内存泄漏的并发 map(双 map 淘汰) —— 文件总结
//
// 背景(golang/go#20135):map 的内存分两层 —— 条目数据
// 与容器骨架(按历史峰值分配的桶数组)。delete 只把槽位
// 清零打"空"标记,骨架一根毛不动;且 map 仍引用着骨架,
// GC 视其为"被占用的空容器"而非垃圾,不收。实测(go1.27):
// 500 万条峰值 144MB,删剩 1000 条仍是 144MB —— 内存跟
// 历史峰值走,不跟存活条目走。
//
// SafeMap 的解法:map 无法从外部缩小,那就整表丢弃(搬家
// 弃楼,不是拆楼)—— dirtyOld/dirtyNew 两张表交替,删够
// 阈值且幸存者够少时,把存活条目拷进另一张表、指针改指
// 过去,旧的大骨架整体沦为无人引用的垃圾,GC 整块收走:
//
//	写:Set 优先写 old(其删除计数未超限时);超限则写 new;
//	删:从 old 删则记 deletionOld,从 new 删记 deletionNew;
//	合并:某侧累计删满 maxDeletion(1 万)且该侧幸存 <1000
//	      条(拷贝便宜才值得)时,该侧幸存者并入另一侧、该
//	      侧重开空表 —— 被掏空侧的旧骨架整体可回收。
//
// 现状核查(2026-09):Go 1.24 换 Swiss Table 后有部分改进
// —— 删到一条不剩时子表会被释放;但"删剩一点"的部分删除
// 场景骨架仍整套保留,#20135 未修复(官方口径:需要时自行
// 重建 map)。timingwheel 的 timers 任务常生灭、永不全空,
// 正中此要害,此实现仍有价值。
// 另:Go 1.24 起 sync.Map 默认改为 HashTrieMap 实现,按路径
// 复制更新、被删条目的旧节点可随 GC 回收,多核并发读写性能
// 显著优于锁方案(含本实现);新场景可评估直接用 sync.Map。
// 注意:本仓库 go.mod 已是 go 1.25,所有用户均可用新实现。
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
