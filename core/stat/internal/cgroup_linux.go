// ————————————————————————————————————————————————————————————————————————————
// cgroup_linux —— cgroup v1/v2 适配:读 CPU 配额与用量 —— 文件总结
//
// 两代 cgroup 的文件接口不同,统一成 cgroup 接口三方法:
//
//	cpuQuota       配额核数(v1: cpu.cfs_quota_us/period;
//	                 v2: cpu.max 两段式;"max" = 无限制)
//	cpuUsage       累计 CPU 用量纳秒(v1: cpuacct.usage;
//	                 v2: cpu.stat 的 usage_usec×1ms)
//	effectiveCpus  配到的核数(cpuset)
//
// 版本判定:statfs 看 /sys/fs/cgroup 的文件系统魔法数
// (CGROUP2_SUPER_MAGIC = v2 统一层);v1 用 /proc/self/cgroup
// 找到本进程各控制器路径(只关心 cpu 前缀的)。
// 附:parseUints 解析 "0-2,4" 这类 CPU 列表;runningInUserNS
// 判断用户命名空间(容器常见,userns 里 cgroup 目录可能不存在,
// 容错处理)。
// ————————————————————————————————————————————————————————————————————————————
package internal

import (
	"bufio"
	"errors"
	"fmt"
	"math"
	"os"
	"path"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/iox"
	"github.com/zeromicro/go-zero/core/lang"
	"golang.org/x/sys/unix"
)

const (
	// cgroupDir cgroup 挂载根。
	cgroupDir = "/sys/fs/cgroup"
	// v2 的配额文件("max period" 或 "quota period" 两段)。
	cpuMaxFile = cgroupDir + "/cpu.max"
	// v2 的用量文件(usage_usec 等键值行)。
	cpuStatFile = cgroupDir + "/cpu.stat"
	// v2 的有效核列表。
	cpusetFile = cgroupDir + "/cpuset.cpus.effective"
)

var (
	// v2 判定只做一次(statfs)。
	isUnifiedOnce sync.Once
	isUnified     bool
	// 用户命名空间判定只做一次。
	inUserNS bool
	nsOnce   sync.Once
)

// cgroup 两代实现的统一接口(配额/用量/核数)。
type cgroup interface {
	cpuQuota() (float64, error)
	cpuUsage() (uint64, error)
	effectiveCpus() (int, error)
}

// currentCgroup 按版本返回适配器。
func currentCgroup() (cgroup, error) {
	if isCgroup2UnifiedMode() {
		return currentCgroupV2()
	}

	return currentCgroupV1()
}

// cgroupV1 v1 实现:控制器 → 各自目录的散文件。
type cgroupV1 struct {
	cgroups map[string]string
}

// cpuQuota 配额核数 = quota_us / period_us(-1 = 无限制)。
func (c *cgroupV1) cpuQuota() (float64, error) {
	quotaUs, err := c.cpuQuotaUs()
	if err != nil {
		return 0, err
	}

	if quotaUs == -1 {
		return -1, nil
	}

	periodUs, err := c.cpuPeriodUs()
	if err != nil {
		return 0, err
	}

	return float64(quotaUs) / float64(periodUs), nil
}

// cpuPeriodUs v1 调度周期(微秒)。
func (c *cgroupV1) cpuPeriodUs() (uint64, error) {
	data, err := iox.ReadText(path.Join(c.cgroups["cpu"], "cpu.cfs_period_us"))
	if err != nil {
		return 0, err
	}

	return parseUint(data)
}

// cpuQuotaUs v1 每周期配额(微秒;-1 = 无限制)。
func (c *cgroupV1) cpuQuotaUs() (int64, error) {
	data, err := iox.ReadText(path.Join(c.cgroups["cpu"], "cpu.cfs_quota_us"))
	if err != nil {
		return 0, err
	}

	return strconv.ParseInt(data, 10, 64)
}

// cpuUsage v1 累计用量(纳秒,cpuacct.usage 原生单位)。
func (c *cgroupV1) cpuUsage() (uint64, error) {
	data, err := iox.ReadText(path.Join(c.cgroups["cpuacct"], "cpuacct.usage"))
	if err != nil {
		return 0, err
	}

	return parseUint(data)
}

// effectiveCpus v1 有效核数(cpuset 列表长度)。
func (c *cgroupV1) effectiveCpus() (int, error) {
	data, err := iox.ReadText(path.Join(c.cgroups["cpuset"], "cpuset.cpus"))
	if err != nil {
		return 0, err
	}

	cpus, err := parseUints(data)
	if err != nil {
		return 0, err
	}

	return len(cpus), nil
}

// cgroupV2 v2 实现:统一层级下的键值/两段式文件。
type cgroupV2 struct {
	// cpu.stat 解析出的键值(如 usage_usec → 值)。
	cgroups map[string]string
}

// cpuQuota 配额核数:cpu.max 两段 "quota period"
// ("max ..." = 无限制)。
func (c *cgroupV2) cpuQuota() (float64, error) {
	data, err := iox.ReadText(cpuMaxFile)
	if err != nil {
		return 0, err
	}

	fields := strings.Fields(data)
	if len(fields) != 2 {
		return 0, fmt.Errorf("cgroup: bad /sys/fs/cgroup/cpu.max file: %s", data)
	}

	if fields[0] == "max" {
		return -1, nil
	}

	quotaUs, err := strconv.ParseInt(fields[0], 10, 64)
	if err != nil {
		return 0, err
	}

	periodUs, err := strconv.ParseUint(fields[1], 10, 64)
	if err != nil {
		return 0, err
	}

	return float64(quotaUs) / float64(periodUs), nil
}

