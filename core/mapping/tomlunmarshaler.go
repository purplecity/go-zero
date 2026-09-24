// ————————————————————————————————————————————————————————————————————————————
// tomlunmarshaler —— TOML 反序列化入口 —— 文件总结
//
// 策略与 YAML 相同:TOML → JSON 字节 → 走 UnmarshalJsonBytes,
// 公共填充与校验逻辑只维护一份。
// ————————————————————————————————————————————————————————————————————————————
package mapping

import (
	"io"

	"github.com/zeromicro/go-zero/internal/encoding"
)

// UnmarshalTomlBytes unmarshals TOML bytes into the given v.
// TOML 字节 → 转 JSON → 复用 JSON 填充管线。
func UnmarshalTomlBytes(content []byte, v any, opts ...UnmarshalOption) error {
	b, err := encoding.TomlToJson(content)
	if err != nil {
		return err
	}

	return UnmarshalJsonBytes(b, v, opts...)
}

// UnmarshalTomlReader unmarshals TOML from the given io.Reader into the given v.
// reader 版:读全量后走字节版。
func UnmarshalTomlReader(r io.Reader, v any, opts ...UnmarshalOption) error {
	b, err := io.ReadAll(r)
	if err != nil {
		return err
	}

	return UnmarshalTomlBytes(b, v, opts...)
}
