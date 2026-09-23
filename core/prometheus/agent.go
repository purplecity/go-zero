// ————————————————————————————————————————————————————————————————————————————
// agent —— Prometheus 指标暴露代理 —— 文件总结
//
// StartAgent 在本进程内起一个 HTTP server(默认 :9101/metrics),
// 暴露 promhttp.Handler() —— Prometheus 服务端定时来抓取指标。
//
// 关键设计:
//
//	once        多个服务组件重复调用 StartAgent 只起一个 server;
//	enabled     全局开关:metric 包所有打点都先查 Enabled(),
//	            未启用时零开销跳过 —— 所以 metric 包的打点代码
//	            可以放心地写在业务热路径上;
//	Host 为空   表示服务配置未开启 prometheus,直接返回(不置 enabled)。
//
// ————————————————————————————————————————————————————————————————————————————
package prometheus

import (
	"fmt"
	"net/http"
	"sync"

	"github.com/prometheus/client_golang/prometheus/promhttp"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/syncx"
	"github.com/zeromicro/go-zero/core/threading"
)

var (
	// once 保证 HTTP server 只启动一次。
	once sync.Once
	// enabled 全局开关(原子布尔),metric 包打点前检查。
	enabled syncx.AtomicBool
)

// Enabled returns if prometheus is enabled.
// 查询 prometheus 是否已启用(metric 包打点总闸)。
func Enabled() bool {
	return enabled.True()
}

// Enable enables prometheus.
// 只启用打点但不启动抓取 server(配合外部 push 网关等场景)。
func Enable() {
	enabled.Set(true)
}

// StartAgent starts a prometheus agent.
// 启动指标抓取 server:Host 为空直接返回;
// once 保证只启动一次;启用打点总闸并起 HTTP 服务。
func StartAgent(c Config) {
	if len(c.Host) == 0 {
		return
	}

	once.Do(func() {
		// 打开全局打点开关。
		enabled.Set(true)
		threading.GoSafe(func() {
			http.Handle(c.Path, promhttp.Handler())
			addr := fmt.Sprintf("%s:%d", c.Host, c.Port)
			logx.Infof("Starting prometheus agent at %s", addr)
			// 阻塞服务;失败(如端口被占)记 error 日志。
			if err := http.ListenAndServe(addr, nil); err != nil {
				logx.Error(err)
			}
		})
	})
}
