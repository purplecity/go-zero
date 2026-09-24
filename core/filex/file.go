// ————————————————————————————————————————————————————————————————————————————
// file —— 按行读文件的首行/末行(不整读文件) —— 文件总结
//
// FirstLine:从头按 1KB 块正向读,遇到第一个换行即停;
// LastLine:从文件尾按 1KB 块反向读,遇到换行即停。
// 都用 ReadAt(不影响文件偏移),超大文件也只读必要的前后
// 几块 —— 典型用途:取 /etc/hostname、日志文件最新一行等。
// ————————————————————————————————————————————————————————————————————————————
package filex

import (
	"io"
	"os"
)

// bufSize 每次读取的块大小(1KB)。
const bufSize = 1024

// FirstLine returns the first line of the file.
// 读文件第一行:逐块正向读,遇到换行截断(文件无换行则全读)。
func FirstLine(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return firstLine(file)
}

// LastLine returns the last line of the file.
// 读文件最后一行:从文件尾逐块反向找换行(末尾有换行则跳过)。
func LastLine(filename string) (string, error) {
	file, err := os.Open(filename)
	if err != nil {
		return "", err
	}
	defer file.Close()

	return lastLine(filename, file)
}

// firstLine 正向取首行:ReadAt 按 1KB 块读,块内找到 '\n'
// 即返回;EOF 还没换行说明全文就一行,整段返回。
func firstLine(file *os.File) (string, error) {
	var first []byte
	var offset int64
	for {
		buf := make([]byte, bufSize)
		n, err := file.ReadAt(buf, offset)

		if err != nil && err != io.EOF {
			return "", err
		}

		for i := 0; i < n; i++ {
			if buf[i] == '\n' {
				return string(append(first, buf[:i]...)), nil
			}
		}

		if err == io.EOF {
			return string(append(first, buf[:n]...)), nil
		}

		first = append(first, buf[:n]...)
		offset += bufSize
	}
}

// lastLine 反向取末行:从文件尾按块回扫,块内从后往前找
// '\n',其后内容即末行;buf 拼到已积累内容的头部(保持顺序)。
// 跳过文件末尾恰好结尾的换行,否则会取到空行。
func lastLine(filename string, file *os.File) (string, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return "", err
	}

	var last []byte
	bufLen := int64(bufSize)
	offset := info.Size()

	// 从尾往头一块块读,直到拼出末行或读完。
	for offset > 0 {
		// 剩余不足一块时收缩 bufLen,offset 归零。
		if offset < bufLen {
			bufLen = offset
			offset = 0
		} else {
			offset -= bufLen
		}

		buf := make([]byte, bufLen)
		n, err := file.ReadAt(buf, offset)
		if err != nil && err != io.EOF {
			return "", err
		}

		if n == 0 {
			break
		}

		// 块尾正好是换行则去掉,避免末行为空。
		if buf[n-1] == '\n' {
			buf = buf[:n-1]
			n--
		} else {
			buf = buf[:n]
		}

		// 块内反向找换行:找到则其后即末行(拼到已积累内容前)。
		for i := n - 1; i >= 0; i-- {
			if buf[i] == '\n' {
				return string(append(buf[i+1:], last...)), nil
			}
		}

		// 本块没有换行:整块拼到积累内容头部,继续往前读。
		last = append(buf, last...)
	}

	return string(last), nil
}
