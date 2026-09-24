// ————————————————————————————————————————————————————————————————————————————
// cache —— 进程内缓存(map + 时间轮过期 + 可选 LRU + 防击穿) —— 文件总结
//
// 四个组件各司其职:
//
//	data map         真实存储(锁保护);
//	timingWheel      到期自动删 key(1s 精度,300 槽);
//	lruCache         可选容量上限(WithLimit),超限逐出最久
//	                 未用的 —— 默认 emptyLru(不限容量);
//	barrier          SingleFlight 防缓存击穿:并发的同 key
//	                 Take 只放一个去 fetch,其余等结果。
//
// 两个工程细节:
//
//	  过期加抖动(±5%):防止大批 key 同一秒集中过期,
//	  造成瞬间全量回源(雪崩)—— mathx.Unstable 实现;
//	  Take 双重检查:拿到 SingleFlight 名额后再查一次缓存
//	  (前一个等结果的调用可能已经填上了)。
//	cacheStat 每分钟报 QPM/命中率(同 sheddingstat 手法)。
//
// ————————————————————————————————————————————————————————————————————————————
package collection

import (
	"container/list"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/mathx"
	"github.com/zeromicro/go-zero/core/syncx"
)

const (
	// defaultCacheName 统计报表里的默认名字。
	defaultCacheName = "proc"
	// slots 时间轮槽数(300 槽 × 1s = 5 分钟轮转周期)。
	slots = 300
	// statInterval 统计报表间隔。
	statInterval = time.Minute
	// make the expiry unstable to avoid lots of cached items expire at the same time
	// make the unstable expiry to be [0.95, 1.05] * seconds
	// 过期抖动幅度 ±5%,防同批 key 集中过期(雪崩)。
	expiryDeviation = 0.05
)

// emptyLruCache 不限容量时的空 LRU(空对象模式)。
var emptyLruCache = emptyLru{}

type (
	// CacheOption defines the method to customize a Cache.
	// 缓存选项。
	CacheOption func(cache *Cache)

	// A Cache object is an in-memory cache.
	// 进程内缓存:map 存储 + 时间轮过期 + 可选 LRU 逐出。
	Cache struct {
		// name 统计报表标识。
		name string
		// lock 保护 data 与 lruCache。
		lock sync.Mutex
		// data 真实键值存储。
		data map[string]any
		// expire 默认过期时长。
		expire time.Duration
		// timingWheel 到期删除器。
		timingWheel *TimingWheel
		// lruCache 容量逐出器(默认空实现不限容)。
		lruCache lru
		// barrier 同 key 并发回源合并(SingleFlight)。
		barrier syncx.SingleFlight
		// unstableExpiry 过期时长抖动器。
		unstableExpiry mathx.Unstable
		// stats 命中率统计。
		stats *cacheStat
	}
)

// NewCache returns a Cache with given expire.
// 创建缓存:默认过期 expire,时间轮负责到期删除
// (删除回调里只认 string key)。
func NewCache(expire time.Duration, opts ...CacheOption) (*Cache, error) {
	cache := &Cache{
		data:           make(map[string]any),
		expire:         expire,
		lruCache:       emptyLruCache,
		barrier:        syncx.NewSingleFlight(),
		unstableExpiry: mathx.NewUnstable(expiryDeviation),
	}

	for _, opt := range opts {
		opt(cache)
	}

	if len(cache.name) == 0 {
		cache.name = defaultCacheName
	}
	cache.stats = newCacheStat(cache.name, cache.size)

	timingWheel, err := NewTimingWheel(time.Second, slots, func(k, v any) {
		key, ok := k.(string)
		if !ok {
			return
		}

		cache.Del(key)
	})
	if err != nil {
		return nil, err
	}

	cache.timingWheel = timingWheel
	return cache, nil
}

