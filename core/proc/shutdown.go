// ————————————————————————————————————————————————————————————————————————————
// shutdown —— 优雅退出(两阶段) —— 文件总结
//
// 退出流程(收到 SIGTERM/SIGINT 后由 gracefulStop 驱动):
//
//	├─ 立即   wrapUpListeners  收尾阶段:停止接新流量、上报状态
//	├─ 等 WrapUpTime(默认 1s)
//	├─ 然后   shutdownListeners 关停阶段:刷盘、关连接、反注册指标
//	└─ 等 waitTime(默认 5.5s)后仍存活 → 自我强杀 syscall.Kill
//	                                  (等待时间比 5s 队列阻塞多 500ms)
//
// 组件通过 AddWrapUpListener/AddShutdownListener 注册回调,
// 返回的 waitForCalled() 可阻塞等待"回调已被执行"(内部是
// WaitGroup.Wait),便于启动方确保清理动作真的完成了。
// Windows 平台由 shutdown+polyfill.go 提供全部空实现。
// ————————————————————————————————————————————————————————————————————————————
//go:build linux || darwin || freebsd

package proc

import (
	"os"
	"os/signal"
	"sync"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
	"github.com/zeromicro/go-zero/core/threading"
)

const (
	// defaultWrapUpTime is the default time to wait before calling wrap up listeners.
	// wrapUp(收尾)阶段的默认时长。
	defaultWrapUpTime = time.Second
	// defaultWaitTime is the default time to wait before force quitting.
	// why we use 5500 milliseconds is because most of our queues are blocking mode with 5 seconds
	// 强杀前的总等待时间;5.5s 是因为内部队列多为 5s 阻塞模式,
	// 留出 500ms 余量让阻塞操作超时返回。
	defaultWaitTime = 5500 * time.Millisecond
)

var (
	// wrapUpListeners 收尾阶段监听器。
	wrapUpListeners = new(listenerManager)
	// shutdownListeners 关停阶段监听器。
	shutdownListeners = new(listenerManager)
	// wrapUpTime/waitTime 两个阶段时长(可由 Setup/配置覆盖)。
	wrapUpTime = defaultWrapUpTime
	waitTime   = defaultWaitTime
	// shutdownLock 保护两个时长变量。
	shutdownLock sync.Mutex
)

// ShutdownConf defines the shutdown configuration for the process.
// 退出配置(来自服务配置文件)。
type ShutdownConf struct {
	// WrapUpTime is the time to wait before calling shutdown listeners.
	// 收尾阶段时长。
	WrapUpTime time.Duration `json:",default=1s"`
	// WaitTime is the time to wait before force quitting.
	// 强杀前的总等待时长。
	WaitTime time.Duration `json:",default=5.5s"`
}

// AddShutdownListener adds fn as a shutdown listener.
// The returned func can be used to wait for fn getting called.
// 注册关停回调;返回 waitForCalled() 可阻塞等待回调已执行。
func AddShutdownListener(fn func()) (waitForCalled func()) {
	return shutdownListeners.addListener(fn)
}

// AddWrapUpListener adds fn as a wrap up listener.
// The returned func can be used to wait for fn getting called.
// 注册收尾回调;返回 waitForCalled() 同上。
func AddWrapUpListener(fn func()) (waitForCalled func()) {
	return wrapUpListeners.addListener(fn)
}

// SetTimeToForceQuit sets the waiting time before force quitting.
// 设置强杀前的总等待时长。
func SetTimeToForceQuit(duration time.Duration) {
	shutdownLock.Lock()
	defer shutdownLock.Unlock()
	waitTime = duration
}

// Setup 应用退出配置(仅覆盖正值)。
func Setup(conf ShutdownConf) {
	shutdownLock.Lock()
	defer shutdownLock.Unlock()

	if conf.WrapUpTime > 0 {
		wrapUpTime = conf.WrapUpTime
	}
	if conf.WaitTime > 0 {
		waitTime = conf.WaitTime
	}
}

// Shutdown calls the registered shutdown listeners, only for test purpose.
// 手动触发关停回调(仅测试用)。
func Shutdown() {
	shutdownListeners.notifyListeners()
}

// WrapUp wraps up the process, only for test purpose.
// 手动触发收尾回调(仅测试用)。
func WrapUp() {
	wrapUpListeners.notifyListeners()
}

// gracefulStop 优雅退出主流程(见文件头时序):
// 停止接收本信号 → 收尾回调 → 等 wrapUpTime → 关停回调 →
// 等剩余宽限 → 仍存活则向自己重发信号(走默认处理强制退出)。
func gracefulStop(signals chan os.Signal, sig syscall.Signal) {
	// 停止监听该信号:之后重发的信号走系统默认处理(直接杀)。
	signal.Stop(signals)

	logx.Infof("Got signal %d, shutting down...", sig)
	// 异步执行收尾回调,主流程按时间轴推进。
	go wrapUpListeners.notifyListeners()

	time.Sleep(wrapUpTime)
	go shutdownListeners.notifyListeners()

	shutdownLock.Lock()
	remainingTime := waitTime - wrapUpTime
	shutdownLock.Unlock()

	// 给回调们留够总时长,超时即强杀兜底。
	time.Sleep(remainingTime)
	logx.Infof("Still alive after %v, going to force kill the process...", waitTime)
	_ = syscall.Kill(syscall.Getpid(), sig)
}

// listenerManager 一组回调的管理:注册、并发执行(每个回调
// 独立 goroutine,任一 panic 被 RunSafe 兜住)、执行后清空(幂等)。
type listenerManager struct {
	lock      sync.Mutex
	waitGroup sync.WaitGroup
	listeners []func()
}

// addListener 注册回调,返回等待其被执行的函数。
func (lm *listenerManager) addListener(fn func()) (waitForCalled func()) {
	lm.waitGroup.Add(1)

	lm.lock.Lock()
	lm.listeners = append(lm.listeners, func() {
		defer lm.waitGroup.Done()
		fn()
	})
	lm.lock.Unlock()

	// we can return lm.waitGroup.Wait directly,
	// but we want to make the returned func more readable.
	// creating an extra closure would be negligible in practice.
	// 包一层闭包纯粹为了可读性。
	return func() {
		lm.waitGroup.Wait()
	}
}

// notifyListeners 并发执行全部回调(RunSafe 防 panic 互扰),
// 执行完清空列表 —— 保证重复触发不会重复执行。
func (lm *listenerManager) notifyListeners() {
	lm.lock.Lock()
	defer lm.lock.Unlock()

	group := threading.NewRoutineGroup()
	for _, listener := range lm.listeners {
		group.RunSafe(listener)
	}
	group.Wait()

	lm.listeners = nil
}
