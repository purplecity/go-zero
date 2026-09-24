// ————————————————————————————————————————————————————————————————————————————
// tee —— 限量 TeeReader —— 文件总结
//
// 标准 io.TeeReader 把读到的内容全部写给 w;limitTeeReader
// 只把前 n 字节写给 w,之后照常读但不再复制 —— 用于
// "边消费边留档,但留档设上限"(如日志、请求体头部采样),
// 防止超大流把备份端撑爆。
// ————————————————————————————————————————————————————————————————————————————
package iox

import "io"

// LimitTeeReader returns a Reader that writes up to n bytes to w what it reads from r.
// First n bytes reads from r performed through it are matched with
// corresponding writes to w. There is no internal buffering -
// the write must complete before the first n bytes read completes.
// Any error encountered while writing is reported as a read error.
// 限量版 TeeReader:读 r 的同时把前 n 字节写给 w,超出部分
// 只读不复制。
func LimitTeeReader(r io.Reader, w io.Writer, n int64) io.Reader {
	return &limitTeeReader{r, w, n}
}

// limitTeeReader 带"剩余可复制字节数"的 tee。
type limitTeeReader struct {
	r io.Reader
	w io.Writer
	n int64 // limit bytes remaining 剩余可写给 w 的字节数
}

// Read 读 r;若还有配额(n>0),把本次读到的内容(截到配额内)
// 写给 w;写失败按读错误上报。配额用完后纯透传。
func (t *limitTeeReader) Read(p []byte) (n int, err error) {
	n, err = t.r.Read(p)
	if n > 0 && t.n > 0 {
		// 本次最多复制 min(n, 剩余配额) 字节。
		limit := int64(n)
		if limit > t.n {
			limit = t.n
		}
		if n, err := t.w.Write(p[:limit]); err != nil {
			return n, err
		}

		t.n -= limit
	}

	return
}
