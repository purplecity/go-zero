// ————————————————————————————————————————————————————————————————————————————
// goroutines —— goroutine 栈 dump(信号 SIGUSR1 触发) —— 文件总结
//
// 收到 SIGUSR1 时,把进程全部 goroutine 的调用栈写到 /tmp 下的
// dump 文件(带 pprof debug=2 的人类可读格式)。排查"服务卡死、
// 哪里阻塞"的第一手段:kill -USR1 <pid> 后看文件即可。
// creator 接口把"创建文件"抽象出来,是为了单测可注入假实现。
// ————————————————————————————————————————————————————————————————————————————
//go:build linux || darwin || freebsd

package proc

import (
	"fmt"
	"os"
	"path"
	"runtime/pprof"
	"syscall"
	"time"

	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// goroutineProfile pprof 的 goroutine 剖析项名。
	goroutineProfile = "goroutine"
	// debugLevel pprof 输出级别 2:含完整栈的详细文本格式。
	debugLevel = 2
)

// creator 创建 dump 文件的抽象(便于测试注入)。
type creator interface {
	Create(name string) (file *os.File, err error)
}

// dumpGoroutines 把全部 goroutine 栈写入 /tmp 下的 dump 文件:
// 文件名 {进程名}-{pid}-goroutines-{时间}.dump
func dumpGoroutines(ctor creator) {
	command := path.Base(os.Args[0])
	pid := syscall.Getpid()
	dumpFile := path.Join(os.TempDir(), fmt.Sprintf("%s-%d-goroutines-%s.dump",
		command, pid, time.Now().Format(timeFormat)))

	logx.Infof("Got dump goroutine signal, printing goroutine profile to %s", dumpFile)

	if f, err := ctor.Create(dumpFile); err != nil {
		logx.Errorf("Failed to dump goroutine profile, error: %v", err)
	} else {
		defer f.Close()
		// debug=2:输出每个 goroutine 的完整状态与调用栈文本。
		pprof.Lookup(goroutineProfile).WriteTo(f, debugLevel)
	}
}

// fileCreator 真实文件系统实现。
type fileCreator struct{}

func (fc fileCreator) Create(name string) (file *os.File, err error) {
	return os.Create(name)
}
