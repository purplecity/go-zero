// ————————————————————————————————————————————————————————————————————————————
// textlinescanner —— 手写行迭代器(与 bufio.Scanner 的差异) —— 文件总结
//
// Scan/Line 两步式行迭代器,核心在 EOF 边界处理:
// ReadString('\n') 在"最后一行无换行符"时返回 (内容, EOF),
// 此时这行仍有效 —— 记下 hasNext=false 但本次返回 true,
// 让调用方把最后无换行的残行也取走,下次才结束。
// 相比 bufio.Scanner:不丢残行、接口可注入任意 reader,
// 且 Line() 能带回底层错误。
// ————————————————————————————————————————————————————————————————————————————
package iox

import (
	"bufio"
	"errors"
	"io"
	"strings"
)

// A TextLineScanner is a scanner that can scan lines from the given reader.
// 行迭代器:Scan 推进并报告是否还有,Line 取当前行。
type TextLineScanner struct {
	// reader 底层按 \n 切分的缓冲读。
	reader *bufio.Reader
	// hasNext 是否还可能有一行(含待取的残行)。
	hasNext bool
	// line 当前行的内容(Scan 成功后有效)。
	line string
	// err 底层读错误(非 EOF)。
	err error
}

// NewTextLineScanner returns a TextLineScanner with the given reader.
// 创建行迭代器。
func NewTextLineScanner(reader io.Reader) *TextLineScanner {
	return &TextLineScanner{
		reader:  bufio.NewReader(reader),
		hasNext: true,
	}
}

// Scan checks if scanner has more lines to read.
// 推进到下一行,返回是否还有。EOF 但带着内容(最后一行
// 无换行)时先返回 true 交出残行,下次才返回 false。
func (scanner *TextLineScanner) Scan() bool {
	if !scanner.hasNext {
		return false
	}

	line, err := scanner.reader.ReadString('\n')
	scanner.line = strings.TrimRight(line, "\n")
	if errors.Is(err, io.EOF) {
		// 标记没有下一行了,但本次的残行仍要交出去。
		scanner.hasNext = false
		return true
	} else if err != nil {
		scanner.err = err
		return false
	}
	return true
}

// Line returns the next available line.
// 取当前行与累积的错误。
func (scanner *TextLineScanner) Line() (string, error) {
	return scanner.line, scanner.err
}
