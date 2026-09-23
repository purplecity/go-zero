// ————————————————————————————————————————————————————————————————————————————
// profile —— 全量性能剖析会话(Unix 平台) —— 文件总结
//
// StartProfile 一次性开启全部六种剖析,输出到 /tmp 下的
// {进程名}-{pid}-{类型}-{时间}.pprof 文件,可用 go tool pprof 分析:
//   cpu          CPU 采样(默认 30s 建议时长,这里由 Stop 决定)
//   mem(heap)    堆内存分配(采样率 4096,即每 4KB 分配采一次)
//   mutex        锁竞争(fraction=1 记录全部争用事件)
//   block        阻塞/channel 等待(rate=1 记录全部)
//   trace        执行轨迹(runtime/trace,供 go tool trace)
//   threadcreate 线程创建记录
//
// 关闭路径有三条,殊途同归到 Profile.Stop(幂等,CAS 防重入):
//   1. 调用方手动 Stop();
//   2. SIGUSR2 触发的会话,1 分钟后 AfterFunc 自动 Stop;
//   3. 剖析期间收到 SIGINT:Stop 后 reset 信号并重发给自己,
//      保留 Ctrl+C 默认的退出行为(先落盘再退出)。
// Windows 平台由 profile+polyfill.go 提供空实现。
// ————————————————————————————————————————————————————————————————————————————
//go:build linux || darwin || freebsd

package proc

