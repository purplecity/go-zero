package cache

import (
	"fmt"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/collection"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/stat"
	"github.com/zeromicro/go-zero/core/stringx"
	"github.com/zeromicro/go-zero/core/threading"
)

const (
	timingWheelSlots = 300
	cleanWorkers     = 5
	taskKeyLen       = 8
)

var (
	// use atomic to avoid data race in unit tests
	timingWheel atomic.Value
	taskRunner  = threading.NewTaskRunner(cleanWorkers)
)

type delayTask struct {
	delay time.Duration
	task  func() error
	keys  []string
}

func init() {
	tw, err := collection.NewTimingWheel(time.Second, timingWheelSlots, clean)
	logx.Must(err)
	timingWheel.Store(tw)

	proc.AddShutdownListener(func() {
		if err := tw.Drain(clean); err != nil {
			logx.Errorf("failed to drain timing wheel: %v", err)
		}
	})
}

// AddCleanTask adds a clean task on given keys.
func AddCleanTask(task func() error, keys ...string) {
	tw := timingWheel.Load().(*collection.TimingWheel)
	if err := tw.SetTimer(stringx.Randn(taskKeyLen), delayTask{
		delay: time.Second,
		task:  task,
		keys:  keys,
	}, time.Second); err != nil {
		logx.Errorf("failed to set timer for keys: %q, error: %v", formatKeys(keys), err)
	}
}

func clean(key, value any) {
	taskRunner.Schedule(func() {
		dt := value.(delayTask)
		err := dt.task()
		if err == nil {
			return
		}

		next, ok := nextDelay(dt.delay)
		if ok {
			dt.delay = next
			tw := timingWheel.Load().(*collection.TimingWheel)
			if err = tw.SetTimer(key, dt, next); err != nil {
				logx.Errorf("failed to set timer for key: %s, error: %v", key, err)
			}
		} else {
			// 退避梯子走完(1s→5s→1m→5m→1h,累计约 66 分钟):
			// 放弃重试 —— 打日志 + 报警(接 stat/alert 的 5 分钟
			// 节流通道);不再 SetTimer,任务从时间轮自然消失。
			// 脏缓存留待 TTL 自然过期 —— TTL 才是最终一致性兜底,
			// 重试只是缩短不一致窗口。
			// 注意:下行 Sprintf 里调用的是 dt.task() 本身 ——
			// 格式化错误信息时顺带真的又删了一次(第 6 次、立即
			// 执行);本意应为已捕获的 err,疑似笔误。删除幂等,
			// 副作用仅:这意外的一搏若成功,报警仍按失败发出。
			msg := fmt.Sprintf("retried but failed to clear cache with keys: %q, error: %v",
				formatKeys(dt.keys), dt.task())
			logx.Error(msg)
			stat.Report(msg)
		}
	})
}

func nextDelay(delay time.Duration) (time.Duration, bool) {
	switch delay {
	case time.Second:
		return time.Second * 5, true
	case time.Second * 5:
		return time.Minute, true
	case time.Minute:
		return time.Minute * 5, true
	case time.Minute * 5:
		return time.Hour, true
	default:
		return 0, false
	}
}
