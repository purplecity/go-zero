// ————————————————————————————————————————————————————————————————————————————
// timingwheel —— 时间轮定时器(带轮次) —— 文件总结
//
// numSlots 个槽围成环,ticker 每 interval 走一格;任务按
// delay 算出目标槽与轮次(circle):delay 超过一圈时任务
// 挂在目标槽、记 circle,每次扫过 circle-- ,减到 0 才执行
// —— 一层轮子即可表达任意长的延时。
//
// 并发模型(本文件的核心):所有对外操作(定时/移动/删除/
// 取空)不直接改数据,而是丢进各自的 channel,由唯一的
// run goroutine 串行消费 —— 单线程拥有全部状态,零锁、
// 天然无竞争;ticker 到点也走同一个循环。
//
// 关键结构:
//
//	slots    每格一条双向链表(同槽多个任务);
//	timers   key → 槽位+任务指针(SafeMap),按 key
//	         O(1) 定位,支撑 Move/Remove;
//	removed  惰性删除标记:摘链表交给扫描时顺手做,
//	         避免遍历找节点;
//	diff     Move 时的"还差几格到新槽",扫描到时
//	         顺手把任务挪到新槽(setTimerPosition 重登记)。
//
// 使用方:cache.go 的过期删除(1s × 300 槽)。
// ————————————————————————————————————————————————————————————————————————————
package collection

import (
	"container/list"
	"errors"
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/lang"
	"github.com/zeromicro/go-zero/core/threading"
	"github.com/zeromicro/go-zero/core/timex"
)

// drainWorkers Drain 全量执行时的并发 worker 数。
const drainWorkers = 8

var (
	// ErrClosed 时间轮已停止。
	ErrClosed = errors.New("TimingWheel is closed already")
	// ErrArgument 参数非法(key 为空或 delay 非正)。
	ErrArgument = errors.New("incorrect task argument")
)

type (
	// Execute defines the method to execute the task.
	// 任务执行回调。
	Execute func(key, value any)

	// A TimingWheel is a timing wheel object to schedule tasks.
	// 时间轮:槽环 + 轮次表达任意延时(见文件头)。
	TimingWheel struct {
		// interval 单槽时长(ticker 周期)。
		interval time.Duration
		// ticker 驱动走格的计时器(可注入 FakeTicker 测试)。
		ticker timex.Ticker
		// slots 槽环:每格一条任务链表。
		slots []*list.List
		// timers key → 槽位登记表(按 key 定位任务)。
		timers *SafeMap
		// tickedPos 当前已走到的槽(初始在"上一圈"末尾,
		// 让第一个 tick 从 0 号槽开始)。
		tickedPos int
		// numSlots 槽数。
		numSlots int
		// execute 任务到期回调。
		execute Execute
		// 以下五个 channel 是对外操作进 run 循环的入口。
		setChannel    chan timingEntry
		moveChannel   chan baseEntry
		removeChannel chan any
		drainChannel  chan func(key, value any)
		stopChannel   chan lang.PlaceholderType
	}

	// timingEntry 槽里的一个任务。
	timingEntry struct {
		baseEntry
		// value 任务携带的值。
		value any
		// circle 剩余轮次:每次扫过减一,0 才执行。
		circle int
		// diff 移动后距新槽的格数(扫描时顺手挪槽)。
		diff int
		// removed 惰性删除标记:扫描到时才真摘链表。
		removed bool
	}

	// baseEntry 任务基本信息:延时 + 键。
	baseEntry struct {
		delay time.Duration
		key   any
	}

	// positionEntry timers 表里的登记项:槽位 + 任务指针。
	positionEntry struct {
		pos  int
		item *timingEntry
	}

	// timingTask 待执行任务的快照(key+value)。
	timingTask struct {
		key   any
		value any
	}
)

// NewTimingWheel returns a TimingWheel.
// 创建时间轮并启动 run 循环(参数非法返回错误)。
func NewTimingWheel(interval time.Duration, numSlots int, execute Execute) (*TimingWheel, error) {
	if interval <= 0 || numSlots <= 0 || execute == nil {
		return nil, fmt.Errorf("interval: %v, slots: %d, execute: %p",
			interval, numSlots, execute)
	}

	return NewTimingWheelWithTicker(interval, numSlots, execute, timex.NewTicker(interval))
}

// NewTimingWheelWithTicker returns a TimingWheel with the given ticker.
// 创建时间轮(注入自定义 ticker,测试用 FakeTicker 精确控时)。
func NewTimingWheelWithTicker(interval time.Duration, numSlots int, execute Execute,
	ticker timex.Ticker) (*TimingWheel, error) {
	tw := &TimingWheel{
		interval:  interval,
		ticker:    ticker,
		slots:     make([]*list.List, numSlots),
		timers:    NewSafeMap(),
		tickedPos: numSlots - 1, // at previous virtual circle
		// 初始指向末槽:第一格 tick 正好转到 0 号槽。
		execute:       execute,
		numSlots:      numSlots,
		setChannel:    make(chan timingEntry),
		moveChannel:   make(chan baseEntry),
		removeChannel: make(chan any),
		drainChannel:  make(chan func(key, value any)),
		stopChannel:   make(chan lang.PlaceholderType),
	}

	tw.initSlots()
	// 唯一的 owner goroutine:串行消费全部操作与 tick。
	go tw.run()

	return tw, nil
}

