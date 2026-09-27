// ————————————————————————————————————————————————————————————————————————————
// metrics —— 每分钟一报:QPS + 耗时分位数报表 —— 文件总结
//
// 被统计方(rest 的 MetricHandler、脱落器 sheddingstat 等)
// 每笔请求 Add 一个 Task;PeriodicalExecutor 后台攒批,每
// 1 分钟 RemoveAll 取走全部任务、Execute 出一份 StatReport:
//
//	QPS = 任务数/60;Drops = Drop 任务数;Average = 总时长/n;
//	Median/Top90th/Top99th/Top99p9th = 耗时分位数。
//
// 【分位数的"层层剥"技巧】(Execute 里那串嵌套 if):
//
//	先 topK(全部,50%) → [0] = 中位数;在前 50% 里再
//	topK(10%) → [0] = P90;再剥 1% → P99;再剥 0.1% →
//	P99.9。每层只在上层结果(已缩小 5~10 倍)里继续筛,
//	而不是每次都从全量重算 —— n 很大时省一大截。
//	样本不足的层(如 n<2、<10、<100、<1000)直接用上一层
//	的最大值兜底(分位数本就无意义,给个上界)。
//
// 输出两路:reportWriter(可选,SetReportWriter 注入,如
// RemoteWriter POST 到远端)+ logx.Statf 一行日志(可关)。
// ————————————————————————————————————————————————————————————
package stat

import (
	"os"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/executors"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/syncx"
)

var (
	// logInterval 出报表的周期(1 分钟 = QPS 的分母)。
	logInterval  = time.Minute
	writerLock   sync.Mutex
	reportWriter Writer = nil
	logEnabled          = syncx.ForAtomicBool(true)
)

type (
	// Writer interface wraps the Write method.
	// 报表输出后端(如 RemoteWriter;默认 nil 只打日志)。
	Writer interface {
		Write(report *StatReport) error
	}

	// A StatReport is a stat report entry.
	// 一分钟窗口的报表(QPS/丢弃/均值/四个分位数)。
	StatReport struct {
		Name          string  `json:"name"`
		Timestamp     int64   `json:"tm"`
		Pid           int     `json:"pid"`
		ReqsPerSecond float32 `json:"qps"`
		Drops         int     `json:"drops"`
		Average       float32 `json:"avg"`
		Median        float32 `json:"med"`
		Top90th       float32 `json:"t90"`
		Top99th       float32 `json:"t99"`
		Top99p9th     float32 `json:"t99p9"`
	}

	// A Metrics is used to log and report stat reports.
	// 报表器:PeriodicalExecutor 攒批 + container 聚合。
	Metrics struct {
		// executor 周期执行器:每分钟触发一次批量 Execute。
		executor  *executors.PeriodicalExecutor
		container *metricsContainer
	}
)

// DisableLog disables logs of stats.
// 关掉 statf 日志输出(报表 writer 不受影响)。
func DisableLog() {
	logEnabled.Set(false)
}

// SetReportWriter sets the report writer.
// 注入报表后端(锁保护:可能被其他 goroutine 调)。
func SetReportWriter(writer Writer) {
	writerLock.Lock()
	reportWriter = writer
	writerLock.Unlock()
}

// NewMetrics returns a Metrics.
// 创建报表器(名字带上进程 pid 进报表)。
func NewMetrics(name string) *Metrics {
	container := &metricsContainer{
		name: name,
		pid:  os.Getpid(),
	}

	return &Metrics{
		executor:  executors.NewPeriodicalExecutor(logInterval, container),
		container: container,
	}
}

// Add adds task to m.
// 上报一笔任务(耗时统计)。
func (m *Metrics) Add(task Task) {
	m.executor.Add(task)
}

// AddDrop adds a drop to m.
// 上报一笔丢弃(如断路器/脱落器拒绝的请求)。
func (m *Metrics) AddDrop() {
	m.executor.Add(Task{
		Drop: true,
	})
}

// SetName sets the name of m.
// 改名(Sync 保证与后续 Add 的执行顺序)。
func (m *Metrics) SetName(name string) {
	m.executor.Sync(func() {
		m.container.name = name
	})
}

type (
	// tasksDurationPair RemoveAll 交出的快照:任务集 + 总时长 + 丢弃数。
	tasksDurationPair struct {
		tasks    []Task
		duration time.Duration
		drops    int
	}

	// metricsContainer 聚合容器:攒 Task,被周期取空。
	metricsContainer struct {
		name     string
		pid      int
		tasks    []Task
		duration time.Duration
		drops    int
	}
)

