// ————————————————————————————————————————————————————————————————————————————
// ring —— 定长环形缓冲 —— 文件总结
//
// 只保留最近 n 个元素:超过容量后新元素覆盖最老的。
// 用途:取"最近 N 条"(如 fx.Stream.Tail)、滚动统计。
// index 防溢出技巧:涨到 2×容量就回卷一个容量,保证取模
// 永远为正且不随时间无限增长(溢出成负数会让索引错乱)。
// Add 写锁;Take 读锁(只读不改)。
// ————————————————————————————————————————————————————————————————————————————
package collection

import "sync"

// A Ring can be used as fixed size ring.
// 定长环形缓冲:新元素覆盖最老的。
type Ring struct {
	// elements 固定容量数组。
	elements []any
	// index 已写入总数(对容量取模即写入位置)。
	index int
	// lock 读写锁:Add 写、Take 读。
	lock sync.RWMutex
}

// NewRing returns a Ring object with the given size n.
// 创建容量 n 的环(n<1 panic)。
func NewRing(n int) *Ring {
	if n < 1 {
		panic("n should be greater than 0")
	}

	return &Ring{
		elements: make([]any, n),
	}
}

// Add adds v into r.
// 写入一个元素:落到 index%容量,覆盖最老的。
func (r *Ring) Add(v any) {
	r.lock.Lock()
	defer r.lock.Unlock()

	rlen := len(r.elements)
	r.elements[r.index%rlen] = v
	r.index++

	// prevent ring index overflow
	// index 回卷:超过 2 倍容量就减掉一个容量 —— 防止 int
	// 溢出(溢成负数后取模结果错乱),同时保持取模值不变。
	if r.index >= rlen<<1 {
		r.index -= rlen
	}
}

// Take takes all items from r.
// 取出当前全部元素(不删除),按写入顺序排列:
// 未满时是前 index 个;已满时从 index%容量(最老处)开始转一圈。
func (r *Ring) Take() []any {
	r.lock.RLock()
	defer r.lock.RUnlock()

	var size int
	var start int
	rlen := len(r.elements)

	if r.index > rlen {
		// 已转满一圈:取全部,start 是最老元素位置。
		size = rlen
		start = r.index % rlen
	} else {
		// 未满:取 index 个,从头开始。
		size = r.index
	}

	elements := make([]any, size)
	for i := 0; i < size; i++ {
		elements[i] = r.elements[(start+i)%rlen]
	}

	return elements
}
