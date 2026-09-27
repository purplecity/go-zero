// ————————————————————————————————————————————————————————————————————————————
// cache —— Redis 缓存:客户端分片路由壳 —— 文件总结
//
// 【包结构】四块各司其职,靠 Cache 接口串联(单节点与集群
// 对调用方无感;单节点配置时 New 直接返回节点,不套壳):
//
//	cache.go      路由壳:一致性哈希决定 key 归哪个节点 ——
//	              dispatcher.Get(key) 是"问路"不是读数据;
//	              Del 多 key 时按节点分组批量发;
//	cachenode.go  节点逻辑:读写 + Take 回源(双检/占位符);
//	cleaner.go    删除失败退避重试(1s→5s→1m→5m→1h);
//	cachestat.go  命中率统计。
//
// 【四大病四味药】每个机制恰好防一种经典事故,不多不少:
//
//	击穿(热 key 过期瞬间并发全打 DB)→ SingleFlight 合并
//	  回源 + 双检(doTake 进 barrier 先 GET 一次 —— barrier
//	  只合并"进行中"的并发,上一轮回填的结果靠缓存粘连);
//	穿透(不存在的 key 反复打 DB)→ "*" 占位符 + SETNX
//	  短过期(setCacheWithNotFound);
//	雪崩(同批 key 同一秒集体过期)→ ±5% 过期抖动
//	  (unstableExpiry,mathx.Unstable);
//	不一致(DB 更新了缓存删不掉)→ 退避重试 + TTL 兜底。
//
// 另一条保命原则:Redis 出错直接 fail fast,绝不放流量去
// 打 DB(doTake 注释:"don't allow the disaster pass to
// the dbs")。
//
// 【分片说明】"多个独立 Redis" ≠ "Redis Cluster":ClusterConf
// 每项是独立实例,路由在客户端做(支持按权重);每个逻辑
// 节点底层也可以是整个 Redis Cluster(c.rds.Type 分支),
// 两层可叠加。一致性哈希解决横向容量(同 key 恒路由同节点,
// 增删节点只迁 ~1/N),高可用靠各节点自己的主从/哨兵。
// ————————————————————————————————————————————————————————————————————————————
package cache

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/zeromicro/go-zero/core/errorx"
	"github.com/zeromicro/go-zero/core/hash"
	"github.com/zeromicro/go-zero/core/stores/redis"
	"github.com/zeromicro/go-zero/core/syncx"
)

type (
	// Cache interface is used to define the cache implementation.
	Cache interface {
		// Del deletes cached values with keys.
		Del(keys ...string) error
		// DelCtx deletes cached values with keys.
		DelCtx(ctx context.Context, keys ...string) error
		// Get gets the cache with key and fills into v.
		Get(key string, val any) error
		// GetCtx gets the cache with key and fills into v.
		GetCtx(ctx context.Context, key string, val any) error
		// IsNotFound checks if the given error is the defined errNotFound.
		IsNotFound(err error) bool
		// Set sets the cache with key and v, using c.expiry.
		Set(key string, val any) error
		// SetCtx sets the cache with key and v, using c.expiry.
		SetCtx(ctx context.Context, key string, val any) error
		// SetWithExpire sets the cache with key and v, using given expire.
		SetWithExpire(key string, val any, expire time.Duration) error
		// SetWithExpireCtx sets the cache with key and v, using given expire.
		SetWithExpireCtx(ctx context.Context, key string, val any, expire time.Duration) error
		// Take takes the result from cache first, if not found,
		// query from DB and set cache using c.expiry, then return the result.
		Take(val any, key string, query func(val any) error) error
		// TakeCtx takes the result from cache first, if not found,
		// query from DB and set cache using c.expiry, then return the result.
		TakeCtx(ctx context.Context, val any, key string, query func(val any) error) error
		// TakeWithExpire takes the result from cache first, if not found,
		// query from DB and set cache using given expire, then return the result.
		TakeWithExpire(val any, key string, query func(val any, expire time.Duration) error) error
		// TakeWithExpireCtx takes the result from cache first, if not found,
		// query from DB and set cache using given expire, then return the result.
		TakeWithExpireCtx(ctx context.Context, val any, key string,
			query func(val any, expire time.Duration) error) error
	}

	cacheCluster struct {
		dispatcher  *hash.ConsistentHash
		errNotFound error
	}
)

