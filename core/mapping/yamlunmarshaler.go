// ————————————————————————————————————————————————————————————————————————————
// yamlunmarshaler —— YAML 反序列化入口 —— 文件总结
//
// 策略:YAML → JSON 字节 → 走 UnmarshalJsonBytes。
// 统一转成 JSON 这条公共路径,配置校验(optional/default/
// range)对 YAML 全部生效,不用维护两套填充逻辑。
// ————————————————————————————————————————————————————————————————————————————
package mapping

import (
	"io"

	"github.com/zeromicro/go-zero/internal/encoding"
)

// UnmarshalYamlBytes unmarshals content into v.
// YAML 字节 → 转 JSON → 复用 JSON 填充管线。
func UnmarshalYamlBytes(content []byte, v any, opts ...UnmarshalOption) error {
	b, err := encoding.YamlToJson(content)
	if err != nil {
		return err
	}

	return UnmarshalJsonBytes(b, v, opts...)
}

// UnmarshalYamlReader unmarshals content from reader into v.
// reader 版:读全量后走字节版。
func UnmarshalYamlReader(reader io.Reader, v any, opts ...UnmarshalOption) error {
	b, err := io.ReadAll(reader)
	if err != nil {
		return err
	}

	return UnmarshalYamlBytes(b, v, opts...)
}
