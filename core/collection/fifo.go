// ————————————————————————————————————————————————————————————————————————————
// fifo —— 定长起步、可增长的 FIFO 队列 —— 文件总结
//
// 切片实现的循环队列:head/tail 双指针 + 取模环转,
// 内存连续、无链表节点分配。满了不丢,而是扩容:
//
//	容量 < 256:翻倍;
//	容量 ≥ 256:向 1.25 倍过渡(类似 slice 的增长曲线,
//	大容量下避免翻倍浪费)。
//
// 扩容时把环上元素按 head→tail 顺序复制回新数组开头,
// 重排 head=0。Take 取走元素后置 nil,帮 GC 释放引用。
// ————————————————————————————————————————————————————————————————————————————
package collection

import "sync"

// queueGrowThreshold 扩容曲线的分界容量(256 以下翻倍)。
const queueGrowThreshold = 256

// A Queue is a FIFO queue.
// 先进先出队列(并发安全)。
type Queue struct {
	// lock 全局互斥锁。
	lock sync.Mutex
	// elements 环形底层数组。
	elements []any
	// head 队头(取出端)下标。
	head int
	// tail 队尾(写入端)下标。
	tail int
	// count 当前元素数。
	count int
}

// NewQueue returns a Queue object.
// 创建初始容量 size 的队列(size<1 panic)。
func NewQueue(size int) *Queue {
	if size < 1 {
		panic("size must be greater than 0")
	}

	return &Queue{
		elements: make([]any, size),
	}
}

// Empty checks if q is empty.
// 是否为空。
func (q *Queue) Empty() bool {
	q.lock.Lock()
	empty := q.count == 0
	q.lock.Unlock()

	return empty
}

// Put puts element into q at the last position.
// 入队:满则先扩容(环上元素按序复制回新数组开头),
// 再写 tail 并前移。
func (q *Queue) Put(element any) {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.count == len(q.elements) {
		// 扩容:把 head 起的环上元素按顺序复制到新数组开头。
		nodes := make([]any, nextQueueCapacity(len(q.elements)))
		n := copy(nodes, q.elements[q.head:])
		copy(nodes[n:], q.elements[:q.head])
		q.head = 0
		q.tail = q.count
		q.elements = nodes
	}

	q.elements[q.tail] = element
	q.tail = (q.tail + 1) % len(q.elements)
	q.count++
}

// Take takes the first element out of q if not empty.
// 出队:取 head 元素;置 nil 断开引用(帮 GC),
// 前移 head。空返回 (nil, false)。
func (q *Queue) Take() (any, bool) {
	q.lock.Lock()
	defer q.lock.Unlock()

	if q.count == 0 {
		return nil, false
	}

	element := q.elements[q.head]
	q.elements[q.head] = nil // 防止底层数组长期持有已出队对象的引用
	q.head = (q.head + 1) % len(q.elements)
	q.count--

	return element, true
}

// nextQueueCapacity 计算扩容后的容量:
// 小队列翻倍,大队列向 1.25 倍平滑过渡(控内存浪费)。
func nextQueueCapacity(capacity int) int {
	if capacity < queueGrowThreshold {
		return capacity << 1
	}

	// Use a growth curve similar to Go slices: double small queues, then
	// transition smoothly toward 1.25x growth for larger queues.
	// cap + (cap + 3×256)/4 ≈ 1.25×cap + 192,平滑过渡到 1.25 倍。
	return capacity + ((capacity + 3*queueGrowThreshold) >> 2)
}