// New returns a Cache.
func New(c ClusterConf, barrier syncx.SingleFlight, st *Stat, errNotFound error,
	opts ...Option) Cache {
	if len(c) == 0 || TotalWeights(c) <= 0 {
		log.Fatal("no cache nodes")
	}

	if len(c) == 1 {
		return NewNode(redis.MustNewRedis(c[0].RedisConf), barrier, st, errNotFound, opts...)
	}

	dispatcher := hash.NewConsistentHash()
	for _, node := range c {
		cn := NewNode(redis.MustNewRedis(node.RedisConf), barrier, st, errNotFound, opts...)
		dispatcher.AddWithWeight(cn, node.Weight)
	}

	return cacheCluster{
		dispatcher:  dispatcher,
		errNotFound: errNotFound,
	}
}

// Del deletes cached values with keys.
func (cc cacheCluster) Del(keys ...string) error {
	return cc.DelCtx(context.Background(), keys...)
}

// DelCtx deletes cached values with keys.
func (cc cacheCluster) DelCtx(ctx context.Context, keys ...string) error {
	switch len(keys) {
	case 0:
		return nil
	case 1:
		key := keys[0]
		c, ok := cc.dispatcher.Get(key)
		if !ok {
			return cc.errNotFound
		}

		return c.(Cache).DelCtx(ctx, key)
	default:
		var be errorx.BatchError
		nodes := make(map[any][]string)
		for _, key := range keys {
			c, ok := cc.dispatcher.Get(key)
			if !ok {
				be.Add(fmt.Errorf("key %q not found", key))
				continue
			}

			nodes[c] = append(nodes[c], key)
		}
		for c, ks := range nodes {
			if err := c.(Cache).DelCtx(ctx, ks...); err != nil {
				be.Add(err)
			}
		}

		return be.Err()
	}
}

// Get gets the cache with key and fills into v.
func (cc cacheCluster) Get(key string, val any) error {
	return cc.GetCtx(context.Background(), key, val)
}

// GetCtx gets the cache with key and fills into v.
func (cc cacheCluster) GetCtx(ctx context.Context, key string, val any) error {
	c, ok := cc.dispatcher.Get(key)
	if !ok {
		return cc.errNotFound
	}

	return c.(Cache).GetCtx(ctx, key, val)
}

// IsNotFound checks if the given error is the defined errNotFound.
func (cc cacheCluster) IsNotFound(err error) bool {
	return errors.Is(err, cc.errNotFound)
}

// Set sets the cache with key and v, using c.expiry.
func (cc cacheCluster) Set(key string, val any) error {
	return cc.SetCtx(context.Background(), key, val)
}

// SetCtx sets the cache with key and v, using c.expiry.
func (cc cacheCluster) SetCtx(ctx context.Context, key string, val any) error {
	c, ok := cc.dispatcher.Get(key)
	if !ok {
		return cc.errNotFound
	}

	return c.(Cache).SetCtx(ctx, key, val)
}

// SetWithExpire sets the cache with key and v, using given expire.
func (cc cacheCluster) SetWithExpire(key string, val any, expire time.Duration) error {
	return cc.SetWithExpireCtx(context.Background(), key, val, expire)
}

// SetWithExpireCtx sets the cache with key and v, using given expire.
func (cc cacheCluster) SetWithExpireCtx(ctx context.Context, key string, val any, expire time.Duration) error {
	c, ok := cc.dispatcher.Get(key)
	if !ok {
		return cc.errNotFound
	}

	return c.(Cache).SetWithExpireCtx(ctx, key, val, expire)
}

// Take takes the result from cache first, if not found,
// query from DB and set cache using c.expiry, then return the result.
func (cc cacheCluster) Take(val any, key string, query func(val any) error) error {
	return cc.TakeCtx(context.Background(), val, key, query)
}

// TakeCtx takes the result from cache first, if not found,
// query from DB and set cache using c.expiry, then return the result.
func (cc cacheCluster) TakeCtx(ctx context.Context, val any, key string, query func(val any) error) error {
	c, ok := cc.dispatcher.Get(key)
	if !ok {
		return cc.errNotFound
	}

	return c.(Cache).TakeCtx(ctx, val, key, query)
}

// TakeWithExpire takes the result from cache first, if not found,
// query from DB and set cache using given expire, then return the result.
func (cc cacheCluster) TakeWithExpire(val any, key string, query func(val any, expire time.Duration) error) error {
	return cc.TakeWithExpireCtx(context.Background(), val, key, query)
}

// TakeWithExpireCtx takes the result from cache first, if not found,
// query from DB and set cache using given expire, then return the result.
func (cc cacheCluster) TakeWithExpireCtx(ctx context.Context, val any, key string, query func(val any, expire time.Duration) error) error {
	c, ok := cc.dispatcher.Get(key)
	if !ok {
		return cc.errNotFound
	}

	return c.(Cache).TakeWithExpireCtx(ctx, val, key, query)
}
