// ————————————————————————————————————————————————————————————————————————————
// bloom —— Redis 位图版布隆过滤器 —— 文件总结
//
// 【是什么】概率型集合:Exists 只会说"可能存在"(有误判)
// 或"一定不存在"(绝不漏判)。Add 把元素经 14 个哈希映射
// 成位图上的 14 个位并全部置 1;Exists 查这 14 位,全 1 才
// 报存在 —— 位冲突越多误判越多,但漏判不可能(元素加过,
// 它的 14 个位必然都是 1)。典型用途:缓存穿透防护(不存
// 在的 key 在回源前先被这里挡掉)、URL/ID 去重等
// "宁可误放、绝不漏判"的场景。
//
// 【go-zero 实现三件套】
//
//	位数组在 Redis:SETBIT/GETBIT 操作一个 string 键,
//	  跨进程共享、进程重启不丢;
//	k=14 个哈希的手法:只用一个 murmur3,把序号 i(0..13)
//	  拼在数据尾部再哈希(加盐变体),省掉 14 个哈希实现
//	  —— 见 getLocations;
//	Lua 脚本打包:14 个位操作合成一次网络往返(否则 14 次
//	  RTT, Redis 跨网络才是大头);testscript 任一位为 0
//	  立即短路返回 false,setscript 全部置 1。
//
// 【参数经验】(maps=14,见 Cao 的误差率表,upstream 注释)
//
//	bits = 20×元素数 → 误差率 0.000067(< 1e-4);
//	最优容量 ≈ 0.7×(bits/maps) = bits/20.2。容量不是硬墙:
//	Add 永远成功、漏判恒为 0,超容只是误判率指数爬坡
//	(实测 bits=200:10 个元素 0%,20 个 5%,50 个 74%)。
//
// 【生产局限:大 key 只在"仪式性时刻"阻塞】bits 大 → 单个
// Redis string 就是巨型 key(1 亿元素 ≈ 238MB)。稳态的
// SETBIT/GETBIT 都是 O(1)(按 offset/8 直定位字节,不扫描
// 全 key),µs 级、毫无压力;会卡的是四个轮不到业务按按钮
// 的时刻:
//
//	① 出生:偏移均匀随机,头几个 Add 就会摸到接近最高位,
//	   触发一次性 realloc+清零(~几十 ms 毛刺)→ 上线前
//	   预热:建好后立刻 SETBIT 最高位,把毛刺挪到部署时刻;
//	② 死亡:DEL/EXPIRE 同步释放整个 key → 删除用 UNLINK,
//	   TTL 慎用或开 lazyfree-lazy-expire;
//	③ 搬家:主从全量同步/集群迁移要把整 key 序列化传输;
//	④ 备份:BGSAVE/AOF 的 fork 随堆变大变慢,期间 SETBIT
//	   的页写时复制(COW)使内存峰值翻倍。
//
// 亿级体量的标配:按业务维度拆多个 filter,或按 bits 分段
// 多 key(offset 加基数路由),把大 key 化整为零;单 filter
// 的甜点是单业务千万级以内。
//
// 【不支持删除】位只置 1 不清零(一个位可能被多个元素共享,
// 清了会漏判别人);del/expire 仅测试用。要"重置"只能换新
// key 重建。位图永不收缩,选 bits 时按峰值预估。
// ————————————————————————————————————————————————————————————
package bloom

import (
	"context"
	_ "embed"
	"errors"
	"strconv"

	"github.com/zeromicro/go-zero/core/hash"
	"github.com/zeromicro/go-zero/core/stores/redis"
)

// for detailed error rate table, see http://pages.cs.wisc.edu/~cao/papers/summary-cache/node8.html
// maps as k in the error rate table
// 哈希函数个数 k(每个元素置/查的位数),取 14 对应误差表里
// bits=20×元素数、误差率 <1e-4 的推荐档。
const maps = 14

var (
	// ErrTooLargeOffset indicates the offset is too large in bitset.
	// 偏移越界(getLocations 取模后本不会触发,防御性检查)。
	ErrTooLargeOffset = errors.New("too large offset")

	//go:embed setscript.lua
	setLuaScript string
	// setScript 把 ARGV 里每个 offset 在 KEYS[1] 上 SETBIT 1。
	setScript = redis.NewScript(setLuaScript)

	//go:embed testscript.lua
	testLuaScript string
	// testScript 逐个 GETBIT,任一位为 0 立即返回 false,
	// 全 1 返回 true(Lua 布尔被客户端转成 int64 1/0)。
	testScript = redis.NewScript(testLuaScript)
)

type (
	// A Filter is a bloom filter.
	// 布隆过滤器:哈希定位(bits 个位中的 14 个)+ 位图后端。
	Filter struct {
		// bits 位图总位数。
		bits uint
		// bitSet 位图后端(这里只有 Redis 实现,接口留了
		// 换内存位图等后门)。
		bitSet bitSetProvider
	}

	// bitSetProvider 按 offset 批量查/置位图。
	bitSetProvider interface {
		check(ctx context.Context, offsets []uint) (bool, error)
		set(ctx context.Context, offsets []uint) error
	}
)

