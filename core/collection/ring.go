// ————————————————————————————————————————————————————————————————————————————
// ring —— 定长环形缓冲 —— 文件总结
//
// 只保留最近 n 个元素:超过容量后新元素覆盖最老的。
// 用途:取"最近 N 条"(如 fx.Stream.Tail)、滚动统计。
// index 防溢出技巧:index 是计数型(累计写入数,区别于
// timingwheel 那类位置型指针):位置靠 %容量 推导,满/未满
// 与元素数靠计数本身。涨到 2×容量就回卷一个容量 ——
// [容量,2×容量) 这段专用于表达"已满",减掉的又是模数的
// 整数倍,取模结果不变(溢出风险与边界验证详见 Add 注释)。
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
	// index 回卷:涨到 2×容量(rlen<<1)就一次性减掉一个容量,
	// 让 index 永远活在 [0, 2×rlen)。两个关键点:
	// 1) 减掉的恰是模数 rlen 的整数倍,index%rlen 分毫不变,
	//    对写位置完全隐形,等于没发生;
	// 2) 阈值必须是 2×rlen 而非 rlen:index 同时承担"位置"
	//    (%rlen)和"满/未满"(与 rlen 比较,见 Take)两个职责,
	//    [rlen,2×rlen) 这段专门表达"已满+当前位置" —— 若涨
	//    到 rlen 就回卷,"恰好写满一圈"会和"空环"混淆成同值。
	// 防溢出的现实性:int64 按百万次/秒写也要 ~292 年才溢,
	// 32 位平台(int32)约半小时;溢成负数后 % 得负索引、直接
	// 越界 panic —— 两行换这个保险,值。
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
