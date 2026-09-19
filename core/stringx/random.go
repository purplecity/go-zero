// ————————————————————————————————————————————————————————————————————————————
// random —— 随机字符串生成 —— 文件总结
//
// 一、三个生成函数
//
//	Rand()   8 位随机字符串(字母+数字),常用于 trace/log 等普通场景;
//	RandId() 8 字节 crypto/rand → 16 位十六进制随机 id,密码学强度,
//	         失败时降级为 Randn(8);用于需要不可预测性的场景;
//	Randn(n) 指定长度的随机字符串。
//
// 二、Randn 的位运算技巧(出自 Go 官方博客的 rand 变体)
//
//	一次 Int63() 产生 63 个随机位,每个字母索引用 6 位表示
//	(62 个字符 < 64),所以一次随机数够取 10 个字符(letterIdxMax),
//	比逐字符 rand 快数倍。
//	掩码取出的索引落在 62~63 时直接丢弃重取 —— 这是"拒绝采样",
//	避免对 62 取模造成的分布偏差(模运算会偏向前几个字符)。
//
// 三、并发安全
//
//	全局共享一个随机源 src;标准库 rand.Source 非线程安全,
//	所以用 lockedSource 加互斥锁包装。可用 Seed 重置种子(测试用)。
//
// ————————————————————————————————————————————————————————————————————————————
package stringx

import (
	crand "crypto/rand"
	"fmt"
	"math/rand"
	"sync"
	"time"
)

const (
	// letterBytes 随机字符串的字符集:52 个字母 + 10 个数字。
	letterBytes = "abcdefghijklmnopqrstuvwxyzABCDEFGHIJKLMNOPQRSTUVWXYZ0123456789"
	// letterIdxBits 6 bits to represent a letter index
	// 每个字符索引消耗的随机位数:2^6=64 ≥ 62 个字符。
	letterIdxBits = 6
	// idLen RandId 的随机字节数(输出 16 位十六进制)。
	idLen = 8
	// defaultRandLen Rand 的默认长度。
	defaultRandLen = 8
	// letterIdxMask All 1-bits, as many as letterIdxBits
	// 低 6 位全 1 掩码(0b111111),用于取出一个字符索引。
	letterIdxMask = 1<<letterIdxBits - 1
	// letterIdxMax # of letter indices fitting in 63 bits
	// 一次 Int63()(63 位)能取出的字符数:63/6=10。
	letterIdxMax = 63 / letterIdxBits
)

// src 全局共享随机源(加锁保证并发安全)。
var src = newLockedSource(time.Now().UnixNano())

// lockedSource 给 rand.Source 加互斥锁:
// 标准库的 Source 接口本身不是并发安全的,
// 全局共享时必须加锁(官方推荐的包装方式)。
type lockedSource struct {
	source rand.Source
	lock   sync.Mutex
}

func newLockedSource(seed int64) *lockedSource {
	return &lockedSource{
		source: rand.NewSource(seed),
	}
}

// Int63 加锁产出 63 位随机数。
func (ls *lockedSource) Int63() int64 {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	return ls.source.Int63()
}

// Seed 加锁重置种子。
func (ls *lockedSource) Seed(seed int64) {
	ls.lock.Lock()
	defer ls.lock.Unlock()
	ls.source.Seed(seed)
}

// Rand returns a random string.
// 返回 8 位随机字符串(字母+数字)。
func Rand() string {
	return Randn(defaultRandLen)
}

// RandId returns a random id string.
// 返回 16 位十六进制随机 id:crypto/rand 读 8 个随机字节,
// 按 2 字节一组格式化成 4 段十六进制。密码学安全随机源,
// 失败(熵不足等)时降级为普通随机串。
func RandId() string {
	b := make([]byte, idLen)
	_, err := crand.Read(b)
	if err != nil {
		return Randn(idLen)
	}

	return fmt.Sprintf("%x%x%x%x", b[0:2], b[2:4], b[4:6], b[6:8])
}

// Randn returns a random string with length n.
// 返回长度 n 的随机字符串(位运算批量取索引 + 拒绝采样,见文件头)。
func Randn(n int) string {
	b := make([]byte, n)
	// A src.Int63() generates 63 random bits, enough for letterIdxMax characters!
	// 从尾部往前填;cache 缓存一次随机数,remain 记录还能取几个索引。
	for i, cache, remain := n-1, src.Int63(), letterIdxMax; i >= 0; {
		if remain == 0 {
			// 当前随机数用尽,再取一个(又够 10 个字符)。
			cache, remain = src.Int63(), letterIdxMax
		}
		// 取低 6 位作为索引;≥62 时丢弃(拒绝采样,保证均匀),
		// 注意此时 i 不前进,消耗的是 cache 的位数。
		if idx := int(cache & letterIdxMask); idx < len(letterBytes) {
			b[i] = letterBytes[idx]
			i--
		}
		cache >>= letterIdxBits
		remain--
	}

	return string(b)
}

// Seed sets the seed to seed.
// 重置全局随机源种子(测试中固定随机序列用)。
func Seed(seed int64) {
	src.Seed(seed)
}
