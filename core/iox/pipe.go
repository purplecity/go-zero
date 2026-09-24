// ————————————————————————————————————————————————————————————————————————————
// pipe —— 进程级 stdin/stdout 重定向 —— 文件总结
//
// RedirectInOut 用 os.Pipe 把进程的 os.Stdin/os.Stdout 换成
// 管道两端,返回 restore 还原函数 —— 调用方用完必须调它。
// 用途:测试/工具场景里捕获子进程输出、注入标准输入
// (注意:替换的是进程全局变量,并发使用不安全)。
// ————————————————————————————————————————————————————————————————————————————
package iox

import "os"

// RedirectInOut redirects stdin to r, stdout to w, and callers need to call restore afterward.
// 重定向进程 stdin/stdout 到新建管道,返回还原函数
// (用完务必调用,否则全局标准流一直指向管道)。
func RedirectInOut() (restore func(), err error) {
	var r, w *os.File
	r, w, err = os.Pipe()
	if err != nil {
		return
	}

	// 记住原值,restore 时换回。
	ow := os.Stdout
	os.Stdout = w
	or := os.Stdin
	os.Stdin = r
	restore = func() {
		os.Stdin = or
		os.Stdout = ow
	}

	return
}
