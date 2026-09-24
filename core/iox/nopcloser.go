// ————————————————————————————————————————————————————————————————————————————
// nopcloser —— Close 空操作的 WriteCloser 适配器 —— 文件总结
//
// 标准库只有 io.NopCloser(io.Reader) 的读方向;这里是写方向:
// 给任意 io.Writer 补一个 No-op 的 Close,凑成 io.WriteCloser。
// 用于"调用方要求 WriteCloser、但底层资源不需要/不能关"的
// 接口适配(如把 bytes.Buffer 塞给要求 Closer 的压缩流)。
// ————————————————————————————————————————————————————————————————————————————
package iox

import "io"

// nopCloser 内嵌 Writer 透传写能力,Close 恒为 nil。
type nopCloser struct {
	io.Writer
}

// Close 空操作:不关任何东西。
func (nopCloser) Close() error {
	return nil
}

// NopCloser returns an io.WriteCloser that does nothing on calling Close.
// 把 Writer 适配成 WriteCloser(Close 为空操作)。
func NopCloser(w io.Writer) io.WriteCloser {
	return nopCloser{w}
}
