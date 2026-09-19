// ————————————————————————————————————————————————————————————————————————————
// lesswriter —— 限频 Writer —— 文件总结
//
// 给任意 io.Writer 套上 limitedExecutor 限频:窗口内只放行第一次
// Write,其余丢弃(但计数,放行时补 "Discarded N" 提示)。
// logx 内部用它包装堆栈输出(stackLog):控制台/文件 writer 装配时
// newLessWriter(errLog, StackCooldownMillis),防止连环报错刷调用栈。
// 注意:被丢弃时也返回 len(p), nil —— 写入方(标准 log.Logger 等)
// 不会感知丢弃,不产生错误路径。
// ————————————————————————————————————————————————————————————————————————————
package logx

import "io"

// lessWriter 限频写入器:包装目标 writer + 限频器。
type lessWriter struct {
	*limitedExecutor
	// writer 是真正接收数据的目标输出流。
	writer io.Writer
}

// newLessWriter 创建窗口为 milliseconds 毫秒的限频 writer。
func newLessWriter(writer io.Writer, milliseconds int) *lessWriter {
	return &lessWriter{
		limitedExecutor: newLimitedExecutor(milliseconds),
		writer:          writer,
	}
}

// Write 窗口内第一次写入放行,其余丢弃;返回值始终报告全部写入,
// 让调用方无感。
func (w *lessWriter) Write(p []byte) (n int, err error) {
	w.logOrDiscard(func() {
		w.writer.Write(p)
	})
	return len(p), nil
}
