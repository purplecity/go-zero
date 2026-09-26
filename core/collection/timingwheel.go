// ————————————————————————————————————————————————————————————————————————————
// timingwheel —— 时间轮定时器(单层 + 轮次) —— 文件总结
//
// 【干啥用】用"一根指针 + 一圈格子"这一个计时器统一管理
// 海量"N 秒后做某事"的任务:插入/删除 O(1),每秒只扫指针
// 指向的一个槽(不像优先队列挤在一个全局堆里),代替成千
// 上万个独立 timer。真实用途(均为 1s × 300 槽):
//
//	· collection/cache.go —— 内存缓存过期:已有 key 每次
//	  SetWithExpire 走 MoveTimer 滚动续期,新 key SetTimer;
//	· stores/cache/cleaner.go —— 缓存删除失败延迟重试:
//	  失败按 1s→5s→1m→5m→1h 退避重新入轮,关机 Drain
//	  全量执行最后一轮清理。
//
// 【钟表模型】把轮子想成只有秒针的表:numSlots 个格子,
// 指针每 interval 走一格。"3 秒后执行"= 纸条放进"当前格
// 往前 3 格"的格子里;延时超过一圈时在纸条上写"还差几圈"
// (circle),指针每次经过该格 circle--,减到 0 的那次经过
// 才执行 —— 一层轮子即可表达任意长延时。
//
// 【槽位/轮次计算】(getPositionAndCircle)
//
//	steps  = delay/interval       总步数(向下取整)
//	pos    = (指针+steps)%槽数    目标格
//	circle = (steps-1)/槽数       额外圈数
//
// steps 要减一:指针走 steps 格到达目标格,该次到达就是
// 第一次扫到,额外圈数须再减一(steps=6 恰一圈→0;7→1)。
//
// 【手推 6 槽例子】interval=1s、numSlots=6、tickedPos
// 初始=5(停在"上一圈"末槽,首个 tick 正好转到 0 号槽):
//
//	t=0    SetTimer(A,3s): steps=3 → 槽2, circle=0
//	t=0    SetTimer(B,25s): steps=25 → 槽0, circle=4
//	t=1s   tick 到槽0: B 圈数 4→3(此时剩 24s=4 圈 ✓)
//	t=2s   tick 到槽1: 空扫,看一眼空链表即返回
//	t=3s   tick 到槽2: A 到期 → runTasks 另起 goroutine
//	       异步执行(回调再慢也拖不住指针)
//	t=3.2  RemoveTimer(B): 只标 removed + 删登记,链表
//	       节点原地不动(惰性删除,免去 O(n) 遍历找节点)
//	t=3.5  SetTimer(C,4s): 锚点是最近一次 tick(t=3s 的
//	       指针)而非"现在": steps=4 → 槽0, circle=0
//	t=4~6s tick 到槽3/4/5: 空扫
//	t=7s   tick 到槽0: 同格两种任务一次扫完 —— B 已标
//	       removed,顺手摘链丢弃;C 到期执行(意图 7.5s,
//	       实际 7s 触发:锚定整格+向下取格,只会早到不会
//	       迟到,最多早接近 2 格;精度=interval)
//
// 【并发模型】所有对外操作(定时/移动/删除/取空)不直接
// 改数据,而是丢进各自的 channel,由唯一的 run goroutine
// 六路 select 串行消费(tick 到点也走同一循环)—— 单线程
// 拥有全部状态,零锁、天然无竞争;各 API 的 select 同时盯
// stopChannel,Stop 后调用立刻返回 ErrClosed 而非卡死。
// 到期任务经 runTasks 逐个 RunSafe 异步发出:单个任务
// panic 不炸指针 goroutine。
//
// 【关键结构】任务双存储:slots 按时间排(给指针扫),
// timers 按 key 查(给 API O(1) 定位),两个小结构体是
// 跨存储的粘合件:
//
//	baseEntry     任务"身份+请求"内核(仅 key+delay):
//	             嵌入 timingEntry 作字段前缀;兼作
//	             moveChannel 载荷(Move 不需 value);
//	             setTask 对已有 key 以 moveTask(
//	             task.baseEntry) 把 Set 降级为 Move。
//	positionEntry timers 登记项:pos 记旧槽号(供 moveTask
//	             选分支、算 diff),item 指针指回链表节点
//	             —— Remove/Move 原改 removed/circle/diff
//	             免扫全轮;drainAll 靠 timer.item==task
//	             指针比对,防误删重挂后的新登记。
//	slots    每格一条双向链表(同槽多个任务);
//	timers   key → positionEntry(SafeMap;读写实际全在
//	         run 内,SafeMap 属防御性余量);
//	removed  惰性删除标记:摘链表交给扫描时顺手做;
//	diff     Move 时的"还差几格到新槽",扫描到时顺手挪槽
//	         (setTimerPosition 重登记)。
//
// 【设计取舍】单层 + 轮次即可覆盖任意延时(1 小时也只
// circle≈11),省去 Kafka/Netty 式层级轮复杂度;精度 =
// interval,缓存清理不在乎秒级误差。ticker 可注入
// FakeTicker,测试可手动拨指针精确控时(cachenode_test.go)。
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

	// baseEntry 任务"身份+请求"内核(仅 key+delay):
	// 嵌入 timingEntry 作字段前缀;兼作 moveChannel 载荷
	// (Move 不需 value);setTask 对已有 key 以
	// moveTask(task.baseEntry) 把 Set 降级为 Move 复用。
	baseEntry struct {
		delay time.Duration
		key   any
	}

	// positionEntry timers 登记项:回答"key 的任务在哪"。
	// item 指针指回槽链表节点,Remove/Move 经 timers O(1)
	// 定位后原改字段,免扫全轮;pos 记旧槽号,供 moveTask
	// 选分支、算 diff。item 必须是指针:写要穿透到链表,
	// 重挂时要原位换新对象(drainAll 的 timer.item==task
	// 指针比对即防误删新登记)。
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
		// 边遍历边摘节点的三步式,顺序不能乱:
		//   ① next := e.Next()  趁链未断先备份下一节点 ——
		//      标准库 Remove 会把 e 的 next/prev 清成 nil
		//      (防内存泄漏),之后 e.Next() 只得 nil;
		//   ② Remove(e) 摘节点;
		//   ③ e = next 用备份推进 —— for 头的 init
		//      (slot.Front()) 只执行一次、post 为空,e 的
		//      前进全靠这行(没它就卡死在首节点)。
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

