// ————————————————————————————————————————————————————————————————————————————
// alert —— 报警通道:节流去重的告警出口 —— 文件总结(linux)
//
// stat.Report 的落点之一(断路器打开、缓存清理重试失败、
// 脱落器过载等都走这里)。设计要点是"别把人淹死":
//
//	lessExecutor(5 分钟节流):同类报警 5 分钟内只发一条,
//	  被扔掉的条数累计在 dropped,下一条发出时附带
//	  "dropped: N" —— 告知漏了多少,而不是悄悄丢;
//	正文自动拼:时间戳 + 集群名(CLUSTER_NAME 环境变量)+
//	  主机名 + 漏报数 + 原始消息;
//	默认上报函数 logx.Alert(接报警系统时 SetReporter 换掉);
//	go test 环境自动关闭(flag 里有 test.v 即 SetReporter(nil)),
//	  防止单测把报警打出去。
//
// 非 linux 平台的行为见 alert+polyfill.go(空实现)。
// ————————————————————————————————————————————————————————————————————————————
//go:build linux

package stat

import (
	"flag"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/zeromicro/go-zero/core/executors"
	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/sysx"
)

const (
	// clusterNameKey 集群名环境变量(报警正文带上)。
	clusterNameKey = "CLUSTER_NAME"
	// testEnv go test 的标志名(检测到即关报警)。
	testEnv = "test.v"
)

var (
	// reporter 报警出口(默认 logx.Alert,可换)。
	reporter = logx.Alert
	lock     sync.RWMutex
	// lessExecutor 5 分钟节流器(DoOrDiscard:窗口内首条发,余弃)。
	lessExecutor = executors.NewLessExecutor(time.Minute * 5)
	// dropped 被节流扔掉的报警条数(下一条补报)。
	dropped     int32
	clusterName = proc.Env(clusterNameKey)
)

func init() {
	if flag.Lookup(testEnv) != nil {
		SetReporter(nil)
	}
}

// Report reports given message.
// 发报警(节流):窗口内首条发出并附带漏报数,其余只计数。
func Report(msg string) {
	lock.RLock()
	fn := reporter
	lock.RUnlock()

	if fn != nil {
		reported := lessExecutor.DoOrDiscard(func() {
			var builder strings.Builder
			builder.WriteString(fmt.Sprintln(time.Now().Format(time.DateTime)))
			if len(clusterName) > 0 {
				builder.WriteString(fmt.Sprintf("cluster: %s\n", clusterName))
			}
			builder.WriteString(fmt.Sprintf("host: %s\n", sysx.Hostname()))
			// 取走并清零漏报数,拼进本条报警。
			dp := atomic.SwapInt32(&dropped, 0)
			if dp > 0 {
				builder.WriteString(fmt.Sprintf("dropped: %d\n", dp))
			}
			builder.WriteString(strings.TrimSpace(msg))
			fn(builder.String())
		})
		if !reported {
			// 节流窗口内被扔掉:计数,下条补报。
			atomic.AddInt32(&dropped, 1)
		}
	}
}

// SetReporter sets the given reporter.
// 换报警出口(nil = 关闭)。
func SetReporter(fn func(string)) {
	lock.Lock()
	defer lock.Unlock()
	reporter = fn
}