// AddTask 收一笔:丢弃只计数,正常任务记时长。
func (c *metricsContainer) AddTask(v any) bool {
	if task, ok := v.(Task); ok {
		if task.Drop {
			c.drops++
		} else {
			c.tasks = append(c.tasks, task)
			c.duration += task.Duration
		}
	}

	return false
}

// Execute 对取走的快照出报表(分位数层层剥,见文件头)。
func (c *metricsContainer) Execute(v any) {
	pair := v.(tasksDurationPair)
	tasks := pair.tasks
	duration := pair.duration
	drops := pair.drops
	size := len(tasks)
	report := &StatReport{
		Name:          c.name,
		Timestamp:     time.Now().Unix(),
		Pid:           c.pid,
		ReqsPerSecond: float32(size) / float32(logInterval/time.Second),
		Drops:         drops,
	}

	if size > 0 {
		report.Average = float32(duration/time.Millisecond) / float32(size)

		fiftyPercent := size >> 1
		if fiftyPercent > 0 {
			// 剥 50%:[0] = 中位数
			top50pTasks := topK(tasks, fiftyPercent)
			medianTask := top50pTasks[0]
			report.Median = float32(medianTask.Duration) / float32(time.Millisecond)
			tenPercent := fiftyPercent / 5
			if tenPercent > 0 {
				// 在前 50% 里剥 10%:[0] = P90
				top10pTasks := topK(top50pTasks, tenPercent)
				task90th := top10pTasks[0]
				report.Top90th = float32(task90th.Duration) / float32(time.Millisecond)
				onePercent := tenPercent / 10
				if onePercent > 0 {
					// 再剥 1%:[0] = P99
					top1pTasks := topK(top10pTasks, onePercent)
					task99th := top1pTasks[0]
					report.Top99th = float32(task99th.Duration) / float32(time.Millisecond)
					pointOnePercent := onePercent / 10
					if pointOnePercent > 0 {
						// 再剥 0.1%:[0] = P99.9
						topPointOneTasks := topK(top1pTasks, pointOnePercent)
						task99Point9th := topPointOneTasks[0]
						report.Top99p9th = float32(task99Point9th.Duration) / float32(time.Millisecond)
					} else {
						// 样本 <1000:P99.9 用 P99 集合的最大值兜底
						report.Top99p9th = getTopDuration(top1pTasks)
					}
				} else {
					// 样本 <100:用前 10% 集合的最大值兜底
					mostDuration := getTopDuration(top10pTasks)
					report.Top99th = mostDuration
					report.Top99p9th = mostDuration
				}
			} else {
				// 样本 <10:同理兜底
				mostDuration := getTopDuration(top50pTasks)
				report.Top90th = mostDuration
				report.Top99th = mostDuration
				report.Top99p9th = mostDuration
			}
		} else {
			// 样本 1 个:全部等于它
			mostDuration := getTopDuration(tasks)
			report.Median = mostDuration
			report.Top90th = mostDuration
			report.Top99th = mostDuration
			report.Top99p9th = mostDuration
		}
	}

	log(report)
}

// RemoveAll 被周期执行器取空:交出快照,容器归零重攒。
func (c *metricsContainer) RemoveAll() any {
	tasks := c.tasks
	duration := c.duration
	drops := c.drops
	c.tasks = nil
	c.duration = 0
	c.drops = 0

	return tasksDurationPair{
		tasks:    tasks,
		duration: duration,
		drops:    drops,
	}
}

// getTopDuration 集合中的最大耗时(毫秒,topK k=1)。
func getTopDuration(tasks []Task) float32 {
	top := topK(tasks, 1)
	if len(top) < 1 {
		return 0
	}

	return float32(top[0].Duration) / float32(time.Millisecond)
}

// log 双路输出:writer(可选)+ Statf 日志(可关)。
func log(report *StatReport) {
	writeReport(report)
	if logEnabled.True() {
		logx.Statf("(%s) - qps: %.1f/s, drops: %d, avg time: %.1fms, med: %.1fms, "+
			"90th: %.1fms, 99th: %.1fms, 99.9th: %.1fms",
			report.Name, report.ReqsPerSecond, report.Drops, report.Average, report.Median,
			report.Top90th, report.Top99th, report.Top99p9th)
	}
}

// writeReport 写远端后端(失败只记日志,不影响主流程)。
func writeReport(report *StatReport) {
	writerLock.Lock()
	defer writerLock.Unlock()

	if reportWriter != nil {
		if err := reportWriter.Write(report); err != nil {
			logx.Error(err)
		}
	}
}
