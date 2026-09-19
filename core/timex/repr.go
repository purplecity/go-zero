// ————————————————————————————————————————————————————————————————————————————
// repr —— 时长的字符串表示 —— 文件总结
//
// 把 time.Duration 转成以毫秒为单位、保留一位小数的紧凑字符串,
// 如 1.5s → "1500.0ms"、12.345ms → "12.3ms"。
// 日志字段、打点统计统一用它,单位一致便于检索与对比
// (logx 的 WithDuration/richLogger 的 duration 字段就是它)。
// ————————————————————————————————————————————————————————————————————————————
package timex

import (
	"fmt"
	"time"
)

// ReprOfDuration returns the string representation of given duration in ms.
// 以 "xx.xms" 形式返回时长:除以 time.Millisecond 换算成毫秒,
// float32 足够毫秒级精度,固定保留一位小数。
func ReprOfDuration(duration time.Duration) string {
	return fmt.Sprintf("%.1fms", float32(duration)/float32(time.Millisecond))
}
