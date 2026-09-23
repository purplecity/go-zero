// ————————————————————————————————————————————————————————————————————————————
// env —— 环境变量读取(带进程级缓存) —— 文件总结
//
// 对 os.Getenv 的缓存封装:每个变量名只在第一次真正读系统环境,
// 之后直接返回缓存值,省去反复的系统调用/字符串查找。
// envInt 变体提供整数解析(空值或解析失败返回 ok=false)。
// 读写锁保护缓存 map,支持并发调用。
// 注意:缓存意味着进程存活期间环境变量不会刷新。
// ————————————————————————————————————————————————————————————————————————————
package proc

import (
	"os"
	"strconv"
	"sync"
)

var (
	// envs 已读取过的环境变量缓存。
	envs = make(map[string]string)
	// envLock 保护缓存(读多写少,RWMutex)。
	envLock sync.RWMutex
)

// Env returns the value of the given environment variable.
// 读取环境变量:先查缓存,未命中则读系统环境并写缓存。
func Env(name string) string {
	envLock.RLock()
	val, ok := envs[name]
	envLock.RUnlock()

	if ok {
		return val
	}

	// 未命中:读系统环境,写缓存。
	// 注:环境变量不存在时 os.Getenv 返回空串,也照常缓存,
	// 避免对同一缺失变量反复系统调用。
	val = os.Getenv(name)
	envLock.Lock()
	envs[name] = val
	envLock.Unlock()

	return val
}

// EnvInt returns an int value of the given environment variable.
// 读取整数型环境变量:值为空或不是合法整数时返回 (0, false)。
func EnvInt(name string) (int, bool) {
	val := Env(name)
	if len(val) == 0 {
		return 0, false
	}

	n, err := strconv.Atoi(val)
	if err != nil {
		return 0, false
	}

	return n, true
}
