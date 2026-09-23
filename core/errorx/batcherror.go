// ————————————————————————————————————————————————————————————————————————————
// batcherror —— 聚合多个错误的批错误 —— 文件总结
//
// 场景:并行/批量操作(如关闭 100 个连接)中,单个失败不该中断
// 整批,把错误逐个 Add 进来,最后 Err() 聚合成一个 error 返回。
//
//	Add   —— 收集非 nil 错误(线程安全);
//	Err   —— errors.Join 聚合;没有任何错误时返回 nil,
//	         因此 `if err := be.Err(); err != nil` 语义自然;
//	NotNil—— 快速判断是否收集过错误。
//
// go-zero 内部大量使用:proc.Close、comboWriter.Close 等。
// ————————————————————————————————————————————————————————————————————————————
package errorx

import (
	"errors"
	"sync"
)

// BatchError is an error that can hold multiple errors.
// 批错误容器:错误切片 + 读写锁(Add 写多,Err 读)。
type BatchError struct {
	errs []error
	lock sync.RWMutex
}

// Add adds one or more non-nil errors to the BatchError instance.
// 添加错误:自动忽略 nil,可一次加多个,线程安全。
func (be *BatchError) Add(errs ...error) {
	be.lock.Lock()
	defer be.lock.Unlock()

	for _, err := range errs {
		if err != nil {
			be.errs = append(be.errs, err)
		}
	}
}

// Err returns an error that represents all accumulated errors.
// It returns nil if there are no errors.
// 聚合返回:无错误时返回 nil(所以可直接当普通 error 用)。
func (be *BatchError) Err() error {
	be.lock.RLock()
	defer be.lock.RUnlock()

	// If there are no non-nil errors, errors.Join(...) returns nil.
	// errors.Join 把多个错误串成一个;入参全空时返回 nil。
	return errors.Join(be.errs...)
}

// NotNil checks if there is at least one error inside the BatchError.
// 是否收集过至少一个错误。
func (be *BatchError) NotNil() bool {
	be.lock.RLock()
	defer be.lock.RUnlock()

	return len(be.errs) > 0
}
