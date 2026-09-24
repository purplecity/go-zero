// ————————————————————————————————————————————————————————————————————————————
// signals —— 信号处理中心(Unix 平台) —— 文件总结
//
// init 启动一个信号监听 goroutine,统一处理四类信号:
//   SIGUSR1  dump 全部 goroutine 栈到 /tmp 下的 dump 文件(排查卡死);
//   SIGUSR2  开始为期 1 分钟的全量性能剖析(cpu/mem/mutex/block/
//            trace/threadcreate,见 profile.go),到点自动 Stop;
//   SIGTERM  优雅退出:先关 done → 通知 wrapUp 监听器 → 等宽限期
//            → 通知 shutdown 监听器 → 宽限期到仍存活则自我强杀;
//   SIGINT   同 SIGTERM(Ctrl+C)。
//
// Done() 返回的 channel 在收到退出信号时关闭 —— go-zero 服务里
// 各组件(metric 反注册、executors Flush 等)通过它感知退出时机。
// Windows 平台由 signals+polyfill.go 提供空实现(build tag 隔离)。
// ————————————————————————————————————————————————————————————————————————————
//go:build linux || darwin || freebsd

package proc

import (
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// profileDuration SIGUSR2 触发的剖析时长。
	profileDuration = time.Minute
	// timeFormat dump 文件名里的时间格式(月日时分秒)。
	timeFormat = "0102150405"
)

// done 退出通知 channel:收到 SIGTERM/SIGINT 时关闭。
var done = make(chan struct{})

func init() {
	go func() {
		// https://golang.org/pkg/os/signal/#Notify
		// 容量 1:信号处理期间再来的同种信号先缓存一个,不丢。
		signals := make(chan os.Signal, 1)
		signal.Notify(signals, syscall.SIGUSR1, syscall.SIGUSR2, syscall.SIGTERM, syscall.SIGINT)

		for {
			v := <-signals
			switch v {
			case syscall.SIGUSR1:
				dumpGoroutines(fileCreator{})
			case syscall.SIGUSR2:
				// 启动剖析,1 分钟后自动停止,无需再发信号。
				profiler := StartProfile()
				time.AfterFunc(profileDuration, profiler.Stop)
			case syscall.SIGTERM:
				stopOnSignal()
				gracefulStop(signals, syscall.SIGTERM)
			case syscall.SIGINT:
				stopOnSignal()
				gracefulStop(signals, syscall.SIGINT)
			default:
				logx.Error("Got unregistered signal:", v)
			}
		}
	}()
}

// Done returns the channel that notifies the process quitting.
// 返回退出通知 channel;组件用 select <-Done() 感知进程即将退出。
func Done() <-chan struct{} {
	return done
}

// stopOnSignal 幂等关闭 done —— 进程退出广播的总开关。
//
//	done 是全进程的退出广播(SIGTERM/SIGINT 时触发):close 一次,
//	所有 select <-Done() 的等待者同时醒来,状态永久。
//	两个信号分支都会调用本函数(常见场景:Ctrl+C 之后,
//	Docker/K8s 等不及又发 SIGTERM),而 close 已关闭的 channel
//	会 panic —— 所以必须幂等。
//
//	机关在 channel 的核心语义:关闭是【永久状态】,不是一次性
//	事件 —— 从已关闭的 channel 接收,永远立刻就绪(返回零值,
//	无限次,什么都不消耗)。接收方三态:
//	  open+空+无发送方   阻塞 → select 里不就绪 → 走 default;
//	  open+有值          立刻拿到值(每份只能拿一次);
//	  closed             永远立刻就绪 → select 永远选本 case。
//	于是:未关闭时 <-done 不就绪,default 里的 close 才执行;
//	已关闭时 case 永远就绪,default 永远不会再执行 → 幂等。
//	(context.Context 的 ctx.Done() 取消广播用的正是同一语义,
//	所以循环里反复 select 它也不会"错过"取消。)
//
//	前提:本函数只被单一信号 goroutine 串行调用,防"重复"
//	不防"并发";若可能并发调用,需改用 sync.Once 包住
//	close(对比 mr 包 finish 的 closeOnce 做法)。
func stopOnSignal() {
	select {
	case <-done:
		// already closed
	default:
		close(done)
	}
}
