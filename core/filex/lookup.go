// ————————————————————————————————————————————————————————————————————————————
// lookup —— 按行边界切分文件区间(供并行处理大文件) —— 文件总结
//
// SplitLineChunks 把文件切成 chunks 个 [Start,Stop) 字节区间,
// 保证完整行不会跨块:先按"文件大小/chunks+1"估出每块的
// 理想终点,再把终点向后推到下一个换行(skipPartialLine),
// 使每块都以整行结尾。+1 是让最后一块不至于太小。
// 若理想切点落在最后一行内部(其后再无换行),末行整行并入
// 当前块(不切断,见循环内对 io.EOF 的特判)。
// 与 rangereader.go 配套:切出的区间交给 RangeReader 读取,
// 即可实现多 goroutine 各扫一块大文件,块内都是完整行。
// ————————————————————————————————————————————————————————————————————————————
package filex

import (
	"errors"
	"io"
	"os"
)

// OffsetRange represents a content block of a file.
// 文件的一个字节区间:[Start, Stop),以整行边界切分。
type OffsetRange struct {
	// File 文件路径。
	File string
	// Start 区间起始偏移(含)。
	Start int64
	// Stop 区间结束偏移(不含)。
	Stop int64
}

// SplitLineChunks splits file into chunks.
// The whole line are guaranteed to be split in the same chunk.
// 把文件切成 chunks 个区间,保证整行不被切断。
func SplitLineChunks(filename string, chunks int) ([]OffsetRange, error) {
	info, err := os.Stat(filename)
	if err != nil {
		return nil, err
	}

	// 只切一块:整个文件一个区间,无需打开文件。
	if chunks <= 1 {
		return []OffsetRange{
			{
				File:  filename,
				Start: 0,
				Stop:  info.Size(),
			},
		}, nil
	}

	file, err := os.Open(filename)
	if err != nil {
		return nil, err
	}
	defer file.Close()

	var ranges []OffsetRange
	var offset int64
	// avoid the last chunk too few bytes
	// 每块理想大小 = 总大小/chunks + 1:多切一点,
	// 避免最后一块只剩几个字节。
	preferSize := info.Size()/int64(chunks) + 1
	for {
		// 剩余量不足一块:全部归最后一块。
		if offset+preferSize >= info.Size() {
			ranges = append(ranges, OffsetRange{
				File:  filename,
				Start: offset,
				Stop:  info.Size(),
			})
			break
		}

		// 理想终点 [offset, offset+preferSize) 可能切断一行,
		// 把终点推到下一行首,保证整行归前一块。
		offsetRange, err := nextRange(file, offset, offset+preferSize)
		if errors.Is(err, io.EOF) {
			// 切点落在最后一行内部(其后直到文件尾再无换行),
			// skipPartialLine 找不到"下一行的开头"而返回 EOF。
			// 按"整行不切断"的契约,把最后一行整行并入本块,
			// Stop 取文件末尾;下方 Stop < Size 不成立,自然退出。
			offsetRange = OffsetRange{
				File:  filename,
				Start: offset,
				Stop:  info.Size(),
			}
		} else if err != nil {
			return nil, err
		}

		ranges = append(ranges, offsetRange)
		if offsetRange.Stop < info.Size() {
			offset = offsetRange.Stop
		} else {
			break
		}
	}

	return ranges, nil
}

// nextRange 取 [start, stop) 区间并把 stop 推到整行边界。
func nextRange(file *os.File, start, stop int64) (OffsetRange, error) {
	offset, err := skipPartialLine(file, stop)
	if err != nil {
		return OffsetRange{}, err
	}

	return OffsetRange{
		File:  file.Name(),
		Start: start,
		Stop:  offset,
	}, nil
}

// skipPartialLine 从 offset 起向后扫,跳过当前不完整的行
// (连同其后的连续换行),返回下一行首的偏移:
// 遇到非换行字符 → 还在残行里,继续;
// 遇到换行 → 跳过连续的 \r\n,碰到非换行即到达新行首。
// 契约:扫到 EOF 仍无换行时返回 io.EOF —— 语义是切点位于
// 最后一行内部(其后不存在"下一行"),由调用方决定收尾
// (见 SplitLineChunks 循环内的 EOF 特判)。
func skipPartialLine(file *os.File, offset int64) (int64, error) {
	for {
		skipBuf := make([]byte, bufSize)
		n, err := file.ReadAt(skipBuf, offset)
		if err != nil && err != io.EOF {
			return 0, err
		}
		if n == 0 {
			return 0, io.EOF
		}

		for i := 0; i < n; i++ {
			if skipBuf[i] != '\r' && skipBuf[i] != '\n' {
				offset++
			} else {
				for ; i < n; i++ {
					if skipBuf[i] == '\r' || skipBuf[i] == '\n' {
						offset++
					} else {
						return offset, nil
					}
				}
				return offset, nil
			}
		}
	}
}
