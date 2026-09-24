// ————————————————————————————————————————————————————————————————————————————
// profilecenter —— 耗时数据聚合中心 —— 文件总结
//
// 定位:零依赖的轻量打点方案(CSV 写日志),与 core/metric
// (Prometheus)功能重叠但处处简化;框架自身不 import 本包
// (core/prof 之外零引用),纯靠业务代码经 profiler.go 的
// Report 上报,属 opt-in 自助工具。
//
// 数据来源是【外部调用】:业务代码通过 profiler.go 的
// prof.Report(name, point) 上报,框架自身从不触发。
// 耗时按 name 聚合到 profileSlot,每个槽位维护四个原子
// 计数 —— 以对"orderQueue"每次处理打一次点为例:
//
//	lifecount —— 全生命周期累计打点次数:该队列总共处理了几次
//	lifecycle —— 全生命周期累计耗时:所有打点耗时之和(ns);
//	             注意不是"进程运行时间"
//	lastcount —— 最近窗口打点次数:距上次报表(5 分钟)内的
//	             次数,每次报表后清零重累计
//	lastcycle —— 最近窗口累计耗时:同样报表后清零重累计
//
// 后台 goroutine 每 5 分钟生成一次 CSV 报表写入 stat 日志
// (logx.Stat),并把 last* 清零重累计。报表的读法:
//
//	次数列是吞吐 —— lastcount/300s 即最近 QPS;lastcount=0
//	表示最近 5 分钟一次都没发生(队列卡死的最强信号);
//	LIFECYCLE/LASTCYCLE 列打印的是【平均耗时】(总耗时/次数,
//	见 generateReport 的 calcFn),价值在两列对比:
//	LASTCYCLE ≫ LIFECYCLE → 最近恶化(下游变慢/缓存失效/
//	GC 变频),≪ → 最近好转 —— 没有全周期基线不知道窗口值
//	高低,没有窗口值不知道现在还好不好。
//	last* 清零重累计 = 手搓版 rate():Prometheus 靠服务端
//	两次抓取的 counter 差值算窗口增量,这里没有 server,
//	客户端自己把窗口增量维护出来。
//
// init 无条件启动后台报表 goroutine(即使从未上报过槽位);
// logx.Stat 未配置时无实际输出。
// loadOrStoreSlot 用读锁快路径 + 写锁双重检查,保证并发下
// 同名槽位只创建一次。
// ————————————————————————————————————————————————————————————————————————————
package prof

import (
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

// profileSlot 一个被统计对象(如某个队列)的计数槽位。
type profileSlot struct {
	// lifecount 该统计对象全生命周期的累计打点次数。
	lifecount int64
	// lastcount 最近窗口(距上次报表)的打点次数,报表后清零。
	lastcount int64
	// lifecycle 该统计对象累计耗时(所有打点耗时之和,纳秒)。
	lifecycle int64
	// lastcycle 最近窗口累计耗时,报表后清零。
	lastcycle int64
}

// profileCenter 聚合中心:name → 槽位。
type profileCenter struct {
	lock  sync.RWMutex
	slots map[string]*profileSlot
}

// flushInterval 报表输出间隔。
const flushInterval = 5 * time.Minute

// pc 全局聚合中心单例。
var pc = &profileCenter{
	slots: make(map[string]*profileSlot),
}

func init() {
	// 启动后台报表 goroutine(Safe 版,panic 不打崩进程)。
	flushRepeatedly()
}

// flushRepeatedly 每 5 分钟把聚合报表写入 stat 日志并重置 last* 计数。
func flushRepeatedly() {
	threading.GoSafe(func() {
		for {
			time.Sleep(flushInterval)
			logx.Stat(generateReport())
		}
	})
}

// report 上报一次耗时。调用链:业务代码 prof.Report →
// realProfiler.Report → 本函数(框架自身从不调用)。
// 找到(或创建)槽位,四个计数全部原子累加。
func report(name string, duration time.Duration) {
	slot := loadOrStoreSlot(name, duration)

	atomic.AddInt64(&slot.lifecount, 1)
	atomic.AddInt64(&slot.lastcount, 1)
	atomic.AddInt64(&slot.lifecycle, int64(duration))
	atomic.AddInt64(&slot.lastcycle, int64(duration))
}

// loadOrStoreSlot 按名取槽位,没有则创建(读锁快路径 +
// 写锁双重检查,防止并发重复创建)。
func loadOrStoreSlot(name string, duration time.Duration) *profileSlot {
	pc.lock.RLock()
	slot, ok := pc.slots[name]
	pc.lock.RUnlock()

	if ok {
		return slot
	}

	pc.lock.Lock()
	defer pc.lock.Unlock()

	// double-check
	// 双重检查:等写锁期间可能已被别人创建。
	if slot, ok = pc.slots[name]; ok {
		return slot
	}

	slot = &profileSlot{}
	pc.slots[name] = slot
	return slot
}

// generateReport 生成 CSV 报表并重置 last* 计数:
// 表头 QUEUE,LIFECOUNT,LIFECYCLE,LASTCOUNT,LASTCYCLE。
// 注意 LIFECYCLE/LASTCYCLE 列打印的是平均耗时(总耗时/次数,
// 次数为 0 显示 "-"),读法见文件头(基线 vs 窗口对比)。
func generateReport() string {
	var builder strings.Builder
	builder.WriteString("Profiling report\n")
	builder.WriteString("QUEUE,LIFECOUNT,LIFECYCLE,LASTCOUNT,LASTCYCLE\n")

	// 计算平均耗时;count 为 0 时显示占位符 "-"。
	calcFn := func(total, count int64) string {
		if count == 0 {
			return "-"
		}
		return (time.Duration(total) / time.Duration(count)).String()
	}

	pc.lock.Lock()
	for key, slot := range pc.slots {
		builder.WriteString(fmt.Sprintf("%s,%d,%s,%d,%s\n",
			key,
			slot.lifecount,
			calcFn(slot.lifecycle, slot.lifecount),
			slot.lastcount,
			calcFn(slot.lastcycle, slot.lastcount),
		))

		// reset last cycle stats
		// 重置"最近"计数,下个周期重新累计。
		atomic.StoreInt64(&slot.lastcount, 0)
		atomic.StoreInt64(&slot.lastcycle, 0)
	}
	pc.lock.Unlock()

	return builder.String()
}
