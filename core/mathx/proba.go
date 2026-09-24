// ————————————————————————————————————————————————————————————————————————————
// proba —— 概率命中器 —— 文件总结
//
// TrueOnProba(p):每次调用以概率 p 返回 true。
// 典型用途:灰度/抽样/压测抖动(如负载均衡里按概率放大小
// 请求、降级开关按比例生效)。rand.Rand 非并发安全,配 Mutex。
// ————————————————————————————————————————————————————————————————————————————
package mathx

import (
	"math/rand"
	"sync"
	"time"
)

// A Proba is used to test if true on given probability.
// 概率命中器:按给定概率随机返回真假。
type Proba struct {
	// rand.New(...) returns a non thread safe object
	// rand.Rand 非并发安全,必须配锁使用。
	r    *rand.Rand
	lock sync.Mutex
}

// NewProba returns a Proba.
// 创建概率命中器(时间作种子)。
func NewProba() *Proba {
	return &Proba{
		r: rand.New(rand.NewSource(time.Now().UnixNano())),
	}
}

// TrueOnProba checks if true on given probability.
// 以概率 proba 返回 true(Uniform(0,1) < proba)。
func (p *Proba) TrueOnProba(proba float64) (truth bool) {
	p.lock.Lock()
	truth = p.r.Float64() < proba
	p.lock.Unlock()
	return
}
