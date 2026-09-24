// ————————————————————————————————————————————————————————————————————————————
// adaptiveshedder —— 自适应负载脱落(BBRA 思路) —— 文件总结
//
// 目标:系统过载时主动丢请求(返回 ErrServiceOverloaded),
// 保护自己不被压垮,且尽量多服务。判定链条:
//
//	shouldDrop = (CPU 过载 或 冷却期内) 且 highThru
//
// 高低全靠滚动窗口里的两个历史指标自适应,没有写死阈值:
//
//	maxFlight(允许的最大并发) = maxPass × minRt × windowScale
//	  maxPass —— 5s 窗口内单桶最大通过数(历史峰值处理能力);
//	  minRt   —— 窗口内最小的平均响应时间(ms);
//	  windowScale —— 每秒桶数/1000,把"每桶通过数"换算成 QPS。
//	  本质是利特尔法则 L = λ×W:允许并发 = 峰值 QPS × 最快 RT。
//	  用"最小 RT"是因为过载时 RT 会膨胀,只有最健康时刻的
//	  RT 才反映真实处理能力。
//
//	overloadFactor = (1000-当前CPU)/(1000-阈值),夹在 [0.1,1]
//	  —— CPU 越接近上限,放行额度越线性收紧,但至少放 10%
//	  (给探测流量留缝,否则永远无法发现已经恢复)。
//
//	avgFlying —— 在飞请求数的 EWMA(β=0.9),只在请求结束时
//	  更新:比 flying 平滑滞后。骤增时 avg 涨得慢(多接请求),
//	  骤降时 avg 落得慢(少接请求),尽量榨干服务能力。
//	  highThru 要求 avg 与瞬时值同时超限,防单边抖动。
//
//	冷却期 —— 丢过请求后 1s 内仍按"热"处理(stillHot),
//	  防止 CPU 刚回落就立刻放开造成抖动。
//
// ————————————————————————————————————————————————————————————————————————————
package load

import (
	"errors"
	"fmt"
	"math"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/collection"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/mathx"
	"github.com/zeromicro/go-zero/core/stat"
	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/core/timex"
)

const (
	// defaultBuckets 滚动窗口默认桶数(5s/50 = 每桶 100ms)。
	defaultBuckets = 50
	// defaultWindow 滚动窗口默认时长。
	defaultWindow = time.Second * 5
	// using 1000m notation, 900m is like 90%, keep it as var for unit test
	// 默认 CPU 阈值 900m(千分之 900,即 90%)。
	defaultCpuThreshold = 900
	// defaultMinRt 无样本时的默认 RT(1s):足够大,避免冷启动丢请求。
	defaultMinRt = float64(time.Second / time.Millisecond)
	// moving average hyperparameter beta for calculating requests on the fly
	// avgFlying 的 EWMA 平滑系数。
	flyingBeta = 0.9
	// coolOffDuration 丢过请求后的冷却期(期间继续按过载处理)。
	coolOffDuration = time.Second
	// cpuMax CPU 满载值(千分制)。
	cpuMax = 1000 // millicpu
	// millisecondsPerSecond 秒→毫秒换算。
	millisecondsPerSecond = 1000
	// overloadFactorLowerBound 过载因子下限(至少放行 10%)。
	overloadFactorLowerBound = 0.1
)

var (
	// ErrServiceOverloaded is returned by Shedder.Allow when the service is overloaded.
	// 过载被拒时返回的错误。
	ErrServiceOverloaded = errors.New("service overloaded")

	// default to be enabled
	// 全局开关(默认开,Disable() 关闭)。
	enabled = syncx.ForAtomicBool(true)
	// default to be enabled
	// 统计日志开关。
	logEnabled = syncx.ForAtomicBool(true)
	// make it a variable for unit test
	// 过载判定函数(可替换以便测试注入)。
	systemOverloadChecker = func(cpuThreshold int64) bool {
		return stat.CpuUsage() >= cpuThreshold
	}
)

