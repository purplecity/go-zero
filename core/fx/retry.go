// ————————————————————————————————————————————————————————————————————————————
// retry —— 重试执行器(串行重试 + 超时总闸) —— 文件总结
//
// DoWithRetry/DoWithRetryCtx:失败后按配置重试,默认 3 次。
// 四个选项:次数(WithRetry)、重试间隔(WithInterval)、
// 整体超时(WithTimeout)、忽略特定错误(WithIgnoreErrors)。
// 错误聚合:每次失败的错误都进 BatchError,最终一起返回
// (不是只报最后一个)。
// 实现要点:
//  1. errChan 容量 1 —— 每轮循环起一个 goroutine 跑 fn,
//     若本轮因 ctx 超时退出,迟到的写入不会卡死 worker;
//  2. 间隔等待也 select ctx —— 等待期间取消/超时立即返回;
//  3. ignoreErrors 命中 errors.Is 则视为成功返回 nil
//     (典型:ErrAlreadyExists 之类"无需重试"的错误)。
//
// 注意:每次重试是新一轮 goroutine;fn 若访问外部全局变量,
// 自己加锁防数据竞争。
// ————————————————————————————————————————————————————————————————————————————
package fx

import (
	"context"
	"errors"
	"time"

	"github.com/zeromicro/go-zero/core/errorx"
)

// defaultRetryTimes 默认重试次数。
const defaultRetryTimes = 3

type (
	// RetryOption defines the method to customize DoWithRetry.
	// 重试选项(函数式选项模式)。
	RetryOption func(*retryOptions)

	retryOptions struct {
		// times 总尝试次数(含首次)。
		times int
		// interval 两次尝试之间的间隔。
		interval time.Duration
		// timeout 整个重试流程的超时(0 表示不限)。
		timeout time.Duration
		// ignoreErrors 命中即视为成功的错误列表。
		ignoreErrors []error
	}
)

// DoWithRetry runs fn, and retries if failed. Default to retry 3 times.
// Note that if the fn function accesses global variables outside the function
// and performs modification operations, it is best to lock them,
// otherwise there may be data race issues
// 带重试执行 fn(无 ctx 版本,默认 3 次)。
func DoWithRetry(fn func() error, opts ...RetryOption) error {
	return retry(context.Background(), func(errChan chan error, retryCount int) {
		errChan <- fn()
	}, opts...)
}

// DoWithRetryCtx runs fn, and retries if failed. Default to retry 3 times.
// fn retryCount indicates the current number of retries, starting from 0
// Note that if the fn function accesses global variables outside the function
// and performs modification operations, it is best to lock them,
// otherwise there may be data race issues
// 带重试执行 fn(ctx 版本,retryCount 从 0 起,可按轮次调整策略)。
func DoWithRetryCtx(ctx context.Context, fn func(ctx context.Context, retryCount int) error,
	opts ...RetryOption) error {
	return retry(ctx, func(errChan chan error, retryCount int) {
		errChan <- fn(ctx, retryCount)
	}, opts...)
}

// retry 重试主循环:每轮起 goroutine 跑 fn,select 等
// 结果与 ctx;间隔等待也可被 ctx 打断。
func retry(ctx context.Context, fn func(errChan chan error, retryCount int), opts ...RetryOption) error {
	options := newRetryOptions()
	for _, opt := range opts {
		opt(options)
	}

	var berr errorx.BatchError
	var cancelFunc context.CancelFunc
	// 配了整体超时则包一层 WithTimeout。
	if options.timeout > 0 {
		ctx, cancelFunc = context.WithTimeout(ctx, options.timeout)
		defer cancelFunc()
	}

	// 容量 1:本轮因 ctx 超时退出时,迟到的 fn 结果不阻塞 goroutine。
	errChan := make(chan error, 1)
	for i := 0; i < options.times; i++ {
		go fn(errChan, i)

		select {
		case err := <-errChan:
			if err != nil {
				// 命中忽略名单:视为成功,立即返回。
				for _, ignoreErr := range options.ignoreErrors {
					if errors.Is(err, ignoreErr) {
						return nil
					}
				}
				berr.Add(err)
			} else {
				// 成功即返回。
				return nil
			}
		case <-ctx.Done():
			// 整体超时/外部取消:带上已累积的错误返回。
			berr.Add(ctx.Err())
			return berr.Err()
		}

		// 有间隔则等待(等待期间同样响应取消)。
		if options.interval > 0 {
			select {
			case <-ctx.Done():
				berr.Add(ctx.Err())
				return berr.Err()
			case <-time.After(options.interval):
			}
		}
	}

	// 用尽次数:聚合的全部错误一起返回。
	return berr.Err()
}

// WithIgnoreErrors Ignore the specified errors
// 选项:这些错误视为成功(errors.Is 匹配)。
func WithIgnoreErrors(ignoreErrors []error) RetryOption {
	return func(options *retryOptions) {
		options.ignoreErrors = ignoreErrors
	}
}

// WithInterval customizes a DoWithRetry call with given interval.
// 选项:重试间隔。
func WithInterval(interval time.Duration) RetryOption {
	return func(options *retryOptions) {
		options.interval = interval
	}
}

// WithRetry customizes a DoWithRetry call with given retry times.
// 选项:总尝试次数。
func WithRetry(times int) RetryOption {
	return func(options *retryOptions) {
		options.times = times
	}
}

// WithTimeout customizes a DoWithRetry call with given timeout.
// 选项:整体超时。
func WithTimeout(timeout time.Duration) RetryOption {
	return func(options *retryOptions) {
		options.timeout = timeout
	}
}

// newRetryOptions 默认配置:3 次尝试、无间隔、无超时。
func newRetryOptions() *retryOptions {
	return &retryOptions{
		times: defaultRetryTimes,
	}
}
