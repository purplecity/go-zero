// ————————————————————————————————————————————————————————————————————————————
// read —— 常用读取工具(复制流/精确读/读文本行) —— 文件总结
//
// DupReadCloser/LimitDupReadCloser:把一个流"分身"成两个
// (边读边拷贝进 buffer)—— 日志/上传场景一份给业务消费、
// 一份留档;Limit 版限制第二份最多 n 字节(防超大流撑爆内存)。
// ReadBytes:凑满 len(buf) 才返回 —— io.Reader 的 Read 允许
// 短读,读定长消息(如长度前缀协议)必须循环补齐。
// ReadText/ReadTextLines:整读去空白 / 按行读 + 三个过滤选项
// (保留空白/去空行/跳过指定前缀)。
// ————————————————————————————————————————————————————————————————————————————
package iox

import (
	"bufio"
	"bytes"
	"io"
	"os"
	"strings"
)

type (
	// textReadOptions 文本行读取的过滤选项。
	textReadOptions struct {
		// keepSpace 保留行首尾空白(默认 Trim)。
		keepSpace bool
		// withoutBlanks 跳过空行。
		withoutBlanks bool
		// omitPrefix 跳过带此前缀的行(如 "#")。
		omitPrefix string
	}

	// TextReadOption defines the method to customize the text reading functions.
	// 读取选项(函数式选项模式)。
	TextReadOption func(*textReadOptions)
)

// DupReadCloser returns two io.ReadCloser that read from the first will be written to the second.
// The first returned reader needs to be read first, because the content
// read from it will be written to the underlying buffer of the second reader.
// 流分身:TeeReader 边读边把内容拷进 buffer,第二个 reader
// 读的是这份拷贝 —— 注意必须先消费第一个,第二个才有数据。
func DupReadCloser(reader io.ReadCloser) (io.ReadCloser, io.ReadCloser) {
	var buf bytes.Buffer
	tee := io.TeeReader(reader, &buf)
	return io.NopCloser(tee), io.NopCloser(&buf)
}

// KeepSpace customizes the reading functions to keep leading and tailing spaces.
// 选项:保留行首尾空白(默认会 Trim)。
func KeepSpace() TextReadOption {
	return func(o *textReadOptions) {
		o.keepSpace = true
	}
}

// LimitDupReadCloser returns two io.ReadCloser that read from the first will be written to the second.
// But the second io.ReadCloser is limited to up to n bytes.
// The first returned reader needs to be read first, because the content
// read from it will be written to the underlying buffer of the second reader.
// 限量流分身:第二份最多保留前 n 字节(如日志只留档头部)。
func LimitDupReadCloser(reader io.ReadCloser, n int64) (io.ReadCloser, io.ReadCloser) {
	var buf bytes.Buffer
	tee := LimitTeeReader(reader, &buf, n)
	return io.NopCloser(tee), io.NopCloser(&buf)
}

// ReadBytes reads exactly the bytes with the length of len(buf)
// 精确读满 buf:循环 Read 补齐短读(io.Reader 单次可能少给),
// 凑不够(EOF/错误)即返回错误。
func ReadBytes(reader io.Reader, buf []byte) error {
	var got int

	for got < len(buf) {
		n, err := reader.Read(buf[got:])
		if err != nil {
			return err
		}

		got += n
	}

	return nil
}

// ReadText reads content from the given file with leading and tailing spaces trimmed.
// 整读文件文本并 Trim 首尾空白。
func ReadText(filename string) (string, error) {
	content, err := os.ReadFile(filename)
	if err != nil {
		return "", err
	}

	return strings.TrimSpace(string(content)), nil
}

// ReadTextLines reads the text lines from given file.
// 按行读取文件,可配过滤选项:Trim 空白(默认)/跳过空行/
// 跳过指定前缀行(如注释 "#")。
//
// Scanner 按行读的三个坑:
// 1. 单行上限 64KB(bufio.MaxScanTokenSize):内部缓冲 4KB 起步
//    翻倍增长,最多到 64KB,一行仍放不下即报 ErrTooLong;
//    超长行需先调 scanner.Buffer() 放大上限。
// 2. Scan() 返回 false 可能是 EOF 也可能是读错误,循环退出后
//    必须查 scanner.Err()(即下方 return 的 err),否则吞掉错误。
// 3. Text() 每次都把内部 []byte 拷贝成新 string;逐行处理大文件
//    可用 Bytes() 免这次拷贝,但其内容在下次 Scan() 后失效,
//    不可留存。
func ReadTextLines(filename string, opts ...TextReadOption) ([]string, error) {
	var readOpts textReadOptions
	for _, opt := range opts {
		opt(&readOpts)
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var lines []string
	scanner := bufio.NewScanner(file)
	for scanner.Scan() {
		line := scanner.Text()
		// 默认 Trim;KeepSpace 选项保留原始空白。
		if !readOpts.keepSpace {
			line = strings.TrimSpace(line)
		}
		// 三道过滤:空行、前缀行。
		if readOpts.withoutBlanks && len(line) == 0 {
			continue
		}
		if len(readOpts.omitPrefix) > 0 && strings.HasPrefix(line, readOpts.omitPrefix) {
			continue
		}

		lines = append(lines, line)
	}

	return lines, scanner.Err()
}

// WithoutBlank customizes the reading functions to ignore blank lines.
// 选项:跳过空行。
func WithoutBlank() TextReadOption {
	return func(o *textReadOptions) {
		o.withoutBlanks = true
	}
}

// OmitWithPrefix customizes the reading functions to ignore the lines with given leading prefix.
// 选项:跳过以 prefix 开头的行。
func OmitWithPrefix(prefix string) TextReadOption {
	return func(o *textReadOptions) {
		o.omitPrefix = prefix
	}
}