// Del deletes the item with the given key from c.
// 删除:锁内删 data 和 LRU,锁外摘时间轮(摘除可能较慢,
// 不拖累锁;漏摘的定时器触发时 key 已不存在,无害)。
func (c *Cache) Del(key string) {
	c.lock.Lock()
	delete(c.data, key)
	c.lruCache.remove(key)
	c.lock.Unlock()

	// RemoveTimer is called outside the lock to avoid performance impact from this
	// potentially time-consuming operation. Data integrity is maintained by lruCache,
	// which will eventually evict any remaining entries when capacity is exceeded.
	// 锁外摘定时器:避免慢操作拖累锁;即使漏摘,到期回调
	// 删一个不存在的 key 也是无害空操作。
	c.timingWheel.RemoveTimer(key)
}

// Get returns the item with the given key from c.
// 读取(带命中/未命中计数)。
func (c *Cache) Get(key string) (any, bool) {
	value, ok := c.doGet(key)
	if ok {
		c.stats.IncrementHit()
	} else {
		c.stats.IncrementMiss()
	}

	return value, ok
}

// Set sets value into c with key.
// 写入,用默认过期时长。
func (c *Cache) Set(key string, value any) {
	c.SetWithExpire(key, value, c.expire)
}

// SetWithExpire sets value into c with key and expire with the given value.
// 写入并指定过期:过期时长加 ±5% 抖动防集中过期;
// 已有 key 用 MoveTimer 挪时间轮,新 key SetTimer 挂上。
func (c *Cache) SetWithExpire(key string, value any, expire time.Duration) {
	c.lock.Lock()
	_, ok := c.data[key]
	c.data[key] = value
	c.lruCache.add(key)
	c.lock.Unlock()

	expiry := c.unstableExpiry.AroundDuration(expire)
	if ok {
		c.timingWheel.MoveTimer(key, expiry)
	} else {
		c.timingWheel.SetTimer(key, value, expiry)
	}
}

// Take returns the item with the given key.
// If the item is in c, return it directly.
// If not, use fetch method to get the item, set into c and return it.
// 读缓存,未命中则回源:SingleFlight 合并同 key 并发回源
// (防击穿);成功结果写回缓存再返回。
func (c *Cache) Take(key string, fetch func() (any, error)) (any, error) {
	if val, ok := c.doGet(key); ok {
		c.stats.IncrementHit()
		return val, nil
	}

	var fresh bool
	val, err := c.barrier.Do(key, func() (any, error) {
		// because O(1) on map search in memory, and fetch is an IO query,
		// so we do double-check, cache might be taken by another call
		// 双重检查:等 SingleFlight 名额期间,前一个调用可能已回填。
		if val, ok := c.doGet(key); ok {
			return val, nil
		}

		v, e := fetch()
		if e != nil {
			return nil, e
		}

		fresh = true
		c.Set(key, v)
		return v, nil
	})
	if err != nil {
		return nil, err
	}

	if fresh {
		// 本调用是回源的那个:按未命中计。
		c.stats.IncrementMiss()
		return val, nil
	}

	// got the result from previous ongoing query
	// 等来了别人回源的结果:按命中计。
	c.stats.IncrementHit()
	return val, nil
}

// doGet 无统计版读取:命中则顺带刷新 LRU 热度。
func (c *Cache) doGet(key string) (any, bool) {
	c.lock.Lock()
	defer c.lock.Unlock()

	value, ok := c.data[key]
	if ok {
		c.lruCache.add(key)
	}

	return value, ok
}

// onEvict LRU 逐出回调(调用时已持锁):删数据 + 摘时间轮。
func (c *Cache) onEvict(key string) {
	// already locked
	delete(c.data, key)
	c.timingWheel.RemoveTimer(key)
}

// size 当前条数(统计报表用)。
func (c *Cache) size() int {
	c.lock.Lock()
	defer c.lock.Unlock()
	return len(c.data)
}

// WithLimit customizes a Cache with items up to limit.
// 选项:容量上限,超限逐出最久未用(LRU)。
func WithLimit(limit int) CacheOption {
	return func(cache *Cache) {
		if limit > 0 {
			cache.lruCache = newKeyLru(limit, cache.onEvict)
		}
	}
}

// WithName customizes a Cache with the given name.
// 选项:统计报表里的名字。
func WithName(name string) CacheOption {
	return func(cache *Cache) {
		cache.name = name
	}
}