// Drain drains all items and executes them.
// 立即取空全部任务并执行(已停止返回 ErrClosed)。
func (tw *TimingWheel) Drain(fn func(key, value any)) error {
	select {
	case tw.drainChannel <- fn:
		return nil
	case <-tw.stopChannel:
		return ErrClosed
	}
}

// MoveTimer moves the task with the given key to the given delay.
// 把已存在的任务挪到新延时(不存在则忽略)。
func (tw *TimingWheel) MoveTimer(key any, delay time.Duration) error {
	if delay <= 0 || key == nil {
		return ErrArgument
	}

	select {
	case tw.moveChannel <- baseEntry{
		delay: delay,
		key:   key,
	}:
		return nil
	case <-tw.stopChannel:
		return ErrClosed
	}
}

// RemoveTimer removes the task with the given key.
// 删除任务(惰性:标记 removed,扫描时摘链表)。
func (tw *TimingWheel) RemoveTimer(key any) error {
	if key == nil {
		return ErrArgument
	}

	select {
	case tw.removeChannel <- key:
		return nil
	case <-tw.stopChannel:
		return ErrClosed
	}
}

// SetTimer sets the task value with the given key to the delay.
// 新设定时任务(key 已存在则更新值并按 Move 处理)。
func (tw *TimingWheel) SetTimer(key, value any, delay time.Duration) error {
	if delay <= 0 || key == nil {
		return ErrArgument
	}

	select {
	case tw.setChannel <- timingEntry{
		baseEntry: baseEntry{
			delay: delay,
			key:   key,
		},
		value: value,
	}:
		return nil
	case <-tw.stopChannel:
		return ErrClosed
	}
}

// Stop stops tw. No more actions after stopping a TimingWheel.
// 停止时间轮:关闭 stopChannel,run 循环退出;
// 之后所有操作因 select 到 stopChannel 而返回 ErrClosed。
func (tw *TimingWheel) Stop() {
	close(tw.stopChannel)
}

// drainAll 取空全部槽:摘链表、清 timers,未删除的任务
// 交给 8 个 worker 并发执行,等全部完成后返回
// (Drain 语义 = "不等定时,现在全部执行")。
func (tw *TimingWheel) drainAll(fn func(key, value any)) {
	runner := threading.NewTaskRunner(drainWorkers)

	for _, slot := range tw.slots {
		for e := slot.Front(); e != nil; {
			task := e.Value.(*timingEntry)
			next := e.Next()
			slot.Remove(e)
			// 只清自己的登记项(同 key 换过任务的不能误删)。
			if val, ok := tw.timers.Get(task.key); ok {
				timer := val.(*positionEntry)
				if timer.item == task {
					tw.timers.Del(task.key)
				}
			}
			e = next
			if !task.removed {
				runner.Schedule(func() {
					fn(task.key, task.value)
				})
			}
		}
	}

	runner.Wait()
}

// getPositionAndCircle 按 delay 算目标槽与轮次:
//
//	steps  = delay/interval(总格数)
//	pos    = (当前槽+steps) % 槽数(目标槽)
//	circle = (steps-1)/槽数(还需转几圈)。
func (tw *TimingWheel) getPositionAndCircle(d time.Duration) (pos, circle int) {
	steps := int(d / tw.interval)
	pos = (tw.tickedPos + steps) % tw.numSlots
	circle = (steps - 1) / tw.numSlots

	return
}

// initSlots 每槽建一条空链表。
func (tw *TimingWheel) initSlots() {
	for i := 0; i < tw.numSlots; i++ {
		tw.slots[i] = list.New()
	}
}

// moveTask 把任务挪到新延时(只在 run goroutine 内执行):
// 三种情况 ——
//
//	新 delay < 一格:立即执行(等不到下格了);
//	目标槽在本圈前方(tick 尚未经过):记 diff,扫描时挪;
//	跨圈(circle>0):轮次减一记 diff(挪到"同圈稍后"的位置);
//	目标槽在本圈后方(已被 tick 过):旧条目标记 removed,
//	新建条目挂到目标槽重新登记。
func (tw *TimingWheel) moveTask(task baseEntry) {
	val, ok := tw.timers.Get(task.key)
	if !ok {
		return
	}

	timer := val.(*positionEntry)
	if task.delay < tw.interval {
		// 不够一个槽周期:立即执行。
		threading.GoSafe(func() {
			tw.execute(timer.item.key, timer.item.value)
		})
		return
	}

	pos, circle := tw.getPositionAndCircle(task.delay)
	if pos >= timer.pos {
		// 前方同圈:diff 记差值,扫描到时挪槽。
		timer.item.circle = circle
		timer.item.diff = pos - timer.pos
	} else if circle > 0 {
		// 跨圈:按"一圈前"的位置挂,diff 记绕远差值。
		circle--
		timer.item.circle = circle
		timer.item.diff = tw.numSlots + pos - timer.pos
	} else {
		// 后方且不跨圈:旧条目惰性删除,新条目重挂。
		timer.item.removed = true
		newItem := &timingEntry{
			baseEntry: task,
			value:     timer.item.value,
		}
		tw.slots[pos].PushBack(newItem)
		tw.setTimerPosition(pos, newItem)
	}
}

