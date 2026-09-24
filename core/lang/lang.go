// ————————————————————————————————————————————————————————————————————————————
// lang —— 语言级小工具(占位类型 + 通用字符串化) —— 文件总结
//
// Placeholder:零大小 struct 占位对象 —— 当语法要求"必须传个值"
// 但值本身无意义时用(常见:向 channel 发信号 <-ch <- lang.Placeholder,
// 或 map[string]lang.Placeholder 当集合)。零内存、语义清晰。
//
// Repr:把任意值转成字符串表示 —— 比 fmt.Sprint 更懂业务类型:
// 优先用 String()/Error(),解引用指针后再判断基础类型,
// 数值/字符串/[]byte 都有专用快路径,避免 reflect 慢路径。
// ————————————————————————————————————————————————————————————————————————————
package lang

import (
	"fmt"
	"reflect"
	"strconv"
)

// Placeholder is a placeholder object that can be used globally.
// 全局占位对象:零大小,用在"只需要一个值、不关心内容"的场景
// (如 channel 信号、集合 map 的值类型)。
var Placeholder PlaceholderType

type (
	// AnyType can be used to hold any type.
	// any 的别名(历史写法,等价于 any)。
	AnyType = any
	// PlaceholderType represents a placeholder type.
	// 占位类型:空 struct,不占内存。
	PlaceholderType = struct{}
)

// Repr returns the string representation of v.
// 任意值的字符串表示:依次尝试 Stringer → 解引用指针 → 基础类型
// 专用格式化(bool/error/浮点/整数/字符串/[]byte),
// 都不匹配才走 fmt.Sprint 兜底;nil 返回空串。
func Repr(v any) string {
	if v == nil {
		return ""
	}

	// if func (v *Type) String() string, we can't use Elem()
	// 先按接口断言 Stringer:指针接收者的 String 解引用后就丢了,
	// 所以必须在解引用前判断。
	switch vt := v.(type) {
	case fmt.Stringer:
		return vt.String()
	}

	val := reflect.ValueOf(v)
	// 层层解引用到非指针值(跳过 nil 指针)再取表示。
	for val.Kind() == reflect.Ptr && !val.IsNil() {
		val = val.Elem()
	}

	return reprOfValue(val)
}

// reprOfValue 基础类型的专用格式化:每个类型走 strconv 快路径
// (比 fmt.Sprint 的反射路径快),兜底才用 fmt.Sprint。
func reprOfValue(val reflect.Value) string {
	switch vt := val.Interface().(type) {
	case bool:
		return strconv.FormatBool(vt)
	case error:
		return vt.Error()
	case float32:
		return strconv.FormatFloat(float64(vt), 'f', -1, 32)
	case float64:
		return strconv.FormatFloat(vt, 'f', -1, 64)
	case fmt.Stringer:
		return vt.String()
	case int:
		return strconv.Itoa(vt)
	case int8:
		return strconv.Itoa(int(vt))
	case int16:
		return strconv.Itoa(int(vt))
	case int32:
		return strconv.Itoa(int(vt))
	case int64:
		return strconv.FormatInt(vt, 10)
	case string:
		return vt
	case uint:
		return strconv.FormatUint(uint64(vt), 10)
	case uint8:
		return strconv.FormatUint(uint64(vt), 10)
	case uint16:
		return strconv.FormatUint(uint64(vt), 10)
	case uint32:
		return strconv.FormatUint(uint64(vt), 10)
	case uint64:
		return strconv.FormatUint(vt, 10)
	case []byte:
		return string(vt)
	default:
		return fmt.Sprint(val.Interface())
	}
}
