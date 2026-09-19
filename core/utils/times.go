// ————————————————————————————————————————————————————————————————————————————
// times —— 计时器与时间戳工具 —— 文件总结
//
// 一、ElapsedTimer 秒表计时器
//
//	构造即开始计时(记录 timex.Now),随时读取已耗时:
//	  Duration  —— time.Duration 类型,程序内判断用;
//	  Elapsed   —— Go 原生格式字符串(如 "1.5s"),日志/提示用;
//	  ElapsedMs —— "1500.0ms" 毫秒格式,与 timex.ReprOfDuration 一致。
//	典型用法:defer 后打日志
//	  timer := utils.NewElapsedTimer()
//	  defer logx.Infof("cost: %v", timer.Elapsed())
//
// 二、CurrentMicros / CurrentMillis
//
//	当前 Unix 时间的微秒/毫秒整数,广泛用于 trace 时间戳、
//	缓存过期时间、统计打点等场景。
//
// ————————————————————————————————————————————————————————————————————————————
package utils

import (
	"fmt"
	"time"

	"github.com/zeromicro/go-zero/core/timex"
)

// An ElapsedTimer is a timer to track the elapsed time.
// ElapsedTimer 秒表:记录起点,随时读取已耗时(只读,并发安全
// 由不可变的 start 保证)。
type ElapsedTimer struct {
	// start 起点时刻(timex 相对时间,构造后不再变化)。
	start time.Duration
}

// NewElapsedTimer returns an ElapsedTimer.
// 创建计时器,从此刻开始计时。
func NewElapsedTimer() *ElapsedTimer {
	return &ElapsedTimer{
		start: timex.Now(),
	}
}

// Duration returns the elapsed time.
// 返回已经过的时间(Duration 类型,便于程序判断,如超时比较)。
func (et *ElapsedTimer) Duration() time.Duration {
	return timex.Since(et.start)
}

// Elapsed returns the string representation of elapsed time.
// 返回已经过时间的 Go 原生格式字符串,如 "1.5s"、"200ms"。
func (et *ElapsedTimer) Elapsed() string {
	return timex.Since(et.start).String()
}

// ElapsedMs returns the elapsed time of string on milliseconds.
// 返回毫秒格式字符串 "1500.0ms"(与 timex.ReprOfDuration 同格式)。
func (et *ElapsedTimer) ElapsedMs() string {
	return fmt.Sprintf("%.1fms", float32(timex.Since(et.start))/float32(time.Millisecond))
}

// CurrentMicros returns the current microseconds.
// 当前 Unix 时间戳(微秒)。
func CurrentMicros() int64 {
	return time.Now().UnixNano() / int64(time.Microsecond)
}

// CurrentMillis returns the current milliseconds.
// 当前 Unix 时间戳(毫秒)。
func CurrentMillis() int64 {
	return time.Now().UnixNano() / int64(time.Millisecond)
}
