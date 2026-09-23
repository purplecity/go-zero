// ————————————————————————————————————————————————————————————————————————————
// runtime —— 进程运行时状态周期打印 —— 文件总结
//
// DisplayStats 启动一个后台 goroutine,按间隔(默认 5 秒)向
// stdout 打一行关键运行时指标:
//
//	Goroutines —— 当前 goroutine 数(暴涨 = 泄漏或阻塞)
//	Alloc      —— 当前堆上存活对象字节数
//	TotalAlloc —— 累计分配字节数(只增,反映分配速率)
//	Sys        —— 向操作系统申请的总内存
//	NumGC      —— 累计 GC 次数
//
// 数据来自 runtime/metrics(新一代指标接口)与 debug.ReadGCStats。
// 典型用法:压测/线上观察时临时开启。
// ————————————————————————————————————————————————————————————————————————————
package prof

import (
	"fmt"
	"io"
	"os"
	"runtime"
	"runtime/debug"
	"runtime/metrics"
	"time"
)

const (
	// defaultInterval 默认打印间隔。
	defaultInterval = time.Second * 5
	// mega 1MB 字节数。
	mega = 1024 * 1024
)

// DisplayStats prints the goroutine, memory, GC stats with given interval, default to 5 seconds.
// 周期打印运行时状态到 stdout,间隔可选(不传默认 5 秒)。
func DisplayStats(interval ...time.Duration) {
	displayStatsWithWriter(os.Stdout, interval...)
}

// displayStatsWithWriter 可指定输出目的地的实现(测试注入用)。
func displayStatsWithWriter(writer io.Writer, interval ...time.Duration) {
	duration := defaultInterval
	for _, val := range interval {
		duration = val
	}

	go func() {
		ticker := time.NewTicker(duration)
		defer ticker.Stop()
		for range ticker.C {
			// 从 runtime/metrics 读取三个内存指标。
			var (
				alloc, totalAlloc, sys uint64
				samples                = []metrics.Sample{
					{Name: "/memory/classes/heap/objects:bytes"},
					{Name: "/gc/heap/allocs:bytes"},
					{Name: "/memory/classes/total:bytes"},
				}
			)
			metrics.Read(samples)

			// 读回后校验类型再取值(防御性)。
			if samples[0].Value.Kind() == metrics.KindUint64 {
				alloc = samples[0].Value.Uint64()
			}
			if samples[1].Value.Kind() == metrics.KindUint64 {
				totalAlloc = samples[1].Value.Uint64()
			}
			if samples[2].Value.Kind() == metrics.KindUint64 {
				sys = samples[2].Value.Uint64()
			}
			var stats debug.GCStats
			debug.ReadGCStats(&stats)
			fmt.Fprintf(writer, "Goroutines: %d, Alloc: %vm, TotalAlloc: %vm, Sys: %vm, NumGC: %v\n",
				runtime.NumGoroutine(), alloc/mega, totalAlloc/mega, sys/mega, stats.NumGC)
		}
	}()
}
