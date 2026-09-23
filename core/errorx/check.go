// ————————————————————————————————————————————————————————————————————————————
// check —— 错误归属判断 —— 文件总结
//
// In(err, errs...):判断 err 是否属于给定的一组错误。
// 底层是 errors.Is —— 支持错误链( Wrapped error 也能匹配到
// 被包裹的目标错误),比 == 比较更可靠。
// ————————————————————————————————————————————————————————————————————————————
package errorx

import "errors"

// In checks if the given err is one of errs.
// 判断 err 是否等于(或包裹着)errs 中的某一个。
// 例:errx.In(err, sql.ErrNoRows, context.DeadlineExceeded)
func In(err error, errs ...error) bool {
	for _, each := range errs {
		if errors.Is(err, each) {
			return true
		}
	}

	return false
}
