// ————————————————————————————————————————————————————————————————————————————
// wrap —— 错误包装(附加上下文信息) —— 文件总结
//
// 在不丢失原始错误的前提下附加说明文字,如
//
//	errorx.Wrap(err, "query user failed")
//	→ "query user failed: <原始错误>"
//
// 用 fmt.Errorf 的 %w 包装,错误链完整保留:
//
//	errors.Is / errors.As 依然能穿透包装匹配到原始错误。
//
// err 为 nil 时原样返回 nil(可在错误传播链上无脑调用)。
// 标准库 go 1.13+ 有 fmt.Errorf + %w 与 errors.Join,本包是
// go-zero 早期封装的保留。
// ————————————————————————————————————————————————————————————————————————————
package errorx

import "fmt"

// Wrap returns an error that wraps err with given message.
// 用固定文字包装 err;err 为 nil 时返回 nil。
func Wrap(err error, message string) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", message, err)
}

// Wrapf returns an error that wraps err with given format and args.
// 同 Wrap,说明文字支持格式化参数。
func Wrapf(err error, format string, args ...any) error {
	if err == nil {
		return nil
	}

	return fmt.Errorf("%s: %w", fmt.Sprintf(format, args...), err)
}