// cpuUsage 累计用量纳秒 = usage_usec × 1ms。
func (c *cgroupV2) cpuUsage() (uint64, error) {
	usec, err := parseUint(c.cgroups["usage_usec"])
	if err != nil {
		return 0, err
	}

	return usec * uint64(time.Microsecond), nil
}

// effectiveCpus v2 有效核数。
func (c *cgroupV2) effectiveCpus() (int, error) {
	data, err := iox.ReadText(cpusetFile)
	if err != nil {
		return 0, err
	}

	cpus, err := parseUints(data)
	if err != nil {
		return 0, err
	}

	return len(cpus), nil
}

// currentCgroupV1 从 /proc/self/cgroup 找本进程的各控制器路径,
// 只保留 cpu 前缀的控制器(cpu/cpuacct/cpuset)。
func currentCgroupV1() (cgroup, error) {
	cgroupFile := fmt.Sprintf("/proc/%d/cgroup", os.Getpid())
	lines, err := iox.ReadTextLines(cgroupFile, iox.WithoutBlank())
	if err != nil {
		return nil, err
	}

	cgroups := make(map[string]string)
	for _, line := range lines {
		cols := strings.Split(line, ":")
		if len(cols) != 3 {
			return nil, fmt.Errorf("invalid cgroup line: %s", line)
		}

		subsys := cols[1]
		// only read cpu staff
		if !strings.HasPrefix(subsys, "cpu") {
			continue
		}

		// https://man7.org/linux/man-pages/man7/cgroups.7.html
		// comma-separated list of controllers for cgroup version 1
		for val := range strings.SplitSeq(subsys, ",") {
			cgroups[val] = path.Join(cgroupDir, val)
		}
	}

	return &cgroupV1{
		cgroups: cgroups,
	}, nil
}

// currentCgroupV2 读根 cgroup 的 cpu.stat 键值表
// (v2 统一层,不需要 /proc/self/cgroup 定位路径)。
func currentCgroupV2() (cgroup, error) {
	lines, err := iox.ReadTextLines(cpuStatFile, iox.WithoutBlank())
	if err != nil {
		return nil, err
	}

	cgroups := make(map[string]string)
	for _, line := range lines {
		cols := strings.Fields(line)
		if len(cols) != 2 {
			return nil, fmt.Errorf("invalid cgroupV2 line: %s", line)
		}

		cgroups[cols[0]] = cols[1]
	}

	return &cgroupV2{
		cgroups: cgroups,
	}, nil
}

// isCgroup2UnifiedMode returns whether we are running in cgroup v2 unified mode.
// v2 判定:statfs 魔法数;userns 下目录缺失按 v1 容错。
func isCgroup2UnifiedMode() bool {
	isUnifiedOnce.Do(func() {
		var st unix.Statfs_t
		err := unix.Statfs(cgroupDir, &st)
		if err != nil {
			if os.IsNotExist(err) && runningInUserNS() {
				// ignore the "not found" error if running in userns
				isUnified = false
				return
			}
			panic(fmt.Sprintf("cannot statfs cgroup root: %s", err))
		}
		isUnified = st.Type == unix.CGROUP2_SUPER_MAGIC
	})

	return isUnified
}

// parseUint 宽容解析:溢出/负数都归 0(v2 某些值可能超大)。
func parseUint(s string) (uint64, error) {
	v, err := strconv.ParseInt(s, 10, 64)
	if err != nil {
		if errors.Is(err, strconv.ErrRange) {
			return 0, nil
		}

		return 0, fmt.Errorf("cgroup: bad int format: %s", s)
	}

	if v < 0 {
		return 0, nil
	}

	return uint64(v), nil
}

// parseUints 解析 CPU 列表 "0-2,4"(区间展开 + 去重)。
func parseUints(val string) ([]uint64, error) {
	if val == "" {
		return nil, nil
	}

	var sets []uint64
	ints := make(map[uint64]lang.PlaceholderType)
	for r := range strings.SplitSeq(val, ",") {
		if strings.Contains(r, "-") {
			fields := strings.SplitN(r, "-", 2)
			minimum, err := parseUint(fields[0])
			if err != nil {
				return nil, fmt.Errorf("cgroup: bad int list format: %s", val)
			}

			maximum, err := parseUint(fields[1])
			if err != nil {
				return nil, fmt.Errorf("cgroup: bad int list format: %s", val)
			}

			if maximum < minimum {
				return nil, fmt.Errorf("cgroup: bad int list format: %s", val)
			}

			for i := minimum; i <= maximum; i++ {
				if _, ok := ints[i]; !ok {
					ints[i] = lang.Placeholder
					sets = append(sets, i)
				}
			}
		} else {
			v, err := parseUint(r)
			if err != nil {
				return nil, err
			}

			if _, ok := ints[v]; !ok {
				ints[v] = lang.Placeholder
				sets = append(sets, v)
			}
		}
	}

	return sets, nil
}

// runningInUserNS detects whether we are currently running in a user namespace.
// 用户命名空间判定:uid_map 首行非"0 0 全范围"即在 userns
// (容器/无 root 场景常见)。
func runningInUserNS() bool {
	nsOnce.Do(func() {
		file, err := os.Open("/proc/self/uid_map")
		if err != nil {
			// This kernel-provided file only exists if user namespaces are supported
			return
		}
		defer file.Close()

		buf := bufio.NewReader(file)
		l, _, err := buf.ReadLine()
		if err != nil {
			return
		}

		line := string(l)
		var a, b, c int64
		fmt.Sscanf(line, "%d %d %d", &a, &b, &c)

		// We assume we are in the initial user namespace if we have a full
		// range - 4294967295 uids starting at uid 0.
		if a == 0 && b == 0 && c == math.MaxUint32 {
			return
		}

		inUserNS = true
	})

	return inUserNS
}
