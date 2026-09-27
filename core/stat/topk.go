// ————————————————————————————————————————————————————————————————————————————
// topk —— 小顶堆 Top-K:最大的 k 个元素 —— 文件总结
//
// 经典"容量 k 的小顶堆"解法:堆里始终维持当前见过的最大
// k 个,堆顶是它们中最小的(即"第 k 大的守门员");新元素
// 只有打得过守门员才入堆(弹掉堆顶换人)。单元素 O(log k),
// 总 O(n·log k),远好于全排序 O(n·log n)。
// 堆内元素无序,但 [0] 恒为第 k 大 —— metrics.go 正是靠
// 这一点逐层剥分位数:topK(全部,50%)[0] = 中位数。
// ————————————————————————————————————————————————————————————
package stat

import "container/heap"

// taskHeap 按 Duration 的小顶堆(container/heap 接口实现)。
type taskHeap []Task

func (h *taskHeap) Len() int {
	return len(*h)
}

// Less 决定堆序:小于 → 小顶堆,堆顶 = 堆内最小(守门员)。
func (h *taskHeap) Less(i, j int) bool {
	return (*h)[i].Duration < (*h)[j].Duration
}

func (h *taskHeap) Swap(i, j int) {
	(*h)[i], (*h)[j] = (*h)[j], (*h)[i]
}

func (h *taskHeap) Push(x any) {
	*h = append(*h, x.(Task))
}

func (h *taskHeap) Pop() any {
	old := *h
	n := len(old)
	x := old[n-1]
	*h = old[0 : n-1]
	return x
}

// topK 求出 all 中 Duration 最大的 k 个(堆内无序,
// 但 [0] 恒为第 k 大)。
func topK(all []Task, k int) []Task {
	h := new(taskHeap)
	heap.Init(h)

	for _, each := range all {
		if h.Len() < k {
			// 没满 k 个:先上车。
			heap.Push(h, each)
		} else if (*h)[0].Duration < each.Duration {
			// 打得过守门员(堆顶):弹掉堆顶,换人。
			heap.Pop(h)
			heap.Push(h, each)
		}
	}

	return *h
}
