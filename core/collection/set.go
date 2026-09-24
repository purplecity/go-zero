// ————————————————————————————————————————————————————————————————————————————
// set —— 泛型集合 —— 文件总结
//
// 用 map[T]lang.PlaceholderType 实现(零大小值),
// 语义同数学集合:去重、O(1) 查询。
// 注意:非并发安全,多 goroutine 使用需自行加锁
// (需要并发版可用 syncx/NewConcurrentMap 场景替代)。
// ————————————————————————————————————————————————————————————————————————————
package collection

import "github.com/zeromicro/go-zero/core/lang"

// Set is a type-safe generic set collection.
// It's not thread-safe, use with synchronization for concurrent access.
// 类型安全的泛型集合(非并发安全)。
type Set[T comparable] struct {
	// data 底层 map:值用零大小占位类型,只关心键。
	data map[T]lang.PlaceholderType
}

// NewSet returns a new type-safe set.
// 创建空集合。
func NewSet[T comparable]() *Set[T] {
	return &Set[T]{
		data: make(map[T]lang.PlaceholderType),
	}
}

// Add adds items to the set. Duplicates are automatically ignored.
// 添加一个或多个元素(重复自动忽略)。
func (s *Set[T]) Add(items ...T) {
	for _, item := range items {
		s.data[item] = lang.Placeholder
	}
}

// Clear removes all items from the set.
// 清空集合。
func (s *Set[T]) Clear() {
	clear(s.data)
}

// Contains checks if an item exists in the set.
// 是否包含某元素。
func (s *Set[T]) Contains(item T) bool {
	_, ok := s.data[item]
	return ok
}

// Count returns the number of items in the set.
// 元素个数。
func (s *Set[T]) Count() int {
	return len(s.data)
}

// Keys returns all elements in the set as a slice.
// 取出全部元素(顺序随机,同 map 遍历)。
func (s *Set[T]) Keys() []T {
	keys := make([]T, 0, len(s.data))
	for key := range s.data {
		keys = append(keys, key)
	}
	return keys
}

// Remove removes an item from the set.
// 删除元素(不存在时无操作)。
func (s *Set[T]) Remove(item T) {
	delete(s.data, item)
}
