// ————————————————————————————————————————————————————————————————————————————
// rangereader —— 只读文件指定区间的 io.Reader —— 文件总结
//
// 把 [start, stop) 字节区间包装成 io.Reader:实现标准 Read,
// 内部用 ReadAt 无状态读取并前移 start,像普通 Reader 一样
// 被 io.ReadAll/bufio.Scanner 消费。与 lookup.go 的
// SplitLineChunks 配套:大文件切块后,每块一个 RangeReader,
// 多 goroutine 并行扫完整行。越界(start≥文件大小或 stop<start)
// 返回 errExceedFileSize。
// ————————————————————————————————————————————————————————————————————————————
package filex

import (
	"errors"
	"os"
)

// errExceedFileSize indicates that the file size is exceeded.
// 区间越界错误。
var errExceedFileSize = errors.New("exceed file size")

// A RangeReader is used to read a range of content from a file.
// 文件区间阅读器:只暴露 [start, stop) 的内容。
type RangeReader struct {
	// file 底层文件(ReadAt 无偏移状态,多 reader 可共享)。
	file *os.File
	// start 当前读到的位置(随 Read 前移)。
	start int64
	// stop 区间终点(不含)。
	stop int64
}

// NewRangeReader returns a RangeReader, which will read the range of content from file.
// 创建区间阅读器:读 file 的 [start, stop) 字节。
func NewRangeReader(file *os.File, start, stop int64) *RangeReader {
	return &RangeReader{
		file:  file,
		start: start,
		stop:  stop,
	}
}

// Read reads the range of content into p.
// 标准 Read 语义:每次从 start 用 ReadAt 读进 p,
// 读完前移 start;区间剩余不足 len(p) 时收缩 p,
// 保证不越过 stop、区间读完返回 io.EOF。
func (rr *RangeReader) Read(p []byte) (n int, err error) {
	// 每次实时取文件大小:文件可能被追加/截断。
	stat, err := rr.file.Stat()
	if err != nil {
		return 0, err
	}

	// 区间非法或起点已越过文件尾:越界。
	if rr.stop < rr.start || rr.start >= stat.Size() {
		return 0, errExceedFileSize
	}

	// 区间剩余不足 p:收缩 p,防止读出 stop 之外的内容。
	if rr.stop-rr.start < int64(len(p)) {
		p = p[:rr.stop-rr.start]
	}

	n, err = rr.file.ReadAt(p, rr.start)
	if err != nil {
		return n, err
	}

	// 前移游标;读满 p 但到 stop 后,下次会收缩 p 至 0
	// 并由 ReadAt 返回 io.EOF。
	rr.start += int64(n)
	return
}