// moveTask 把任务挪到新延时(只在 run goroutine 内执行)。
// 记账原则:总预算 steps 格,先扣掉必经段 —— 任务躺在旧槽,
// 扣圈/搬家都只能等指针扫到旧槽,而指针走到旧槽还需 a 格;
// 余下 steps-a 格再折算成"整圈进 circle、尾程进 diff",触发
// 时刻才恰好等于 steps。不能直接抄 SetTimer 的 (steps-1)/N
// 圈数公式:那是"从指针位置新挂任务"的配套公式,漏掉 a 会
// 使误差恒为一圈(指针已过旧槽→晚一圈,未过且目标绕后→
// 早一圈)。
//
//	新 delay < 一格:立即执行(等不到下格了);
//	steps < a:意图触发早于指针下次扫到旧槽,diff 无从落地
//	  —— 旧条目标 removed,新条目按 SetTimer 同款公式立即
//	  重挂(几何精确);
//	steps >= a:circle=(steps-a)/槽数,diff=(steps-a)%槽数,
//	  扫描时先扣圈、扣完挪槽。
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

	steps := int(task.delay / tw.interval)
	// 指针走到旧槽还需 a 格(取值 1..numSlots),三种情况:
	var a int
	if tw.tickedPos > timer.pos {
		// 旧槽在指针"后方"(数值更小):继续往前走,绕过
		// 表盘起点回卷 —— 一整圈减去两槽间隔。
		a = tw.numSlots - (tw.tickedPos - timer.pos)
	} else if tw.tickedPos == timer.pos {
		// 指针正踩在旧槽上:这格刚被扫过,再见到它
		// 要等整整一圈。
		a = tw.numSlots
	} else {
		// 旧槽在指针"前方":直走差值即可。
		a = timer.pos - tw.tickedPos
	}
	// 上面三个分支可以压缩成一行(相差整圈的数 mod N 后归一,
	// 再用 -1/+1 把 mod 的输出区间 [0,N-1] 平移成 [1,N]),
	// 嫌分支长时可用等价的一行式:
	// a = (timer.pos-tw.tickedPos-1+tw.numSlots)%tw.numSlots + 1
	if steps < a {
		// 意图触发早于下次扫到旧槽:只能立即重挂。
		timer.item.removed = true
		newItem := &timingEntry{
			baseEntry: task,
			value:     timer.item.value,
		}
		pos, circle := tw.getPositionAndCircle(task.delay)
		newItem.circle = circle
		tw.slots[pos].PushBack(newItem)
		tw.setTimerPosition(pos, newItem)
		return
	}

	// 余下 steps-a 格:整圈进 circle,尾程进 diff。
	timer.item.circle = (steps - a) / tw.numSlots
	timer.item.diff = (steps - a) % tw.numSlots
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
//	circle>0:轮次减一,留下等后续圈(节点不摘,无需
//	         备份 next,e.Next() 直接前进);
//	diff>0:挪到"当前槽+diff"的新位置(Move 的落地);
//	其余:到期 —— 收集待执行,摘链表、清登记。
//
// 摘节点的分支都用 drainAll 同款三步式(先备份 next →
// Remove → e=next):标准库 Remove 会清指针,而 for 头的
// init 只执行一次,推进只能靠备份,不能靠 e.Next()。
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
