// ————————————————————————————————————————————————————————————————————————————
// host —— 主机名获取(带缓存与兜底) —— 文件总结
//
// init 时读一次 os.Hostname 并缓存到包级变量,之后 Hostname()
// 直接返回缓存值(避免每次系统调用)。
// 兜底设计:极少数环境拿不到主机名(如某些受限容器)时,
// 用随机 id 代替 —— 日志目录(logx volume 模式用主机名拼路径)、
// 服务注册等场景不能因为拿不到主机名而挂掉。
// ————————————————————————————————————————————————————————————————————————————
package sysx

import (
	"os"

	"github.com/zeromicro/go-zero/core/stringx"
)

// hostname 缓存的主机名,init 时写入,之后只读。
var hostname string

func init() {
	var err error
	hostname, err = os.Hostname()
	if err != nil {
		// 拿不到主机名就用随机 id 兜底,保证调用方永远拿到非空值。
		hostname = stringx.RandId()
	}
}

// Hostname returns the name of the host, if no hostname, a random id is returned.
// 返回主机名;获取失败的环境返回随机 id(启动时已缓存,直接返回)。
func Hostname() string {
	return hostname
}