type (
	// A Promise interface is returned by Shedder.Allow to let callers tell
	// whether the processing request is successful or not.
	// 允诺:请求处理完必须调 Pass(成功)或 Fail(失败)之一,
	// shedder 靠它回收在飞计数并统计 RT。
	Promise interface {
		// Pass lets the caller tell that the call is successful.
		// 成功:减在飞 + RT/通过数计入窗口。
		Pass()
		// Fail lets the caller tell that the call is failed.
		// 失败:仅减在飞(失败样本不计入能力统计)。
		Fail()
	}

	// Shedder is the interface that wraps the Allow method.
	// 负载脱落器:Allow 放行返回 Promise,过载返回错误。
	Shedder interface {
		// Allow returns the Promise if allowed, otherwise ErrServiceOverloaded.
		// 尝试准入。
		Allow() (Promise, error)
	}

	// ShedderOption lets caller customize the Shedder.
	// 脱落器选项。
	ShedderOption func(opts *shedderOptions)

	shedderOptions struct {
		// window 统计窗口时长。
		window time.Duration
		// buckets 窗口桶数。
		buckets int
		// cpuThreshold 触发过载的 CPU 阈值(千分制)。
		cpuThreshold int64
	}

	// adaptiveShedder 自适应脱落器(见文件头算法说明)。
	adaptiveShedder struct {
		// cpuThreshold CPU 过载阈值。
		cpuThreshold int64
		// windowScale 每秒桶数/1000:桶通过数 → QPS 的换算系数。
		windowScale float64
		// flying 当前在飞请求数(原子)。
		flying int64
		// avgFlying 在飞数的 EWMA 平滑值(配自旋锁,避免原子浮点)。
		avgFlying float64
		// avgFlyingLock 保护 avgFlying 的自旋锁。
		avgFlyingLock syncx.SpinLock
		// overloadTime 最近一次判定过载的时间戳(冷却期用)。
		overloadTime *syncx.AtomicDuration
		// droppedRecently 最近是否丢过请求(冷却期标记)。
		droppedRecently *syncx.AtomicBool
		// passCounter 通过数滚动窗口(取单桶峰值 maxPass)。
		passCounter *collection.RollingWindow[int64, *collection.Bucket[int64]]
		// rtCounter 响应时间滚动窗口(取最小桶均值 minRt)。
		rtCounter *collection.RollingWindow[int64, *collection.Bucket[int64]]
	}
)

// Disable lets callers disable load shedding.
// 全局关闭负载脱落(NewAdaptiveShedder 将返回空实现)。
func Disable() {
	enabled.Set(false)
}

// DisableLog disables the stat logs for load shedding.
// 关闭脱落统计日志。
func DisableLog() {
	logEnabled.Set(false)
}

// NewAdaptiveShedder returns an adaptive shedder.
// opts can be used to customize the Shedder.
// 创建自适应脱落器;全局 Disable 后返回空实现(nopShedder)。
// 两个窗口都带 IgnoreCurrentBucket:当前桶未写满,统计它
// 会低估能力导致误杀,故只看已完整的桶。
func NewAdaptiveShedder(opts ...ShedderOption) Shedder {
	if !enabled.True() {
		return newNopShedder()
	}

	options := shedderOptions{
		window:       defaultWindow,
		buckets:      defaultBuckets,
		cpuThreshold: defaultCpuThreshold,
	}
	for _, opt := range opts {
		opt(&options)
	}
	bucketDuration := options.window / time.Duration(options.buckets)
	newBucket := func() *collection.Bucket[int64] {
		return new(collection.Bucket[int64])
	}
	return &adaptiveShedder{
		cpuThreshold:    options.cpuThreshold,
		windowScale:     float64(time.Second) / float64(bucketDuration) / millisecondsPerSecond,
		overloadTime:    syncx.NewAtomicDuration(),
		droppedRecently: syncx.NewAtomicBool(),
		passCounter:     collection.NewRollingWindow[int64, *collection.Bucket[int64]](newBucket, options.buckets, bucketDuration, collection.IgnoreCurrentBucket[int64, *collection.Bucket[int64]]()),
		rtCounter:       collection.NewRollingWindow[int64, *collection.Bucket[int64]](newBucket, options.buckets, bucketDuration, collection.IgnoreCurrentBucket[int64, *collection.Bucket[int64]]()),
	}
}

