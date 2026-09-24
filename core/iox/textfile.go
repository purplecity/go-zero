// ————————————————————————————————————————————————————————————————————————————
// textfile —— 数文件行数(流式,不整读) —— 文件总结
//
// CountLines 按 32KB 块流式读取,数 '\n' 个数;文件末尾
// 无换行符时(最后一行不结尾),EOF 前补数 1 行
// (noEol 标记跨块记忆"上一块末尾不是换行")。
// 数十 GB 文件也只占 32KB 内存。
// ————————————————————————————————————————————————————————————————————————————
package iox

import (
	"bytes"
	"errors"
	"io"
	"os"
)

// bufSize 读取块大小(32KB)。
const bufSize = 32 * 1024

// CountLines returns the number of lines in the file.
// 统计文件行数:块读 + bytes.Count 数换行符,
// 末行无换行符的补 1。
func CountLines(file string) (int, error) {
	f, err := os.Open(file)
	if err != nil {
		return 0, err
	}
	defer f.Close()

	// noEol 上一块结尾不是 '\n'(说明有一行还在延续)。
	var noEol bool
	buf := make([]byte, bufSize)
	count := 0
	lineSep := []byte{'\n'}

	for {
		c, err := f.Read(buf)
		count += bytes.Count(buf[:c], lineSep)

		switch {
		case errors.Is(err, io.EOF):
			// EOF 时若末尾没有换行,最后一行也要数上。
			if noEol {
				count++
			}
			return count, nil
		case err != nil:
			return count, err
		}

		// 记住本块是否以换行结尾,供下一轮/EOF 判断。
		noEol = c > 0 && buf[c-1] != '\n'
	}
}