// New creates a Filter, store is the backed redis, key is the key for the bloom filter,
// bits is how many bits will be used, maps is how many hashes for each addition.
// best practices:
// elements - means how many actual elements
// when maps = 14, formula: 0.7*(bits/maps), bits = 20*elements, the error rate is 0.000067 < 1e-4
// for detailed error rate table, see http://pages.cs.wisc.edu/~cao/papers/summary-cache/node8.html
// 创建过滤器:bits 决定位图大小与误判率(见文件头参数经验)。
// maps 固定 14,不开放配置。
func New(store *redis.Redis, key string, bits uint) *Filter {
	return &Filter{
		bits:   bits,
		bitSet: newRedisBitSet(store, key, bits),
	}
}

// Add adds data into f.
// 加入元素(置 14 个位)。
func (f *Filter) Add(data []byte) error {
	return f.AddCtx(context.Background(), data)
}

// AddCtx adds data into f with context.
// 带上下文加入元素。
func (f *Filter) AddCtx(ctx context.Context, data []byte) error {
	locations := f.getLocations(data)
	return f.bitSet.set(ctx, locations)
}

// Exists checks if data is in f.
// 元素是否可能存在(true 有小概率误判;false 一定不存在)。
func (f *Filter) Exists(data []byte) (bool, error) {
	return f.ExistsCtx(context.Background(), data)
}

// ExistsCtx checks if data is in f with context.
// 带上下文查询。
func (f *Filter) ExistsCtx(ctx context.Context, data []byte) (bool, error) {
	locations := f.getLocations(data)
	isSet, err := f.bitSet.check(ctx, locations)
	if err != nil {
		return false, err
	}

	return isSet, nil
}

// getLocations 算出元素的 14 个位下标 —— "加盐"多重哈希:
// 同一个 murmur3,把序号 i 拼在数据尾部分别哈希,模拟 14 个
// 独立哈希函数,省掉引入多个哈希实现;对 bits 取模均匀落位。
// 注意 append(data, byte(i)):若 data 有富余容量,会写穿到
// 调用方底层数组的 data[len] 位置(数据本身不受影响)——
// 跨 goroutine 复用同一 buffer 时需留意。
func (f *Filter) getLocations(data []byte) []uint {
	locations := make([]uint, maps)
	for i := uint(0); i < maps; i++ {
		hashValue := hash.Hash(append(data, byte(i)))
		locations[i] = uint(hashValue % uint64(f.bits))
	}

	return locations
}

// redisBitSet Redis 位图后端:一个 string 键 + SETBIT/GETBIT。
type redisBitSet struct {
	// store Redis 客户端。
	store *redis.Redis
	// key 位图所在的 Redis 键。
	key string
	// bits 总位数(越界防御用)。
	bits uint
}

func newRedisBitSet(store *redis.Redis, key string, bits uint) *redisBitSet {
	return &redisBitSet{
		store: store,
		key:   key,
		bits:  bits,
	}
}

// buildOffsetArgs 把 offset 列表转成 Lua 的 ARGV 字符串,
// 顺带做越界防御(取模后本不会超过 bits)。
func (r *redisBitSet) buildOffsetArgs(offsets []uint) ([]string, error) {
	args := make([]string, 0, len(offsets))

	for _, offset := range offsets {
		if offset >= r.bits {
			return nil, ErrTooLargeOffset
		}

		args = append(args, strconv.FormatUint(uint64(offset), 10))
	}

	return args, nil
}

// check 查一批位是否全 1:一次 Lua 往返(Lua 内部任一位为 0
// 即短路);键不存在(redis.Nil)视为"不存在"返回 false。
func (r *redisBitSet) check(ctx context.Context, offsets []uint) (bool, error) {
	args, err := r.buildOffsetArgs(offsets)
	if err != nil {
		return false, err
	}

	resp, err := r.store.ScriptRunCtx(ctx, testScript, []string{r.key}, args)
	if errors.Is(err, redis.Nil) {
		return false, nil
	} else if err != nil {
		return false, err
	}

	// Lua 的 true/false 经客户端转成 int64 1/0。
	exists, ok := resp.(int64)
	if !ok {
		return false, nil
	}

	return exists == 1, nil
}

// del only use for testing.
// 删除整个位图(仅测试用;生产不支持删除,见文件头)。
func (r *redisBitSet) del() error {
	_, err := r.store.Del(r.key)
	return err
}

// expire only use for testing.
// 给位图设 TTL(仅测试用)。
func (r *redisBitSet) expire(seconds int) error {
	return r.store.Expire(r.key, seconds)
}

// set 置一批位为 1:一次 Lua 往返(14 次 SETBIT 合一,
// 省的是网络往返而非语义 —— 位操作本身在 Redis 侧串行)。
func (r *redisBitSet) set(ctx context.Context, offsets []uint) error {
	args, err := r.buildOffsetArgs(offsets)
	if err != nil {
		return err
	}

	_, err = r.store.ScriptRunCtx(ctx, setScript, []string{r.key}, args)
	if errors.Is(err, redis.Nil) {
		return nil
	}

	return err
}
