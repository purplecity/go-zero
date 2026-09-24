// ————————————————————————————————————————————————————————————————————————————
// sheddergroup —— 按业务 key 复用脱落器 —— 文件总结
//
// 同一进程里不同业务(如不同 RPC 方法)各自需要独立的
// 负载统计,但懒创建 + 复用:第一次 GetShedder(key) 时创建,
// 之后同一 key 拿同一个实例(syncx.ResourceManager 保证
// 并发下只建一次)。nopCloser 把 Shedder 适配成 io.Closer
// 以满足 ResourceManager 的接口(Close 为空操作)。
// ————————————————————————————————————————————————————————————————————————————
package load

import (
	"io"

	"github.com/zeromicro/go-zero/core/syncx"
)

// A ShedderGroup is a manager to manage key-based shedders.
// 脱落器组:按 key 管理一组独立统计的脱落器。
type ShedderGroup struct {
	// options 创建脱落器时统一应用的选项。
	options []ShedderOption
	// manager 底层资源管理器(懒创建 + 并发安全)。
	manager *syncx.ResourceManager
}

// NewShedderGroup returns a ShedderGroup.
// 创建脱落器组(opts 会应用到组内每个脱落器)。
func NewShedderGroup(opts ...ShedderOption) *ShedderGroup {
	return &ShedderGroup{
		options: opts,
		manager: syncx.NewResourceManager(),
	}
}

// GetShedder gets the Shedder for the given key.
// 取(或首次创建)key 对应的脱落器:同 key 恒同实例。
func (g *ShedderGroup) GetShedder(key string) Shedder {
	shedder, _ := g.manager.GetResource(key, func() (closer io.Closer, e error) {
		return nopCloser{
			Shedder: NewAdaptiveShedder(g.options...),
		}, nil
	})
	return shedder.(Shedder)
}

// nopCloser 给 Shedder 补一个空 Close,凑成 io.Closer
// (ResourceManager 的接口要求)。
type nopCloser struct {
	Shedder
}

// Close 空操作:脱落器无需关闭。
func (c nopCloser) Close() error {
	return nil
}
