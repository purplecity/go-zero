// ————————————————————————————————————————————————————————————————————————————
// usage —— 进程自监控:CPU(EMA 平滑)+ 内存/GC 报表 —— 文件总结
//
// init 起 goroutine 双 ticker:
//
//	每 250ms:internal.RefreshCpu() 采一次 CPU,做 EMA 平滑
//	  cpu = 旧值×0.95 + 新值×0.05 —— 时间常数 ≈ 250ms/(1-0.95)
//	  = 5s,即"过去约 5 秒的平均负载",平滑掉瞬时毛刺;
//	每 1 分钟:打印一行内存/GC(Alloc/TotalAlloc/Sys/NumGC)。
//
// CpuUsage() 返回 0~1000 的千分比(1000 = 用满 1 个核,
// 即 k8s 的 milli 格式),是 adaptiveshedder 判断过载的
// 信号源;采样底盘在 internal/(cgroup 感知,容器里按配额
// 而不是整机核数算利用率)。
// ————————————————————————————————————————————————————————————————————————————
package stat

import (
	"runtime/debug"
	"runtime/metrics"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stat/internal"
	"github.com/zeromicro/go-zero/core/threading"
)

const (
	// 250ms and 0.95 as beta will count the average cpu load for past 5 seconds
	// CPU 采样周期。
	cpuRefreshInterval = time.Millisecond * 250
	// 内存/GC 报表周期。
	allRefreshInterval = time.Minute
	// moving average beta hyperparameter
	// EMA 系数:0.95 → 平滑窗口约 5 秒。
	beta = 0.95
)

// cpuUsage 当前 CPU 千分比(0~1000,原子读写)。
var cpuUsage int64

func init() {
	go func() {
		cpuTicker := time.NewTicker(cpuRefreshInterval)
		defer cpuTicker.Stop()
		allTicker := time.NewTicker(allRefreshInterval)
		defer allTicker.Stop()

		for {
			select {
			case <-cpuTicker.C:
				threading.RunSafe(func() {
					curUsage := internal.RefreshCpu()
					prevUsage := atomic.LoadInt64(&cpuUsage)
					// cpu = cpuᵗ⁻¹ * beta + cpuᵗ * (1 - beta)
					// EMA:新值只占 5%,旧值占 95% —— 平滑。
					usage := int64(float64(prevUsage)*beta + float64(curUsage)*(1-beta))
					atomic.StoreInt64(&cpuUsage, usage)
				})
			case <-allTicker.C:
				if logEnabled.True() {
					printUsage()
				}
			}
		}
	}()
}

// CpuUsage returns current cpu usage.
// 当前 CPU 千分比(1000 = 1 核;脱落器用过载阈值 900 即 90%)。
func CpuUsage() int64 {
	return atomic.LoadInt64(&cpuUsage)
}

// bToMb 字节数转 MiB。
func bToMb(b uint64) float32 {
	return float32(b) / 1024 / 1024
}

// printUsage 打一行内存/GC:堆对象、累计分配、向 OS 申请、GC 次数
// (数据源:runtime/metrics 与 debug.ReadGCStats)。
func printUsage() {
	var (
		alloc, totalAlloc, sys uint64
		samples                = []metrics.Sample{
			{Name: "/memory/classes/heap/objects:bytes"},
			{Name: "/gc/heap/allocs:bytes"},
			{Name: "/memory/classes/total:bytes"},
		}
		stats debug.GCStats
	)
	metrics.Read(samples)

	if samples[0].Value.Kind() == metrics.KindUint64 {
		alloc = samples[0].Value.Uint64()
	}
	if samples[1].Value.Kind() == metrics.KindUint64 {
		totalAlloc = samples[1].Value.Uint64()
	}
	if samples[2].Value.Kind() == metrics.KindUint64 {
		sys = samples[2].Value.Uint64()
	}
	debug.ReadGCStats(&stats)

	logx.Statf("CPU: %dm, MEMORY: Alloc=%.1fMi, TotalAlloc=%.1fMi, Sys=%.1fMi, NumGC=%d",
		CpuUsage(), bToMb(alloc), bToMb(totalAlloc), bToMb(sys), stats.NumGC)
}
