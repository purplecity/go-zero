// ————————————————————————————————————————————————————————————————————————————
// cpu_linux —— CPU 采样核心:cgroup 配额感知的利用率 —— 文件总结
//
// 【为什么必须感知 cgroup】容器里 /proc/stat 反映的是宿主机
// 整机,进程却只分到配额(如 2 核)。按整机算,容器用满自己
// 的 2 核时利用率可能只有 25%(宿主机 8 核)—— 脱落器会以为
// 很闲,继续放请求,直到被 throttled 卡死。所以分母用
// limit = min(配到的核数, cgroup quota),分子是本 cgroup
// 的 CPU 增量,算出的才是"配额利用率"。
//
// 【核心公式】(RefreshCpu 中)
//
//	usage = cpuDelta × cores × 1000 / (systemDelta × limit)
//
//	推导:systemCpuUsage 是整机所有核的总纳秒,单核平均
//	= systemDelta/cores;进程等效核数 = cpuDelta/(单核);
//	利用率 = 等效核数/limit × 1000(千分比,1000=1 核)。
//
// 【数据源】进程侧:cgroup v1 cpuacct.usage / v2 cpu.stat
// usage_usec;整机侧:/proc/stat 的 cpu 行前 8 列时钟拍
// (×10ms 转纳秒)。采不到(/proc 不存在,如 wsl 场景、
// 初始化失败)→ noCgroup=true,恒返 0(优雅降级)。
// ————————————————————————————————————————————————————————————————————————————
package internal

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/iox"
	"github.com/zeromicro/go-zero/core/logx"
)

const (
	// cpuTicks 时钟频率(每秒 100 拍,Linux USER_HZ)。
	cpuTicks = 100
	// cpuFields /proc/stat cpu 行取前 8 列(user..steal,
	// 不含 guest,避免与 user 重复计数)。
	cpuFields = 8
	// cpuMax 利用率上限 1000(千分比)。
	cpuMax = 1000
	// statFile 整机 CPU 数据源。
	statFile = "/proc/stat"
)

var (
	// preSystem/preTotal 上次采样的整机/进程 CPU 累计值
	//(增量计算的基线)。
	preSystem uint64
	preTotal  uint64
	// limit 利用率的分母:min(核数, cgroup quota)。
	limit float64
	// cores 配到的核数。
	cores uint64
	// noCgroup 采样不可用时降级:恒返 0。
	noCgroup bool
	initOnce sync.Once
)

// RefreshCpu refreshes cpu usage and returns.
// 采一次 CPU 千分比(0~1000;见文件头公式)。
func RefreshCpu() uint64 {
	initializeOnce()

	if noCgroup {
		return 0
	}

	total, err := cpuUsage()
	if err != nil {
		return 0
	}

	system, err := systemCpuUsage()
	if err != nil {
		return 0
	}

	var usage uint64
	// 本次增量 = 本次累计 − 上次累计。
	cpuDelta := total - preTotal
	systemDelta := system - preSystem
	if cpuDelta > 0 && systemDelta > 0 {
		usage = uint64(float64(cpuDelta*cores*cpuMax) / (float64(systemDelta) * limit))
		if usage > cpuMax {
			usage = cpuMax
		}
	}
	// 更新基线(无论是否算出,下次以本次为基准)。
	preSystem = system
	preTotal = total

	return usage
}

// cpuQuota 读 cgroup CPU 配额(核数;-1 = 无限制)。
func cpuQuota() (float64, error) {
	cg, err := currentCgroup()
	if err != nil {
		return 0, err
	}

	return cg.cpuQuota()
}

// cpuUsage 读本 cgroup 的 CPU 累计用量(纳秒)。
func cpuUsage() (uint64, error) {
	cg, err := currentCgroup()
	if err != nil {
		return 0, err
	}

	return cg.cpuUsage()
}

// effectiveCpus 读配到的核数(cpuset)。
func effectiveCpus() (int, error) {
	cg, err := currentCgroup()
	if err != nil {
		return 0, err
	}

	return cg.effectiveCpus()
}

// if /proc not present, ignore the cpu calculation, like wsl linux
// initialize 一次性初始化:核数、limit(= min(核数, quota))
// 与增量基线;失败由调用方降级为 noCgroup。
func initialize() error {
	cpus, err := effectiveCpus()
	if err != nil {
		return err
	}

	cores = uint64(cpus)
	limit = float64(cpus)
	quota, err := cpuQuota()
	if err == nil && quota > 0 {
		if quota < limit {
			// 配额比核数小(容器常态):分母用它。
			limit = quota
		}
	}

	preSystem, err = systemCpuUsage()
	if err != nil {
		return err
	}

	preTotal, err = cpuUsage()
	return err
}

// initializeOnce 只初始化一次;失败(含 panic)置 noCgroup,
// 此后 RefreshCpu 恒 0 —— 采样坏不能带崩主进程。
func initializeOnce() {
	initOnce.Do(func() {
		defer func() {
			if p := recover(); p != nil {
				noCgroup = true
				logx.Error(p)
			}
		}()

		if err := initialize(); err != nil {
			noCgroup = true
			logx.Error(err)
		}
	})
}

// systemCpuUsage 读整机 CPU 累计(换算成纳秒):/proc/stat
// 的 "cpu" 行前 8 列时钟拍求和 × (1s/100 拍)。
func systemCpuUsage() (uint64, error) {
	lines, err := iox.ReadTextLines(statFile, iox.WithoutBlank())
	if err != nil {
		return 0, err
	}

	for _, line := range lines {
		fields := strings.Fields(line)
		if fields[0] == "cpu" {
			if len(fields) < cpuFields {
				return 0, fmt.Errorf("bad format of cpu stats")
			}

			var totalClockTicks uint64
			for _, i := range fields[1:cpuFields] {
				v, err := parseUint(i)
				if err != nil {
					return 0, err
				}

				totalClockTicks += v
			}

			return (totalClockTicks * uint64(time.Second)) / cpuTicks, nil
		}
	}

	return 0, errors.New("bad stats format")
}
