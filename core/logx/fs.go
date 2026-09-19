// ————————————————————————————————————————————————————————————————————————————
// fs —— 文件系统抽象层 —— 文件总结
//
// 把 os.Create/Open/Remove、io.Copy、Closer.Close 抽象成接口,
// 目的只有一个:可测试性。gzipFile(rotatelogger.go)在压缩日志文件时
// 通过该接口操作文件,单测(rotatelogger_test)可以注入假实现来
// 模拟磁盘行为;生产环境永远使用下面的 realFileSystem。
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"io"
	"os"
)

// fileSys 是包级单例,gzipFile 通过它操作文件,测试中可被替换。
var fileSys realFileSystem

type (
	// fileSystem 抽象了日志压缩所需的全部文件操作。
	fileSystem interface {
		// Close 关闭打开的文件(转调 io.Closer.Close)。
		Close(closer io.Closer) error
		// Copy 把 reader 内容拷贝到 writer(压缩时读原文件写 gzip 流)。
		Copy(writer io.Writer, reader io.Reader) (int64, error)
		// Create 创建(或截断)文件。
		Create(name string) (*os.File, error)
		// Open 只读打开文件。
		Open(name string) (*os.File, error)
		// Remove 删除文件(压缩成功后删原文件用)。
		Remove(name string) error
	}

	// realFileSystem 是 fileSystem 的真实实现,直接包装标准库。
	realFileSystem struct{}
)

func (fs realFileSystem) Close(closer io.Closer) error {
	return closer.Close()
}

func (fs realFileSystem) Copy(writer io.Writer, reader io.Reader) (int64, error) {
	return io.Copy(writer, reader)
}

func (fs realFileSystem) Create(name string) (*os.File, error) {
	return os.Create(name)
}

func (fs realFileSystem) Open(name string) (*os.File, error) {
	return os.Open(name)
}

func (fs realFileSystem) Remove(name string) error {
	return os.Remove(name)
}
