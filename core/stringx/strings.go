// ————————————————————————————————————————————————————————————————————————————
// strings —— 常用字符串工具集 —— 文件总结
//
// 一组与标准库互补的小工具,重点说明几个:
//
//	Filter           按谓词删除字符(strings.Map 返回 -1 即丢弃该 rune);
//	FirstN           取前 n 个 rune(按字符截断,不切坏中文),可附省略号;
//	Join             跳过空元素的分隔拼接(预分配内存,零多余拷贝);
//	Substr           按 rune 的 [start, stop) 子串,带位置校验错误;
//	TakeWithPriority 依序执行候选函数取第一个非空结果(惰性求值);
//	ToCamelCase      仅把首字母转小写("UserID" → "userID");
//	Reverse          按 rune 反转(不会把中文切成乱码);
//	Remove/Union     切片删除元素 / 并集(保持去重,顺序不保证)。
//
// 其中 Contains 与 TakeOne 已标记 Deprecated,
// 分别推荐用标准库 slices.Contains 与 cmp.Or 替代。
// ————————————————————————————————————————————————————————————————————————————
package stringx

import (
	"errors"
	"slices"
	"strings"
	"unicode"

	"github.com/zeromicro/go-zero/core/lang"
)

var (
	// ErrInvalidStartPosition is an error that indicates the start position is invalid.
	// Substr 的起点非法(负数或超出长度)。
	ErrInvalidStartPosition = errors.New("start position is invalid")
	// ErrInvalidStopPosition is an error that indicates the stop position is invalid.
	// Substr 的终点非法(负数、超出长度或小于起点)。
	ErrInvalidStopPosition = errors.New("stop position is invalid")
)

// Contains checks if str is in list.
// Deprecated: use slices.Contains instead.
// 已废弃:直接用标准库 slices.Contains(list, str)。
func Contains(list []string, str string) bool {
	return slices.Contains(list, str)
}

// Filter filters chars from s with given remove function.
// 按谓词删除字符:remove(r) 为 true 的字符被丢弃。
// strings.Map 返回 -1 表示删除该 rune,天然按字符处理,不切坏多字节字符。
func Filter(s string, remove func(r rune) bool) string {
	return strings.Map(func(r rune) rune {
		if remove(r) {
			return -1
		}
		return r
	}, s)
}

// FirstN returns first n runes from s.
// 返回前 n 个 rune;截断发生时把可变参数 ellipsis 追加在尾部。
// range s 的 j 是每个 rune 的字节下标,数到第 n 个字符时正好
// 在字符边界上截断(不会把中文切成半个字);n>=字符数时原样返回。
func FirstN(s string, n int, ellipsis ...string) string {
	if n < 0 {
		return ""
	}

	var i int

	for j := range s {
		if i == n {
			ret := s[:j]
			for _, each := range ellipsis {
				ret += each
			}
			return ret
		}
		i++
	}

	return s
}

// HasEmpty checks if there are empty strings in args.
// 任一参数为空串则返回 true,常用于参数校验。
func HasEmpty(args ...string) bool {
	for _, arg := range args {
		if len(arg) == 0 {
			return true
		}
	}

	return false
}

// Join joins any number of elements into a single string, separating them with given sep.
// Empty elements are ignored. However, if the argument list is empty or all its elements are empty,
// Join returns an empty string.
// 用 sep 拼接任意个元素,跳过空元素;全空/空列表返回空串。
// 先累计总长精确预分配,避免 append 扩容拷贝。
func Join(sep byte, elem ...string) string {
	var size int
	for _, e := range elem {
		size += len(e)
	}
	if size == 0 {
		return ""
	}

	buf := make([]byte, 0, size+len(elem)-1)
	for _, e := range elem {
		if len(e) == 0 {
			continue
		}

		// 已有内容才加分隔符,天然避免开头出现 sep。
		if len(buf) > 0 {
			buf = append(buf, sep)
		}
		buf = append(buf, e...)
	}

	return string(buf)
}

// NotEmpty checks if all strings are not empty in args.
// 全部参数非空返回 true(HasEmpty 的取反)。
func NotEmpty(args ...string) bool {
	return !HasEmpty(args...)
}

// Remove removes given strs from strings.
// 从切片中删除所有等于 strs 中任一元素的项;
// 拷贝一份原切片再原地压缩(n 作为新长度游标),不修改入参。
func Remove(strings []string, strs ...string) []string {
	out := append([]string(nil), strings...)

	for _, str := range strs {
		var n int
		for _, v := range out {
			if v != str {
				out[n] = v
				n++
			}
		}
		out = out[:n]
	}

	return out
}

// Reverse reverses s.
// 反转字符串:先转 []rune 再反转,按字符而非字节,
// 中文等多字节字符不会被切坏。
func Reverse(s string) string {
	runes := []rune(s)
	slices.Reverse(runes)
	return string(runes)
}

// Substr returns runes between start and stop [start, stop)
// regardless of the chars are ascii or utf8.
// 按 rune 取子串 [start, stop),ASCII 与中文一视同仁;
// 起点或终点非法时返回对应错误。
func Substr(str string, start, stop int) (string, error) {
	rs := []rune(str)
	length := len(rs)

	if start < 0 || start > length {
		return "", ErrInvalidStartPosition
	}

	if stop < 0 || stop > length || start > stop {
		return "", ErrInvalidStopPosition
	}

	return string(rs[start:stop]), nil
}

// TakeOne returns valid string if not empty or later one.
// Deprecated: use cmp.Or instead.
// 已废弃:直接用标准库 cmp.Or(valid, or)。
// 语义:valid 非空返回 valid,否则返回 or。
func TakeOne(valid, or string) string {
	if len(valid) > 0 {
		return valid
	}

	return or
}

// TakeWithPriority returns the first not empty result from fns.
// 依序调用候选函数,返回第一个非空结果,全空返回空串。
// 惰性求值:前面的函数有结果时,后面的函数不会执行。
// 常见用法:优先取用户配置,其次环境变量,最后默认值。
func TakeWithPriority(fns ...func() string) string {
	for _, fn := range fns {
		val := fn()
		if len(val) > 0 {
			return val
		}
	}

	return ""
}

// ToCamelCase returns the string that converts the first letter to lowercase.
// 把首字母转小写:"UserID" → "userID"(Go 风格驼峰)。
// range s 直接命中第一个 rune(含多字节),i 为其字节长度。
// 空串返回空串。
func ToCamelCase(s string) string {
	for i, v := range s {
		return string(unicode.ToLower(v)) + s[i+1:]
	}

	return ""
}

// Union merges the strings in first and second.
// 两个切片的并集(去重);map 遍历顺序随机,
// 结果元素顺序不保证,只关心成员本身时使用。
func Union(first, second []string) []string {
	set := make(map[string]lang.PlaceholderType)

	for _, each := range first {
		set[each] = lang.Placeholder
	}
	for _, each := range second {
		set[each] = lang.Placeholder
	}

	merged := make([]string, 0, len(set))
	for k := range set {
		merged = append(merged, k)
	}

	return merged
}
