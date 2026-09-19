// ————————————————————————————————————————————————————————————————————————————
// util —— 日志输出的小工具函数 —— 文件总结
//
// 提供 caller 定位与时间戳两个基础能力:
//
//	getCaller    —— 通过 runtime.Caller 按深度回溯调用栈,取出调用位置;
//	prettyCaller —— 把完整文件路径精简为"最后两级目录/文件名:行号",
//	                例如 /home/x/go-zero/core/logx/logs.go:89
//	                → logx/logs.go:89,日志更短更聚焦;
//	getTimestamp —— 按全局 timeFormat(logs.go,可由 LogConf.TimeFormat
//	                定制)格式化当前时间。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"fmt"
	"runtime"
	"strings"
	"time"
)

// getCaller 获取向上第 callDepth 层调用者的"文件:行号"。
// 深度从 logx 公开 API 起算(callerDepth=4),保证指向用户代码;
// 取不到时返回空串(如 goroutine 栈底)。
func getCaller(callDepth int) string {
	_, file, line, ok := runtime.Caller(callDepth)
	if !ok {
		return ""
	}

	return prettyCaller(file, line)
}

// getTimestamp 按全局 timeFormat 生成当前时间字符串。
func getTimestamp() string {
	return time.Now().Format(timeFormat)
}

// prettyCaller 精简调用位置:去掉最后一段 "/" 之前的内容,
// 只保留"父目录/文件名:行号"两级,平衡信息量与可读性。
func prettyCaller(file string, line int) string {
	idx := strings.LastIndexByte(file, '/')
	if idx < 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}

	idx = strings.LastIndexByte(file[:idx], '/')
	if idx < 0 {
		return fmt.Sprintf("%s:%d", file, line)
	}

	return fmt.Sprintf("%s:%d", file[idx+1:], line)
}
