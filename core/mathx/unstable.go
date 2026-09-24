// ————————————————————————————————————————————————————————————————————————————
// unstable —— 围绕基准值的随机抖动发生器 —— 文件总结
//
// 给定 deviation(0~1),生成 [base·(1-deviation),
// base·(1+deviation)] 区间内的随机值(Duration 或 int64)。
// 用途:给定时/重试等周期加随机抖动,避免集群同频共振
// (如所有实例同一毫秒一起重试/一起过期 —— 惊群)。
// 公式拆解:1+deviation-2·deviation·rand() 在
// [1-deviation, 1+deviation] 上均匀分布,再乘 base。
// ————————————————————————————————————————————————————————————————————————————
package mathx

import (
	"math/rand"
	"sync"
	"time"
)

// An Unstable is used to generate random value around the mean value based on given deviation.
// 抖动发生器:围绕基准值按偏差随机浮动。
type Unstable struct {
	// deviation 抖动幅度,构造时钳制到 [0,1]。
	deviation float64
	// r 非并发安全的随机源,配锁。
	r    *rand.Rand
	lock *sync.Mutex
}

// NewUnstable returns an Unstable.
// 创建抖动发生器:偏差钳制到 [0,1]。
func NewUnstable(deviation float64) Unstable {
	if deviation < 0 {
		deviation = 0
	}
	if deviation > 1 {
		deviation = 1
	}
	return Unstable{
		deviation: deviation,
		r:         rand.New(rand.NewSource(time.Now().UnixNano())),
		lock:      new(sync.Mutex),
	}
}

// AroundDuration returns a random duration with given base and deviation.
// 围绕 base 的随机时长:[base·(1-d), base·(1+d)] 均匀分布。
func (u Unstable) AroundDuration(base time.Duration) time.Duration {
	u.lock.Lock()
	val := time.Duration((1 + u.deviation - 2*u.deviation*u.r.Float64()) * float64(base))
	u.lock.Unlock()
	return val
}

// AroundInt returns a random int64 with given base and deviation.
// 围绕 base 的随机整数,区间同 AroundDuration。
func (u Unstable) AroundInt(base int64) int64 {
	u.lock.Lock()
	val := int64((1 + u.deviation - 2*u.deviation*u.r.Float64()) * float64(base))
	u.lock.Unlock()
	return val
}
