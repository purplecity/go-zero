// ————————————————————————————————————————————————————————————————————————————
// color —— 日志着色辅助 —— 文件总结
//
// 提供给 logx 内部使用的着色函数,核心约束:只在 plain(纯文本)编码
// 下才真正加 ANSI 颜色转义序列;json 编码时原样返回。
// 原因:JSON 日志是给机器/日志采集系统消费的,混入 ANSI 转义符
// 会破坏 JSON 解析与字段提取;彩色输出只服务于终端上的人。
// encoding 变量定义在 logs.go,运行期原子读取,无锁、并发安全。
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"sync/atomic"

	"github.com/zeromicro/go-zero/core/color"
)

// WithColor is a helper function to add color to a string, only in plain encoding.
// 给 text 加颜色,仅当编码为 plain 时生效;json 编码时原样返回 text。
func WithColor(text string, colour color.Color) string {
	if atomic.LoadUint32(&encoding) == plainEncodingType {
		return color.WithColor(text, colour)
	}

	return text
}

// WithColorPadding is a helper function to add color to a string with leading and trailing spaces,
// only in plain encoding.
// 同上,但会给文本前后各加一个空格,让彩色标签(如级别)与
// 后面的内容在终端上隔开、更易读。
func WithColorPadding(text string, colour color.Color) string {
	if atomic.LoadUint32(&encoding) == plainEncodingType {
		return color.WithColorPadding(text, colour)
	}

	return text
}
