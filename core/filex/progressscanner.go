// ————————————————————————————————————————————————————————————————————————————
// progressscanner —— 带进度条的行扫描器(装饰器) —— 文件总结
//
// 装饰器模式:包装任意 Scanner,每次 Text() 顺带把该行字节数
// 累进进度条,命令行处理大文件时能看到消费进度。
// Scan() 直接透传被包装者;只重写 Text() 加进度上报。
// ————————————————————————————————————————————————————————————————————————————
package filex

import "gopkg.in/cheggaaa/pb.v1"

type (
	// A Scanner is used to read lines.
	// 行扫描器接口(Scan 是否还有剩余 + Text 取下一行)。
	Scanner interface {
		// Scan checks if it has remaining to read.
		// 是否还有剩余可读。
		Scan() bool
		// Text returns next line.
		// 取下一行。
		Text() string
	}

	// progressScanner 带进度条的扫描器:内嵌 Scanner 透传
	// 能力,Text 额外累加进度。
	progressScanner struct {
		Scanner
		// bar 进度条(按已处理字节数推进)。
		bar *pb.ProgressBar
	}
)

// NewProgressScanner returns a Scanner with progress indicator.
// 给扫描器包一层进度上报。
func NewProgressScanner(scanner Scanner, bar *pb.ProgressBar) Scanner {
	return &progressScanner{
		Scanner: scanner,
		bar:     bar,
	}
}

// Text 取下一行,并把行长(含换行 +1)累进进度条。
func (ps *progressScanner) Text() string {
	s := ps.Scanner.Text()
	ps.bar.Add64(int64(len(s)) + 1) // take newlines into account
	return s
}
