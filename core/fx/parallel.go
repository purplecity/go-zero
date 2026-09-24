// ————————————————————————————————————————————————————————————————————————————
// parallel —— 并行执行一组函数 —— 文件总结
//
// Parallel:全并发跑,只等结束不关心结果;
// ParallelErr:全并发跑,收集全部错误合成一个 BatchError 返回
// (不是短路 —— 每个都会跑完)。
// 都基于 threading.RoutineGroup,RunSafe 保证 panic 不打崩进程。
// 对比 mr.Finish:语义相同,fx 这版更轻(无 channel 流水线)。
// ————————————————————————————————————————————————————————————————————————————
package fx

import (
	"github.com/zeromicro/go-zero/core/errorx"
	"github.com/zeromicro/go-zero/core/threading"
)

// Parallel runs fns parallelly and waits for done.
// 全并发执行并等待结束(不关心结果)。
func Parallel(fns ...func()) {
	group := threading.NewRoutineGroup()
	for _, fn := range fns {
		group.RunSafe(fn)
	}
	group.Wait()
}

// ParallelErr 全并发执行,收集全部错误(BatchError 聚合,
// 逐条列出)而非只报第一个。
func ParallelErr(fns ...func() error) error {
	var be errorx.BatchError

	group := threading.NewRoutineGroup()
	for _, fn := range fns {
		f := fn // 循环变量捕获(旧版 Go 语义),确保各 goroutine 拿到各自的 fn
		group.RunSafe(func() {
			if err := f(); err != nil {
				be.Add(err)
			}
		})
	}
	group.Wait()

	return be.Err()
}
