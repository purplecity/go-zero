// ————————————————————————————————————————————————————————————————————————————
// pool —— 带上限与过期的对象池 —— 文件总结
//
// 与 sync.Pool 的三点区别(原注释同义):
//  1. 有资源上限:池空且达到 limit 时 Get 阻塞等待(sync.Pool 无上限);
//  2. 可设最大存活时间 maxAge:取出的对象太旧会销毁重建;
//  3. 资源的创建/销毁函数由调用方定制(如数据库连接的建/断)。
//
// 数据结构:链表作空闲栈(head 指向栈顶,Put 头插、Get 头取,LIFO);
// created 记录"已创建未销毁"的数量,防止超过 limit。
// 并发模型:一把 Mutex + sync.Cond —— Get 拿不到时 Wait,
// Put 后 Signal 唤醒一个等待者。典型用途:redis/mysql 连接池。
// ————————————————————————————————————————————————————————————————————————————
package syncx

import (
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/timex"
)

type (
	// PoolOption defines the method to customize a Pool.
	// 池的函数式选项(目前只有 WithMaxAge)。
	PoolOption func(*Pool)

	// node 空闲链表节点:item 是资源本体,next 串成链,
	// lastUsed 记录归还时刻(判断 maxAge 用)。
	node struct {
		item     any
		next     *node
		lastUsed time.Duration
	}

	// A Pool is used to pool resources.
	// The difference between sync.Pool is that:
	//  1. the limit of the resources
	//  2. max age of the resources can be set
	//  3. the method to destroy resources can be customized
	// 对象池,三个字段组:容量与计数(limit/created)、同步原语
	// (lock/cond)、空闲链表与建/毁函数(head/create/destroy)。
	Pool struct {
		// limit 最大资源数(含借出中的)。
		limit int
		// created 已创建且未销毁的资源数,created == limit 时 Get 等待。
		created int
		// maxAge 资源最大存活时长;0 表示不过期。
		maxAge time.Duration
		// lock 互斥锁(cond 的底层锁),保护以下全部字段。
		lock sync.Locker
		cond *sync.Cond
		// head 空闲链表栈顶(nil 表示池里没有空闲资源)。
		head *node
		// create 资源不够时的创建函数;destroy 销毁函数。
		create  func() any
		destroy func(any)
	}
)

// NewPool returns a Pool.
// 创建对象池:n 为容量上限(必须 > 0),
// create/destroy 分别为建/毁资源的回调。
func NewPool(n int, create func() any, destroy func(any), opts ...PoolOption) *Pool {
	if n <= 0 {
		panic("pool size can't be negative or zero")
	}

	// lock 与 cond 必须共用同一把锁,sync.NewCond 帮忙绑定。
	lock := new(sync.Mutex)
	pool := &Pool{
		limit:   n,
		lock:    lock,
		cond:    sync.NewCond(lock),
		create:  create,
		destroy: destroy,
	}

	for _, opt := range opts {
		opt(pool)
	}

	return pool
}

// Get gets a resource.
// 获取一个资源,for 循环内三选一:
//  1. 池里有空闲:取栈顶;若超过 maxAge 则销毁并继续循环找下一个;
//  2. 还没建满(limit 内):created++ 并新建一个;
//  3. 都不满足:cond.Wait() 挂起,等别人 Put 后唤醒重来。
func (p *Pool) Get() any {
	p.lock.Lock()
	defer p.lock.Unlock()

	for {
		if p.head != nil {
			head := p.head
			p.head = head.next
			if p.maxAge > 0 && head.lastUsed+p.maxAge < timex.Now() {
				// 空闲太久:销毁,腾出配额后继续找/建新的。
				p.created--
				p.destroy(head.item)
				continue
			} else {
				return head.item
			}
		}

		if p.created < p.limit {
			p.created++
			return p.create()
		}

		// 池空且已建满:挂起等待 Put 的 Signal 唤醒。
		p.cond.Wait()
	}
}

// Put puts a resource back.
// 归还资源:头插入空闲链表并记录归还时刻,唤醒一个等待者。
// nil 直接忽略(可能是create失败等的占位归还)。
func (p *Pool) Put(x any) {
	if x == nil {
		return
	}

	p.lock.Lock()
	defer p.lock.Unlock()

	p.head = &node{
		item:     x,
		next:     p.head,
		lastUsed: timex.Now(),
	}
	p.cond.Signal()
}

// WithMaxAge returns a function to customize a Pool with given max age.
// 选项:设置资源最大存活时长,超过后取出即销毁重建。
func WithMaxAge(duration time.Duration) PoolOption {
	return func(pool *Pool) {
		pool.maxAge = duration
	}
}
