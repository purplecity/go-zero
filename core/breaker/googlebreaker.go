// ————————————————————————————————————————————————————————————————————————————
// googlebreaker —— 熔断算法核心:Google SRE 客户端自适应限流 —— 文件总结
//
// 不是经典"闭合/断开/半开"三态机,而是 SRE 书 Client-Side
// Throttling 的连续概率版:每次请求实时算一个丢弃概率,按
// 概率掷骰子 —— 没有状态跳变,熔断与恢复都是平滑斜坡。
//
// 统计底盘:10s × 40 桶(每桶 250ms)滚动窗口,账页格式见
// bucket.go,列车模型见 collection/rollingwindow.go。
//
// 【accept() 五步流水线】
//
//	① history() 聚合窗口 → accepts(成功)/total(总请求)/
//	   failingBuckets(连续故障桶数)/workingBuckets(连续
//	   正常桶数)—— 两个都是"尾段连击",最近一次反方即清零;
//	② SRE 公式算容忍额度:
//	     w = k − (k−minK)×failingBuckets/40   (故障连击越久
//	       容忍系数越小:1.5 → 1.1,越来越严)
//	     dropRatio = (total − protection − w×accepts)/(total+1)
//	   直觉:把观察到的成功数放大 w 倍当"可放行额度",请求
//	   超出额度就按超出比例丢;protection=5 是低流量豁免
//	   (总请求太少时不丢,防冷启动误杀);
//	③ dropRatio ≤ 0 → 放行;
//	④ 半开探测:距上次放行超 1s → 强制放行一个探路 —— 全断
//	   状态下每秒漏出一个请求试探恢复,等价于经典断路器
//	   的 half-open,只是没有显式状态;
//	⑤ 恢复软化:dropRatio × 非正常桶占比 —— 连续正常桶越多
//	   丢得越少(40 桶全正常时系数为 0,全放行),恢复期
//	   平滑放开无悬崖;最后按概率丢弃。
//
// 拒绝统一返回 ErrServiceUnavailable,由外层 loggedThrottle
// (breaker.go)记最近错误日志并上报。
// ————————————————————————————————————————————————————————————————————————————
package breaker

import (
	"time"

	"github.com/zeromicro/go-zero/core/collection"
	"github.com/zeromicro/go-zero/core/mathx"
	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/core/timex"
)

const (
	// 250ms for bucket duration
	// 统计窗口 10 秒。
	window = time.Second * 10
	// 40 桶 → 每桶 250ms。
	buckets = 40
	// forcePassDuration 半开探测间隔:全断状态下每 1s 强制放行一个。
	forcePassDuration = time.Second
	// k 容忍系数:成功数 × k 当作可放行额度(SRE 书公式)。
	k = 1.5
	// minK k 的下限:故障连击把 k 从 1.5 压向 1.1,越连越严。
	minK = 1.1
	// protection 低流量豁免:总请求 ≤5 时分子为负 → 不丢(防冷启动误杀)。
	protection = 5
)

// googleBreaker is a netflixBreaker pattern from google.
// see Client-Side Throttling section in https://landing.google.com/sre/sre-book/chapters/handling-overload/
type (
	googleBreaker struct {
		// k 容忍系数(每个实例固定取常量 k,预留成字段便于实验)。
		k float64
		// stat 滚动窗口统计底盘。
		stat *collection.RollingWindow[int64, *bucket]
		// proba 按概率掷骰子(丢弃判定)。
		proba *mathx.Proba
		// lastPass 上次放行时刻(半开探测的间隔基准)。
		lastPass *syncx.AtomicDuration
	}

	// windowResult 一次 Reduce 聚合出的窗口快照。
	windowResult struct {
		// accepts 窗口内成功数(公式的分子素材)。
		accepts int64
		// total 窗口内总请求数 = 成功+失败+丢弃(公式的分母)。
		total int64
		// failingBuckets 连续"只失败"的桶数(尾段连击)。
		failingBuckets int64
		// workingBuckets 连续"只成功"的桶数(尾段连击)。
		workingBuckets int64
	}
)

func newGoogleBreaker() *googleBreaker {
	bucketDuration := time.Duration(int64(window) / int64(buckets))
	st := collection.NewRollingWindow[int64, *bucket](func() *bucket {
		return new(bucket)
	}, buckets, bucketDuration)
	return &googleBreaker{
		stat:     st,
		k:        k,
		proba:    mathx.NewProba(),
		lastPass: syncx.NewAtomicDuration(),
	}
}

