// ————————————————————————————————————————————————————————————————————————————
// marshaler —— struct → 按 tag 分组导出(反向填充) —— 文件总结
//
// Marshal 把 struct 的每个字段按其 tag 键分组导出:
//
//	map[tag键][字段名] = 字段值
//
// 典型用途:API 请求 struct 同时声明 json/path/header 多套
// tag,Marshal 一次把三组键值都导出来(参数校验、日志、
// 签名等场景)。 匿名字段递归展开,结果并入外层分组。
// Marshal 与 Unmarshaler 是一对:一个 struct → map(按 tag
// 分组),一个 map → struct(按 tag 填充)。
//
// 导出前做 tag 声明的校验(与反序列化同规则):
// 非 optional 字段必须非零;options 枚举;range 区间。
// 注意:optional=another 依赖未实现(少用且难实现)。
// ————————————————————————————————————————————————————————————————————————————
package mapping

import (
	"fmt"
	"reflect"
	"slices"
	"strings"
)

const (
	// emptyTag 无 tag 字段归入的分组键(空串)。
	emptyTag = ""
	// tagKVSeparator tag 里键与选项的分隔符 ":"。
	tagKVSeparator = ":"
)

// Marshal marshals the given val and returns the map that contains the fields.
// optional=another is not implemented, and it's hard to implement and not commonly used.
// support anonymous field, e.g.:
//
//	type Foo struct {
//		Token string `header:"token"`
//	}
//	type FooB struct {
//		Foo
//		Bar string  `json:"bar"`
//	}
//
// 导出 struct 字段:结果按 tag 键分组,匿名字段递归展开。
func Marshal(val any) (map[string]map[string]any, error) {
	ret := make(map[string]map[string]any)
	// 类型与值都解一层指针。
	tp := reflect.TypeOf(val)
	if tp.Kind() == reflect.Ptr {
		tp = tp.Elem()
	}
	rv := reflect.ValueOf(val)
	if rv.Kind() == reflect.Ptr {
		rv = rv.Elem()
	}

	for i := 0; i < tp.NumField(); i++ {
		field := tp.Field(i)
		value := rv.Field(i)
		if err := processMember(field, value, ret); err != nil {
			return nil, err
		}
	}

	return ret, nil
}

// getTag 从 struct tag 原文里取键名:取 ":" 前的部分
// (有 ":" 说明带了选项);无分隔符则整串是键。
func getTag(field reflect.StructField) (string, bool) {
	tag := string(field.Tag)
	if i := strings.Index(tag, tagKVSeparator); i >= 0 {
		return strings.TrimSpace(tag[:i]), true
	}

	return strings.TrimSpace(tag), false
}

// insertValue 收集一个值:collector[tag分组][key] = val。
func insertValue(collector map[string]map[string]any, tag string, key string, val any) {
	if m, ok := collector[tag]; ok {
		m[key] = val
	} else {
		collector[tag] = map[string]any{
			key: val,
		}
	}
}

// processMember 处理单个字段:
// 解 tag 键与选项 → 校验 → 取值(FromString 则转字符串)
// → 匿名字段递归 Marshal 并入 / 普通字段直接收集。
func processMember(field reflect.StructField, value reflect.Value,
	collector map[string]map[string]any) error {
	var key string
	var opt *fieldOptions
	var err error
	tag, ok := getTag(field)
	if !ok {
		// 无 tag:归入空分组,字段名作键。
		tag = emptyTag
		key = field.Name
	} else {
		key, opt, err = parseKeyAndOptions(tag, field)
		if err != nil {
			return err
		}

		// 带 tag 的字段按选项校验。
		if err = validate(field, value, opt); err != nil {
			return err
		}
	}

	val := value.Interface()
	// FromString:值统一转字符串形式。
	if opt != nil && opt.FromString {
		val = fmt.Sprint(val)
	}

	if field.Anonymous {
		// 匿名字段:递归导出内层,结果并入本层分组。
		anonCollector, err := Marshal(val)
		if err != nil {
			return err
		}

		for anonTag, anonMap := range anonCollector {
			for anonKey, anonVal := range anonMap {
				insertValue(collector, anonTag, anonKey, anonVal)
			}
		}
	} else {
		insertValue(collector, tag, key, val)
	}

	return nil
}

// validate 按 tag 选项校验字段值:
// 非 optional → 必须非零;optional 且为零 → 通过;
// 再查 options 枚举与 range 区间。
func validate(field reflect.StructField, value reflect.Value, opt *fieldOptions) error {
	if opt == nil || !opt.Optional {
		if err := validateOptional(field, value); err != nil {
			return err
		}
	}

	if opt == nil {
		return nil
	}

	// 可选且为零值:直接放行(不必再查枚举/区间)。
	if opt.Optional && value.IsZero() {
		return nil
	}

	if len(opt.Options) > 0 {
		if err := validateOptions(value, opt); err != nil {
			return err
		}
	}

	if opt.Range != nil {
		if err := validateRange(value, opt); err != nil {
			return err
		}
	}

	return nil
}

// validateOptional 必填检查:指针非 nil、slice/map 非空。
func validateOptional(field reflect.StructField, value reflect.Value) error {
	switch field.Type.Kind() {
	case reflect.Ptr:
		if value.IsNil() {
			return fmt.Errorf("field %q is nil", field.Name)
		}
	case reflect.Slice, reflect.Map:
		if value.IsNil() || value.Len() == 0 {
			return fmt.Errorf("field %q is empty", field.Name)
		}
	}

	return nil
}

// validateOptions 枚举校验:值的字符串形式必须在白名单里。
func validateOptions(value reflect.Value, opt *fieldOptions) error {
	val := fmt.Sprint(value.Interface())
	if !slices.Contains(opt.Options, val) {
		return fmt.Errorf("field %q not in options", val)
	}

	return nil
}

// validateRange 区间校验:数值转 float64 后按
// [left, right] / (…) 开闭组合判断。
func validateRange(value reflect.Value, opt *fieldOptions) error {
	var val float64
	switch v := value.Interface().(type) {
	case int:
		val = float64(v)
	case int8:
		val = float64(v)
	case int16:
		val = float64(v)
	case int32:
		val = float64(v)
	case int64:
		val = float64(v)
	case uint:
		val = float64(v)
	case uint8:
		val = float64(v)
	case uint16:
		val = float64(v)
	case uint32:
		val = float64(v)
	case uint64:
		val = float64(v)
	case float32:
		val = float64(v)
	case float64:
		val = v
	default:
		return fmt.Errorf("unknown support type for range %q", value.Type().String())
	}

	// validates [left, right], [left, right), (left, right], (left, right)
	// 越界或踩到开区间端点即失败。
	if val < opt.Range.left ||
		(!opt.Range.leftInclude && val == opt.Range.left) ||
		val > opt.Range.right ||
		(!opt.Range.rightInclude && val == opt.Range.right) {
		return fmt.Errorf("%v out of range", value.Interface())
	}

	return nil
}
