// ————————————————————————————————————————————————————————————————————————————
// jsonunmarshaler —— JSON 反序列化入口(基于 Unmarshaler) —— 文件总结
//
// 三个入口(Bytes/Reader/Map)最终都走通用 Unmarshaler
// (tag key 为 "json"):先用 jsonx 解析成 map[string]any
// (UseNumber 防精度丢失),再按 struct 的 json tag 填充。
// 好处:字段校验(optional/default/range/options)对 JSON
// 一样生效 —— 这是 go-zero 配置体系(yaml/toml 转成 JSON
// 再进来)的公共底座。
// jsonUnmarshaler 是无选项时的全局复用实例。
// ————————————————————————————————————————————————————————————————————————————
package mapping

import (
	"io"

	"github.com/zeromicro/go-zero/core/jsonx"
)

// jsonTagKey 本文件的 tag 键。
const jsonTagKey = "json"

// jsonUnmarshaler 全局默认实例(无自定义选项时复用)。
var jsonUnmarshaler = NewUnmarshaler(jsonTagKey)

// UnmarshalJsonBytes unmarshals content into v.
// JSON 字节 → map → 按字段填充 v。
func UnmarshalJsonBytes(content []byte, v any, opts ...UnmarshalOption) error {
	return unmarshalJsonBytes(content, v, getJsonUnmarshaler(opts...))
}

// UnmarshalJsonMap unmarshals content from m into v.
// 已是 map 的数据直接填充(省一次解析)。
func UnmarshalJsonMap(m map[string]any, v any, opts ...UnmarshalOption) error {
	return getJsonUnmarshaler(opts...).Unmarshal(m, v)
}

// UnmarshalJsonReader unmarshals content from reader into v.
// 从 reader 读 JSON → map → 填充 v。
func UnmarshalJsonReader(reader io.Reader, v any, opts ...UnmarshalOption) error {
	return unmarshalJsonReader(reader, v, getJsonUnmarshaler(opts...))
}

// getJsonUnmarshaler 取实例:有选项新建,无选项复用全局。
func getJsonUnmarshaler(opts ...UnmarshalOption) *Unmarshaler {
	if len(opts) > 0 {
		return NewUnmarshaler(jsonTagKey, opts...)
	}

	return jsonUnmarshaler
}

// unmarshalJsonBytes 字节版:jsonx 解析(UseNumber)后交给 Unmarshaler。
func unmarshalJsonBytes(content []byte, v any, unmarshaler *Unmarshaler) error {
	var m any
	if err := jsonx.Unmarshal(content, &m); err != nil {
		return err
	}

	return unmarshaler.Unmarshal(m, v)
}

// unmarshalJsonReader reader 版:读全量后走字节版。
func unmarshalJsonReader(reader io.Reader, v any, unmarshaler *Unmarshaler) error {
	var m any
	if err := jsonx.UnmarshalFromReader(reader, &m); err != nil {
		return err
	}

	return unmarshaler.Unmarshal(m, v)
}
