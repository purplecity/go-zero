// ————————————————————————————————————————————————————————————————————————————
// temps —— 内容确定的临时文件工具(主要供测试用) —— 文件总结
//
// 两个函数都是"把给定文本写进临时文件":
//
//	TempFileWithText   返回打开的 *os.File(用完 Close,文件按名删);
//	TempFilenameWithText 返回文件路径(内部已 Close,用完按名删)。
//
// 文件名 = 内容 MD5 + CreateTemp 的随机后缀:MD5 提供 32 位
// 十六进制的合法/定长/可辨认前缀(同内容同前缀,便于排查清理),
// 随机后缀才是唯一性保证 —— 重复创建是两个文件,并非去重。
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
//
// 文件名的构成 —— os.CreateTemp 第二参是"模式"而非最终名:
//
//	<md5(text),32 位十六进制前缀> + <CreateTemp 追加的随机串>
//
//	MD5 前缀:把任意 text 映射成合法(仅 [0-9a-f])、定长(32,
//	不超文件系统文件名上限)、可辨认(同内容同前缀,便于排查
//	和按前缀清理)的名字;
//	随机后缀:唯一性的真正保证 —— 模式里没有 '*' 时自动追加,
//	配合 O_EXCL 创建语义,同内容重复创建也是两个独立文件,
//	不覆盖、非去重;也因此 MD5 碰撞在此无影响,它不作安全机制。
func TempFileWithText(text string) (*os.File, error) {
	// CreateTemp:临时目录创建并打开(O_EXCL,绝不复用已有文件)。
	tmpFile, err := os.CreateTemp(os.TempDir(), hash.Md5Hex([]byte(text)))
	if err != nil {
		return nil, err
	}

	// 按文件名写入内容(WriteFile 独立开句柄,与返回值句柄无关)。
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