// Allow implements Shedder.Allow.
// 准入:该丢则丢(记冷却标记并返回过载错误);
// 放行则登记在飞,返回 promise(调用方完事必须 Pass/Fail)。
func (as *adaptiveShedder) Allow() (Promise, error) {
	if as.shouldDrop() {
		as.droppedRecently.Set(true)

		return nil, ErrServiceOverloaded
	}

	as.addFlying(1)

	return &promise{
		start:   timex.Now(),
		shedder: as,
	}, nil
}

// addFlying 调整在飞计数;请求结束(delta<0)时顺带
// 用 EWMA 更新 avgFlying。
func (as *adaptiveShedder) addFlying(delta int64) {
	flying := atomic.AddInt64(&as.flying, delta)
	// update avgFlying when the request is finished.
	// this strategy makes avgFlying have a little bit of lag against flying, and smoother.
	// when the flying requests increase rapidly, avgFlying increase slower, accept more requests.
	// when the flying requests drop rapidly, avgFlying drop slower, accept fewer requests.
	// it makes the service to serve as many requests as possible.
	// 只在减少时更新:让 avgFlying 略滞后于瞬时值 —— 突增时
	// 慢涨(多放请求进来),骤降时慢落(少放),榨干服务能力。
	if delta < 0 {
		as.avgFlyingLock.Lock()
		as.avgFlying = as.avgFlying*flyingBeta + float64(flying)*(1-flyingBeta)
		as.avgFlyingLock.Unlock()
	}
}

// highThru 是否高吞吐:平滑值与瞬时值【同时】超过
// maxFlight×overloadFactor 才算 —— 双保险防抖动。
func (as *adaptiveShedder) highThru() bool {
	as.avgFlyingLock.Lock()
	avgFlying := as.avgFlying
	as.avgFlyingLock.Unlock()
	maxFlight := as.maxFlight() * as.overloadFactor()
	return avgFlying > maxFlight && float64(atomic.LoadInt64(&as.flying)) > maxFlight
}

// maxFlight 计算允许的最大并发(利特尔法则):
//
//	windows   = 每秒桶数(把每桶通过数放大成 QPS)
//	maxQPS    = maxPass × windows
//	allowed   = maxQPS × minRt(ms) / 1000
//
// 下限 1:窗口没数据时不至于一个都不放。
func (as *adaptiveShedder) maxFlight() float64 {
	// windows = buckets per second
	// maxQPS = maxPASS * windows
	// minRT = min average response time in milliseconds
	// allowedFlying = maxQPS * minRT / milliseconds_per_second
	maxFlight := float64(as.maxPass()) * as.minRt() * as.windowScale
	return mathx.AtLeast(maxFlight, 1)
}

// maxPass 窗口内单桶最大通过数(历史峰值处理能力)。
func (as *adaptiveShedder) maxPass() int64 {
	var result int64 = 1

	as.passCounter.Reduce(func(b *collection.Bucket[int64]) {
		if b.Sum > result {
			result = b.Sum
		}
	})

	return result
}

// minRt 窗口内最小的桶平均响应时间(ms):
// 过载时 RT 膨胀,取最小值才是"健康时的处理速度"。
func (as *adaptiveShedder) minRt() float64 {
	// if no requests in previous windows, return defaultMinRt,
	// its a reasonable large value to avoid dropping requests.
	// 无样本时用默认 1s:宁可放行不可误杀。
	result := defaultMinRt

	as.rtCounter.Reduce(func(b *collection.Bucket[int64]) {
		if b.Count <= 0 {
			return
		}

		avg := math.Round(float64(b.Sum) / float64(b.Count))
		if avg < result {
			result = avg
		}
	})

	return result
}

