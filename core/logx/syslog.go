// ————————————————————————————————————————————————————————————————————————————
// syslog —— 标准库日志重定向 —— 文件总结
//
// Go 标准库 log 的全局输出默认打到 stderr,格式与业务日志体系脱节。
// CollectSysLog 把 log.SetOutput 指向本文件的 redirector,让标准库
// 的日志(第三方库经常直接 log.Printf)统一流经 logx 的 Info 级别
// 输出,格式、落盘、采集与业务日志保持一致。服务启动时调用一次即可。
// ————————————————————————————————————————————————————————————————————————————
package logx

import "log"

// redirector 实现了 io.Writer,把写入的内容转成 logx Info 日志。
type redirector struct{}

// CollectSysLog redirects system log into logx info
// 开启标准库日志重定向,之后 log.Print/log.Printf 等的输出
// 都会以 Info 级别经过 logx 输出。
func CollectSysLog() {
	log.SetOutput(new(redirector))
}

// Write 实现 io.Writer:标准库 logger 每次输出都会调用这里。
func (r *redirector) Write(p []byte) (n int, err error) {
	Info(string(p))
	// 按约定返回"全部写入",不让标准库 logger 报错。
	return len(p), nil
}