// accept 判定本次请求放行还是丢弃(五步流水线,见文件头)。
func (b *googleBreaker) accept() error {
	var w float64
	history := b.history()
	// ① 容忍系数:故障连击越久,k 越小(1.5 → 1.1),额度越紧。
	w = b.k - (b.k-minK)*float64(history.failingBuckets)/buckets
	// ② SRE 公式:成功数 × w 当"容忍额度",超出部分按比例丢。
	weightedAccepts := mathx.AtLeast(w, minK) * float64(history.accepts)
	// https://landing.google.com/sre/sre-book/chapters/handling-overload/#eq2101
	// for better performance, no need to care about the negative ratio
	// protection 豁免藏在分子:total≤5 时分子为负 → 下方直接放行。
	dropRatio := (float64(history.total-protection) - weightedAccepts) / float64(history.total+1)
	if dropRatio <= 0 {
		return nil
	}

	// ③(文件头流水线的 ④)半开探测:距上次放行超 1s,
	// 强制放行一个请求探路。
	lastPass := b.lastPass.Load()
	if lastPass > 0 && timex.Since(lastPass) > forcePassDuration {
		b.lastPass.Set(timex.Now())
		return nil
	}

	// ④ 恢复软化:连续正常桶越多,丢弃率按比例缩小
	//    (workingBuckets=40 → 系数 0,全放行)。
	dropRatio *= float64(buckets-history.workingBuckets) / buckets

	// ⑤ 按概率丢弃(掷骰子)。
	if b.proba.TrueOnProba(dropRatio) {
		return ErrServiceUnavailable
	}

	// 放行则记录放行时刻(半开探测的间隔基准)。
	b.lastPass.Set(timex.Now())

	return nil
}

// allow 放行判定 + 发通行证(Allow 模式入口)。
func (b *googleBreaker) allow() (internalPromise, error) {
	if err := b.accept(); err != nil {
		b.markDrop()
		return nil, err
	}

	return googlePromise{
		b: b,
	}, nil
}

// doReq Do 模式入口:判放 → 拒绝走 fallback / 放行执行并记账。
func (b *googleBreaker) doReq(req func() error, fallback Fallback, acceptable Acceptable) error {
	if err := b.accept(); err != nil {
		b.markDrop()
		if fallback != nil {
			return fallback(err)
		}

		return err
	}

	var succ bool
	defer func() {
		// if req() panic, success is false, mark as failure
		// panic 也按失败记账再重抛(succ 未及置位)。
		if succ {
			b.markSuccess()
		} else {
			b.markFailure()
		}
	}()

	err := req()
	// acceptable 放宽"失败"定义:业务可接受错误(如 404)
	// 不算故障,仍记成功。
	if acceptable(err) {
		succ = true
	}

	return err
}

// markDrop 记一笔丢弃(拒绝也进窗口,压低放行率,见 bucket.go)。
func (b *googleBreaker) markDrop() {
	b.stat.Add(drop)
}

// markFailure 记一笔失败。
func (b *googleBreaker) markFailure() {
	b.stat.Add(fail)
}

// markSuccess 记一笔成功。
func (b *googleBreaker) markSuccess() {
	b.stat.Add(success)
}

// history 聚合滚动窗口(Reduce 从旧到新遍历,方向关键 ——
// 连击计数依赖扫描顺序):
//
//	accepts/total:窗口内成功数与总请求数;
//	failingBuckets:遇"有成功"的桶清零、遇"只失败"的桶 +1、
//	  其余桶(空/纯 drop)不动 —— 扫完后 = 离现在最近的
//	  一段故障连击长度;
//	workingBuckets:对称逻辑,连续"只成功"的尾段连击。
//
// 这是"尾段连击"而非"窗口内失败桶总数":只要最近有过成功,
// 故障连击立刻归零重来 —— 只盯"现在还在坏吗",不看"曾经坏多久"。
func (b *googleBreaker) history() windowResult {
	var result windowResult

	b.stat.Reduce(func(b *bucket) {
		result.accepts += b.Success
		result.total += b.Sum
		if b.Failure > 0 {
			result.workingBuckets = 0
		} else if b.Success > 0 {
			result.workingBuckets++
		}
		if b.Success > 0 {
			result.failingBuckets = 0
		} else if b.Failure > 0 {
			result.failingBuckets++
		}
	})

	return result
}

// googlePromise Allow 模式的通行证:回报成功/失败即记账。
type googlePromise struct {
	b *googleBreaker
}

func (p googlePromise) Accept() {
	p.b.markSuccess()
}

func (p googlePromise) Reject() {
	p.b.markFailure()
}