// overloadFactor 过载放行系数:CPU 越高额度越小,
// 线性从 1 收紧到下限 0.1(至少放 10%,留探测恢复的缝)。
func (as *adaptiveShedder) overloadFactor() float64 {
	// as.cpuThreshold must be less than cpuMax
	factor := (cpuMax - float64(stat.CpuUsage())) / (cpuMax - float64(as.cpuThreshold))
	// at least accept 10% of acceptable requests, even cpu is highly overloaded.
	return mathx.Between(factor, overloadFactorLowerBound, 1)
}

// shouldDrop 丢弃判定:(过载 或 冷却期内)且高吞吐才丢;
// 丢弃时打点日志 + stat 上报(带当时全部指标,便于排查)。
func (as *adaptiveShedder) shouldDrop() bool {
	if as.systemOverloaded() || as.stillHot() {
		if as.highThru() {
			flying := atomic.LoadInt64(&as.flying)
			as.avgFlyingLock.Lock()
			avgFlying := as.avgFlying
			as.avgFlyingLock.Unlock()
			msg := fmt.Sprintf(
				"dropreq, cpu: %d, maxPass: %d, minRt: %.2f, hot: %t, flying: %d, avgFlying: %.2f",
				stat.CpuUsage(), as.maxPass(), as.minRt(), as.stillHot(), flying, avgFlying)
			logx.Error(msg)
			stat.Report(msg)
			return true
		}
	}

	return false
}

// stillHot 是否仍在冷却期:丢过请求且距上次过载不足 1s
// —— 防止 CPU 刚回落就放开造成抖动;超时则清标记。
func (as *adaptiveShedder) stillHot() bool {
	if !as.droppedRecently.True() {
		return false
	}

	overloadTime := as.overloadTime.Load()
	if overloadTime == 0 {
		return false
	}

	if timex.Since(overloadTime) < coolOffDuration {
		return true
	}

	// 冷却期满,复位标记。
	as.droppedRecently.Set(false)
	return false
}

// systemOverloaded CPU 是否过载;过载时记时间戳供冷却期判断。
func (as *adaptiveShedder) systemOverloaded() bool {
	if !systemOverloadChecker(as.cpuThreshold) {
		return false
	}

	as.overloadTime.Set(timex.Now())
	return true
}

// WithBuckets customizes the Shedder with the given number of buckets.
// 选项:窗口桶数。
func WithBuckets(buckets int) ShedderOption {
	return func(opts *shedderOptions) {
		opts.buckets = buckets
	}
}

// WithCpuThreshold customizes the Shedder with the given cpu threshold.
// 选项:CPU 过载阈值(千分制)。
func WithCpuThreshold(threshold int64) ShedderOption {
	return func(opts *shedderOptions) {
		opts.cpuThreshold = threshold
	}
}

// WithWindow customizes the Shedder with given
// 选项:统计窗口时长。
func WithWindow(window time.Duration) ShedderOption {
	return func(opts *shedderOptions) {
		opts.window = window
	}
}

// promise Allow 放行时返回的允诺:记住起点时间,
// 结束时向 shedder 回报。
type promise struct {
	// start 请求开始时间(算 RT 用)。
	start time.Duration
	// shedder 所属脱落器。
	shedder *adaptiveShedder
}

// Fail 失败回报:只减在飞(失败样本不进能力统计)。
func (p *promise) Fail() {
	p.shedder.addFlying(-1)
}

// Pass 成功回报:减在飞 + RT/通过数写入滚动窗口,
// 供 maxPass/minRt 下轮计算。
func (p *promise) Pass() {
	rt := float64(timex.Since(p.start)) / float64(time.Millisecond)
	p.shedder.addFlying(-1)
	p.shedder.rtCounter.Add(int64(math.Ceil(rt)))
	p.shedder.passCounter.Add(1)
}
