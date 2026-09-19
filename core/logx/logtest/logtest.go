// ————————————————————————————————————————————————————————————————————————————
// logtest —— logx 的测试辅助包(logtest) —— 文件总结
//
// 供单测捕获/控制 logx 行为,全部函数都会在 t.Cleanup 里恢复原状,
// 多个测试互不污染:
//
//	Discard(t)       丢弃所有日志(测试静默);
//	NewCollector(t)  捕获日志到内存 Buffer,可用 Content/Bytes 断言;
//	PanicOnFatal(t)  把 Must 的 os.Exit 换成 panic,避免测试进程被杀。
//
// ————————————————————————————————————————————————————————————————————————————
package logtest

import (
	"bytes"
	"encoding/json"
	"io"
	"testing"

	"github.com/zeromicro/go-zero/core/logx"
)

// Buffer 日志收集器:包装内存 buffer,提供多种读取方式。
type Buffer struct {
	buf *bytes.Buffer
	t   *testing.T
}

// Discard 把 logx 的 writer 换成 io.Discard(所有日志静默丢弃),
// 测试结束自动恢复原 writer。用于不想让被测代码输出日志的场景。
func Discard(t *testing.T) {
	prev := logx.Reset()
	logx.SetWriter(logx.NewWriter(io.Discard))

	t.Cleanup(func() {
		logx.SetWriter(prev)
	})
}

// NewCollector returns a collector to store log contents.
// 创建日志收集器:logx 的全部输出被捕获进内存 Buffer,
// 测试结束自动恢复原 writer。配合 Content()/String() 断言日志内容。
func NewCollector(t *testing.T) *Buffer {
	var buf bytes.Buffer
	writer := logx.NewWriter(&buf)
	prev := logx.Reset()
	logx.SetWriter(writer)

	t.Cleanup(func() {
		logx.SetWriter(prev)
	})

	return &Buffer{
		buf: &buf,
		t:   t,
	}
}

// Bytes returns the raw contents of the collected logs as bytes.
// 返回捕获的原始字节。
func (b *Buffer) Bytes() []byte {
	return b.buf.Bytes()
}

// Content returns the content of the collected logs.
// 解析 JSON 日志并返回 content 字段:
// content 是字符串时直接返回;是对象/数字等其他 JSON 值时
// 重新序列化成字符串返回;解析失败返回空串。
func (b *Buffer) Content() string {
	var m map[string]interface{}
	if err := json.Unmarshal(b.buf.Bytes(), &m); err != nil {
		return ""
	}

	content, ok := m["content"]
	if !ok {
		return ""
	}

	switch val := content.(type) {
	case string:
		return val
	default:
		// err is impossible to be not nil, unmarshaled from b.buf.Bytes()
		bs, _ := json.Marshal(content)
		return string(bs)
	}
}

// Reset resets the collected log buffer.
// 清空已捕获的内容(一个测试内分段断言时用)。
func (b *Buffer) Reset() {
	b.buf.Reset()
}

// String returns the collected log contents as a string.
// 返回捕获的全部文本。
func (b *Buffer) String() string {
	return b.buf.String()
}

// PanicOnFatal makes logx.Must panic instead of exiting on fatal errors.
// 让 Must 出错时 panic 而不是 os.Exit(1),否则测试进程会被直接杀掉。
// 用 CAS 防止并发测试重复切换;测试结束自动恢复。
func PanicOnFatal(t *testing.T) {
	ok := logx.ExitOnFatal.CompareAndSwap(true, false)
	if !ok {
		return
	}

	t.Cleanup(func() {
		logx.ExitOnFatal.CompareAndSwap(false, true)
	})
}
