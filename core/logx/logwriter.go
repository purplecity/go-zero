// ————————————————————————————————————————————————————————————————————————————
// logwriter —— 标准库 log.Logger 的适配器 —— 文件总结
//
// 把 *log.Logger 包装成 io.WriteCloser,作为 concreteWriter 中六股
// 日志流(info/error/severe/slow/stat/stack)的通用落盘适配层:
// concreteWriter 各流拿到的是 logWriter,内部转调 log.Logger.Print。
// Close 恒返回 nil:关闭职责在真正的文件持有者(如 RotateLogger),
// 这里的 logger 只是个引用。
// ————————————————————————————————————————————————————————————————————————————
package logx

import "log"

// logWriter 包装标准库 logger,使其满足 io.WriteCloser。
type logWriter struct {
	logger *log.Logger
}

// newLogWriter 用给定 logger 构造适配器;flags 由调用方传入
// (logx 统一传 vars.go 的 flags=0x0,即不带日期/时间前缀,
// 因为时间戳由日志条目自身提供)。
func newLogWriter(logger *log.Logger) logWriter {
	return logWriter{
		logger: logger,
	}
}

// Close 空实现,见文件头说明。
func (lw logWriter) Close() error {
	return nil
}

// Write 把字节流转给底层 logger 打印,返回值按约定报告全部长度已写入。
func (lw logWriter) Write(data []byte) (int, error) {
	lw.logger.Print(string(data))
	return len(data), nil
}
