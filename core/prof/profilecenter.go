// ————————————————————————————————————————————————————————————————————————————
// profilecenter —— 耗时数据聚合中心 —— 文件总结
//
// profiler 上报的耗时按 name 聚合到 profileSlot,每个槽位维护
// 四个原子计数:
//
//	lifecount/lifecycle —— 进程存活期内的总次数与总耗时
//	lastcount/lastcycle —— 距上次报表以来的次数与总耗时
//
// 后台 goroutine 每 5 分钟生成一次 CSV 报表写入 stat 日志
// (logx.Stat),并把 last* 两个计数清零重新累计 —— 报表里
// 同时能看到"全生命周期均值"和"最近 5 分钟均值"。
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
	// lifecount 进程存活的累计次数。
	lifecount int64
	// lastcount 距上次报表的次数(定期清零)。
	lastcount int64
	// lifecycle 累计总耗时(纳秒)。
	lifecycle int64
	// lastcycle 距上次报表的总耗时(定期清零)。
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

// report 上报一次耗时:找到(或创建)槽位,四个计数全部原子累加。
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
// 表头 QUEUE,LIFECOUNT,LIFECYCLE,LASTCOUNT,LASTCYCLE,
// 平均耗时 = 总耗时/次数(次数为 0 显示 "-")。
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