import (
	"fmt"
	"os"
	"os/signal"
	"path"
	"runtime"
	"runtime/pprof"
	"runtime/trace"
	"sync/atomic"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

// DefaultMemProfileRate is the default memory profiling rate.
// See also http://golang.org/pkg/runtime/#pkg-variables
// 内存剖析采样率:每分配 4KB 采样一次。
const DefaultMemProfileRate = 4096

// started is non zero if a profile is running.
// 全局剖析会话开关:防止重复启动(原子 CAS)。
var started uint32

// Profile represents an active profiling session.
// 一次剖析会话:各子剖析通过向 closers 注册收尾函数实现统一 Stop。
type Profile struct {
	// closers holds cleanup functions that run after each profile
	// 每个子剖析的收尾函数(写文件、关文件、还原采样率)。
	closers []func()

	// stopped records if a call to profile.Stop has been made
	// Stop 幂等标记。
	stopped uint32
}

// close 依次执行全部收尾函数。
func (p *Profile) close() {
	for _, closer := range p.closers {
		closer()
	}
}

// startBlockProfile 开启阻塞剖析:记录 goroutine 在同步原语上的等待。
func (p *Profile) startBlockProfile() {
	fn := createDumpFile("block")
	f, err := os.Create(fn)
	if err != nil {
		logx.Errorf("profile: could not create block profile %q: %v", fn, err)
		return
	}

	// rate=1 记录全部阻塞事件(生产环境慎用,量可能很大)。
	runtime.SetBlockProfileRate(1)
	logx.Infof("profile: block profiling enabled, %s", fn)
	p.closers = append(p.closers, func() {
		pprof.Lookup("block").WriteTo(f, 0)
		f.Close()
		runtime.SetBlockProfileRate(0)
		logx.Infof("profile: block profiling disabled, %s", fn)
	})
}

// startCpuProfile 开启 CPU 采样剖析。
func (p *Profile) startCpuProfile() {
	fn := createDumpFile("cpu")
	f, err := os.Create(fn)
	if err != nil {
		logx.Errorf("profile: could not create cpu profile %q: %v", fn, err)
		return
	}

	logx.Infof("profile: cpu profiling enabled, %s", fn)
	pprof.StartCPUProfile(f)
	p.closers = append(p.closers, func() {
		pprof.StopCPUProfile() // Stop 时才把采样数据落盘
		f.Close()
		logx.Infof("profile: cpu profiling disabled, %s", fn)
	})
}

// startMemProfile 开启堆内存剖析。
func (p *Profile) startMemProfile() {
	fn := createDumpFile("mem")
	f, err := os.Create(fn)
	if err != nil {
		logx.Errorf("profile: could not create memory profile %q: %v", fn, err)
		return
	}

	// 记住原采样率,Stop 时还原,不影响进程后续行为。
	old := runtime.MemProfileRate
	runtime.MemProfileRate = DefaultMemProfileRate
	logx.Infof("profile: memory profiling enabled (rate %d), %s", runtime.MemProfileRate, fn)
	p.closers = append(p.closers, func() {
		pprof.Lookup("heap").WriteTo(f, 0)
		f.Close()
		runtime.MemProfileRate = old
		logx.Infof("profile: memory profiling disabled, %s", fn)
	})
}

// startMutexProfile 开启锁竞争剖析。
func (p *Profile) startMutexProfile() {
	fn := createDumpFile("mutex")
	f, err := os.Create(fn)
	if err != nil {
		logx.Errorf("profile: could not create mutex profile %q: %v", fn, err)
		return
	}

	runtime.SetMutexProfileFraction(1)
	logx.Infof("profile: mutex profiling enabled, %s", fn)
	p.closers = append(p.closers, func() {
		if mp := pprof.Lookup("mutex"); mp != nil {
			mp.WriteTo(f, 0)
		}
		f.Close()
		runtime.SetMutexProfileFraction(0)
		logx.Infof("profile: mutex profiling disabled, %s", fn)
	})
}

// startThreadCreateProfile 开启 OS 线程创建记录剖析。
func (p *Profile) startThreadCreateProfile() {
	fn := createDumpFile("threadcreate")
	f, err := os.Create(fn)
	if err != nil {
		logx.Errorf("profile: could not create threadcreate profile %q: %v", fn, err)
		return
	}

	logx.Infof("profile: threadcreate profiling enabled, %s", fn)
	p.closers = append(p.closers, func() {
		if mp := pprof.Lookup("threadcreate"); mp != nil {
			mp.WriteTo(f, 0)
		}
		f.Close()
		logx.Infof("profile: threadcreate profiling disabled, %s", fn)
	})
}

// startTraceProfile 开启执行轨迹剖析(供 go tool trace 使用)。
func (p *Profile) startTraceProfile() {
	fn := createDumpFile("trace")
	f, err := os.Create(fn)
	if err != nil {
		logx.Errorf("profile: could not create trace output file %q: %v", fn, err)
		return
	}

	if err := trace.Start(f); err != nil {
		logx.Errorf("profile: could not start trace: %v", err)
		return
	}

	logx.Infof("profile: trace enabled, %s", fn)
	p.closers = append(p.closers, func() {
		trace.Stop()
		logx.Infof("profile: trace disabled, %s", fn)
	})
}

// Stop stops the profile and flushes any unwritten data.
// 停止剖析并落盘全部数据;CAS 防重入,重复调用是空操作。
func (p *Profile) Stop() {
	if !atomic.CompareAndSwapUint32(&p.stopped, 0, 1) {
		// someone has already called close
		return
	}
	p.close()
	// 还原全局开关,允许下次 StartProfile。
	atomic.StoreUint32(&started, 0)
}

// StartProfile starts a new profiling session.
// The caller should call the Stop method on the value returned
// to cleanly stop profiling.
// 启动全量剖析;已在剖析中时返回 noopStopper(空操作)。
// 另注册 SIGINT 监听:剖析期间按 Ctrl+C 会先 Stop 落盘,
// 再 reset 信号并重发给自己,保留默认的退出行为。
func StartProfile() Stopper {
	if !atomic.CompareAndSwapUint32(&started, 0, 1) {
		logx.Error("profile: Start() already called")
		return noopStopper
	}

	var prof Profile
	prof.startCpuProfile()
	prof.startMemProfile()
	prof.startMutexProfile()
	prof.startBlockProfile()
	prof.startTraceProfile()
	prof.startThreadCreateProfile()

	go func() {
		c := make(chan os.Signal, 1)
		signal.Notify(c, syscall.SIGINT)
		<-c

		logx.Info("profile: caught interrupt, stopping profiles")
		prof.Stop()

		signal.Reset()
		syscall.Kill(os.Getpid(), syscall.SIGINT)
	}()

	return &prof
}

// createDumpFile 生成 pprof 输出文件路径:
// {TempDir}/{进程名}-{pid}-{类型}-{时间}.pprof
func createDumpFile(kind string) string {
	command := path.Base(os.Args[0])
	pid := syscall.Getpid()
	return path.Join(os.TempDir(), fmt.Sprintf("%s-%d-%s-%s.pprof",
		command, pid, kind, time.Now().Format(timeFormat)))
}
