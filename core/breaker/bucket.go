// ————————————————————————————————————————————————————————————————————————————
// bucket —— 滚动窗口的账页(成功/失败/丢弃三本账) —— 文件总结
//
// collection.RollingWindow 的桶实现(BucketInterface):每桶
// 250ms 一页,Add 按"事件类型"分流记账,四本账一起记:
//
//	Sum     总请求数 = 成功+失败+丢弃(googleBreaker 公式的分母);
//	Success 成功数(分子;连续成功桶是恢复的依据);
//	Failure 失败数(连续失败桶是收紧的依据);
//	Drop    被断路器丢弃数 —— 拒绝也要记账:丢掉的请求压低
//	        放行率,否则丢得越多窗口越"干净"、恢复越快,
//	        熔断就抖动了。
//
// 事件常量 success=0 恰好落进 Add 的 default 分支:传错值也
// 按成功记,宁松勿紧。
// ————————————————————————————————————————————————————————————
package breaker

// 事件类型:Add 记账的分流依据(0/1/2)。
const (
	success = iota
	fail
	drop
)

// bucket defines the bucket that holds sum and num of additions.
// 一页账:四本账同步记(分工见文件头)。
type bucket struct {
	Sum     int64
	Success int64
	Failure int64
	Drop    int64
}

// Add 按事件类型分流记账。
func (b *bucket) Add(v int64) {
	switch v {
	case fail:
		b.fail()
	case drop:
		b.drop()
	default:
		b.succeed()
	}
}

// Reset 清零(桶被时间轮转重用时调用,机制见 rollingwindow)。
func (b *bucket) Reset() {
	b.Sum = 0
	b.Success = 0
	b.Failure = 0
	b.Drop = 0
}

// drop 记一笔丢弃:Sum 与 Drop 同增。
func (b *bucket) drop() {
	b.Sum++
	b.Drop++
}

// fail 记一笔失败:Sum 与 Failure 同增。
func (b *bucket) fail() {
	b.Sum++
	b.Failure++
}

// succeed 记一笔成功:Sum 与 Success 同增。
func (b *bucket) succeed() {
	b.Sum++
	b.Success++
}
