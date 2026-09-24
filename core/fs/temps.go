// ————————————————————————————————————————————————————————————————————————————
// temps —— 内容确定的临时文件工具(主要供测试用) —— 文件总结
//
// 两个函数都是"把给定文本写进临时文件":
//
//	TempFileWithText   返回打开的 *os.File(用完 Close,文件按名删);
//	TempFilenameWithText 返回文件路径(内部已 Close,用完按名删)。
//
// 文件名用内容 MD5 作前缀:同内容多次创建,文件名可预期、可去重。
// go-zero 内部大量用在"需要真实文件路径"的单测里。
// ————————————————————————————————————————————————————————————————————————————
package fs

import (
	"os"

	"github.com/zeromicro/go-zero/core/hash"
)

// TempFileWithText creates the temporary file with the given content,
// and returns the opened *os.File instance.
// The file is kept as open, the caller should close the file handle,
// and remove the file by name.
// 创建带指定内容的临时文件并保持打开:调用方负责 Close 与按名删除。
func TempFileWithText(text string) (*os.File, error) {
	tmpFile, err := os.CreateTemp(os.TempDir(), hash.Md5Hex([]byte(text)))
	if err != nil {
		return nil, err
	}

	if err := os.WriteFile(tmpFile.Name(), []byte(text), os.ModeTemporary); err != nil {
		return nil, err
	}

	return tmpFile, nil
}

// TempFilenameWithText creates the file with the given content,
// and returns the filename (full path).
// The caller should remove the file after use.
// 创建带指定内容的临时文件,返回路径(内部已 Close):
// 调用方只需要路径时用它,用完按名删除。
func TempFilenameWithText(text string) (string, error) {
	tmpFile, err := TempFileWithText(text)
	if err != nil {
		return "", err
	}

	filename := tmpFile.Name()
	if err = tmpFile.Close(); err != nil {
		return "", err
	}

	return filename, nil
}
