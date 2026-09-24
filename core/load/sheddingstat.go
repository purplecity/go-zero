// ————————————————————————————————————————————————————————————————————————————
// sheddingstat —— 负载脱落统计(每分钟报表) —— 文件总结
//
// 三个原子计数(total/pass/drop),后台 goroutine 每 1 分钟
// 取一次快照写 stat 日志并清零 —— 与 profilecenter 同一
// "窗口计数 + 定期清零" 手法(手搓 rate)。drop>0 时日志
// 标签变为 shedding_stat_drop,方便直接 grep 出"丢过请求"
// 的时段。DisableLog() 可关报表。
// ————————————————————————————————————————————————————————————————————————————
package load

import (
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/stat"
)

type (
	// A SheddingStat is used to store the statistics for load shedding.
	// 脱落统计:total 总数 / pass 放行 / drop 丢弃。
	SheddingStat struct {
		// name 业务标识(报表里区分来源)。
		name string
		// total 窗口内请求总数。
		total int64
		// pass 窗口内放行数。
		pass int64
		// drop 窗口内丢弃数。
		drop int64
	}

	// snapshot 一分钟窗口的计数快照。
	snapshot struct {
		Total int64
		Pass  int64
		Drop  int64
	}
)

// NewSheddingStat returns a SheddingStat.
// 创建统计并启动每分钟报表 goroutine。
func NewSheddingStat(name string) *SheddingStat {
	st := &SheddingStat{
		name: name,
	}
	go st.run()
	return st
}

// IncrementTotal increments the total requests.
// 总数 +1。
func (s *SheddingStat) IncrementTotal() {
	atomic.AddInt64(&s.total, 1)
}

// IncrementPass increments the passed requests.
// 放行数 +1。
func (s *SheddingStat) IncrementPass() {
	atomic.AddInt64(&s.pass, 1)
}

// IncrementDrop increments the dropped requests.
// 丢弃数 +1。
func (s *SheddingStat) IncrementDrop() {
	atomic.AddInt64(&s.drop, 1)
}

// loop 每分钟清零取快照并写 stat 日志;
// drop>0 用 shedding_stat_drop 标签,可直接 grep 丢请求时段。
func (s *SheddingStat) loop(c <-chan time.Time) {
	for range c {
		st := s.reset()

		if !logEnabled.True() {
			continue
		}

		c := stat.CpuUsage()
		if st.Drop == 0 {
			logx.Statf("(%s) shedding_stat [1m], cpu: %d, total: %d, pass: %d, drop: %d",
				s.name, c, st.Total, st.Pass, st.Drop)
		} else {
			logx.Statf("(%s) shedding_stat_drop [1m], cpu: %d, total: %d, pass: %d, drop: %d",
				s.name, c, st.Total, st.Pass, st.Drop)
		}
	}
}

// reset 原子清零三个计数并返回清零前的快照(Swap 一步完成)。
func (s *SheddingStat) reset() snapshot {
	return snapshot{
		Total: atomic.SwapInt64(&s.total, 0),
		Pass:  atomic.SwapInt64(&s.pass, 0),
		Drop:  atomic.SwapInt64(&s.drop, 0),
	}
}

// run 启动每分钟一跳的报表循环。
func (s *SheddingStat) run() {
	ticker := time.NewTicker(time.Minute)
	defer ticker.Stop()

	s.loop(ticker.C)
}