type (
	// lru LRU 抽象:add 刷热度,remove 摘除。
	lru interface {
		add(key string)
		remove(key string)
	}

	// emptyLru 空 LRU:不限容量时的空对象。
	emptyLru struct{}

	// keyLru 基于 container/list 的 LRU:
	// 双向链表维护访问顺序(头新尾旧)+ map 加速定位。
	keyLru struct {
		// limit 容量上限。
		limit int
		// evicts 访问序链表:头部最新,尾部最旧(逐出对象)。
		evicts *list.List
		// elements key → 链表节点(O(1) 定位)。
		elements map[string]*list.Element
		// onEvict 逐出回调(删真实数据)。
		onEvict func(key string)
	}
)

// add 空实现(不限容量)。
func (elru emptyLru) add(string) {
}

// remove 空实现。
func (elru emptyLru) remove(string) {
}

// newKeyLru 创建 LRU:超限时 onEvict 逐出最旧。
func newKeyLru(limit int, onEvict func(key string)) *keyLru {
	return &keyLru{
		limit:    limit,
		evicts:   list.New(),
		elements: make(map[string]*list.Element),
		onEvict:  onEvict,
	}
}

// add 访问 key:已存在则移到头部(刷热度);
// 新增则插头部,超限逐出尾部最旧的。
// 注意:调用方必须已持有 Cache.lock。
func (klru *keyLru) add(key string) {
	if elem, ok := klru.elements[key]; ok {
		klru.evicts.MoveToFront(elem)
		return
	}

	// Add new item
	elem := klru.evicts.PushFront(key)
	klru.elements[key] = elem

	// Verify size not exceeded
	if klru.evicts.Len() > klru.limit {
		klru.removeOldest()
	}
}

// remove 显式删除 key(调用方已持锁)。
func (klru *keyLru) remove(key string) {
	if elem, ok := klru.elements[key]; ok {
		klru.removeElement(elem)
	}
}

// removeOldest 逐出链表尾(最久未访问)。
func (klru *keyLru) removeOldest() {
	elem := klru.evicts.Back()
	if elem != nil {
		klru.removeElement(elem)
	}
}

// removeElement 摘除节点:链表 + map 同步删,回调通知外层。
func (klru *keyLru) removeElement(e *list.Element) {
	klru.evicts.Remove(e)
	key := e.Value.(string)
	delete(klru.elements, key)
	klru.onEvict(key)
}

// cacheStat 命中率统计(每分钟窗口,Swap 清零取快照)。
type cacheStat struct {
	// name 报表标识。
	name string
	// hit/miss 窗口内命中/未命中数(原子)。
	hit  uint64
	miss uint64
	// sizeCallback 取当前条数。
	sizeCallback func() int
}

// newCacheStat 创建统计并启动报表 goroutine。
func newCacheStat(name string, sizeCallback func() int) *cacheStat {
	st := &cacheStat{
		name:         name,
		sizeCallback: sizeCallback,
	}
	go st.statLoop()
	return st
}

// IncrementHit 命中 +1。
func (cs *cacheStat) IncrementHit() {
	atomic.AddUint64(&cs.hit, 1)
}

// IncrementMiss 未命中 +1。
func (cs *cacheStat) IncrementMiss() {
	atomic.AddUint64(&cs.miss, 1)
}

// statLoop 每分钟输出 QPM/命中率/条数(无流量则跳过)。
func (cs *cacheStat) statLoop() {
	ticker := time.NewTicker(statInterval)
	defer ticker.Stop()

	for range ticker.C {
		hit := atomic.SwapUint64(&cs.hit, 0)
		miss := atomic.SwapUint64(&cs.miss, 0)
		total := hit + miss
		if total == 0 {
			continue
		}
		percent := 100 * float32(hit) / float32(total)
		logx.Statf("cache(%s) - qpm: %d, hit_ratio: %.1f%%, elements: %d, hit: %d, miss: %d",
			cs.name, total, percent, cs.sizeCallback(), hit, miss)
	}
}