// onTick 走一格:指针前移,扫描并执行新指向槽里的到期任务。
func (tw *TimingWheel) onTick() {
	tw.tickedPos = (tw.tickedPos + 1) % tw.numSlots
	l := tw.slots[tw.tickedPos]
	tw.scanAndRunTasks(l)
}

// removeTask 删除:标记 removed + 清登记(链表节点
// 等扫描到时顺手摘掉,见 scanAndRunTasks)。
func (tw *TimingWheel) removeTask(key any) {
	val, ok := tw.timers.Get(key)
	if !ok {
		return
	}

	timer := val.(*positionEntry)
	timer.item.removed = true
	tw.timers.Del(key)
}

// run 唯一的 owner 循环:六路 select —— tick 走格、
// 四种操作、停止。全部状态只被本 goroutine 碰,无锁。
func (tw *TimingWheel) run() {
	for {
		select {
		case <-tw.ticker.Chan():
			tw.onTick()
		case task := <-tw.setChannel:
			tw.setTask(&task)
		case key := <-tw.removeChannel:
			tw.removeTask(key)
		case task := <-tw.moveChannel:
			tw.moveTask(task)
		case fn := <-tw.drainChannel:
			tw.drainAll(fn)
		case <-tw.stopChannel:
			tw.ticker.Stop()
			return
		}
	}
}

// runTasks 异步执行一批到期任务:独立 goroutine 逐个
// RunSafe(单个任务 panic 不影响其余)。
func (tw *TimingWheel) runTasks(tasks []timingTask) {
	if len(tasks) == 0 {
		return
	}

	go func() {
		for i := range tasks {
			threading.RunSafe(func() {
				tw.execute(tasks[i].key, tasks[i].value)
			})
		}
	}()
}

// scanAndRunTasks 扫描一个槽,对每个任务四选一:
//
//	removed:摘链表丢弃(惰性删除落地);
//	circle>0:轮次减一,留下等后续圈;
//	diff>0:挪到"当前槽+diff"的新位置(Move 的落地);
//	其余:到期 —— 收集待执行,摘链表、清登记。
//
// 执行经 runTasks 异步发出,不阻塞 tick 循环。
func (tw *TimingWheel) scanAndRunTasks(l *list.List) {
	var tasks []timingTask

	for e := l.Front(); e != nil; {
		task := e.Value.(*timingEntry)
		if task.removed {
			// 惰性删除:现在才真摘。
			next := e.Next()
			l.Remove(e)
			e = next
			continue
		} else if task.circle > 0 {
			// 未到目标轮次:圈数减一。
			task.circle--
			e = e.Next()
			continue
		} else if task.diff > 0 {
			// Move 挪槽落地:摘下重挂到新位置。
			next := e.Next()
			l.Remove(e)
			// (tw.tickedPos+task.diff)%tw.numSlots
			// cannot be the same value of tw.tickedPos
			// 挪动后的位置不可能等于当前槽(否则无需挪)。
			pos := (tw.tickedPos + task.diff) % tw.numSlots
			tw.slots[pos].PushBack(task)
			tw.setTimerPosition(pos, task)
			task.diff = 0
			e = next
			continue
		}

		// 到期:收集执行,摘链表、清登记。
		tasks = append(tasks, timingTask{
			key:   task.key,
			value: task.value,
		})
		next := e.Next()
		l.Remove(e)
		tw.timers.Del(task.key)
		e = next
	}

	tw.runTasks(tasks)
}

// setTask 登记新任务(只被 run goroutine 调):
// delay 不足一格抬到一格(最快也是下一格执行);
// key 已存在则更新值并走 Move 逻辑。
func (tw *TimingWheel) setTask(task *timingEntry) {
	if task.delay < tw.interval {
		task.delay = tw.interval
	}

	if val, ok := tw.timers.Get(task.key); ok {
		entry := val.(*positionEntry)
		entry.item.value = task.value
		tw.moveTask(task.baseEntry)
	} else {
		pos, circle := tw.getPositionAndCircle(task.delay)
		task.circle = circle
		tw.slots[pos].PushBack(task)
		tw.setTimerPosition(pos, task)
	}
}

// setTimerPosition 更新/新建 key 的槽位登记(timers 表)。
func (tw *TimingWheel) setTimerPosition(pos int, task *timingEntry) {
	if val, ok := tw.timers.Get(task.key); ok {
		timer := val.(*positionEntry)
		timer.item = task
		timer.pos = pos
	} else {
		tw.timers.Set(task.key, &positionEntry{
			pos:  pos,
			item: task,
		})
	}
}
