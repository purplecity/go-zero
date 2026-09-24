// ————————————————————————————————————————————————————————————————————————————
// jsonx —— JSON 序列化/反序列化的行为修正版 —— 文件总结
//
// Marshal 与标准库 json.Marshal 的两点差异(刻意为之):
//  1. 不转义 HTML 字符(& < > 保持原样,而非 \u0026 等)
//     —— API 响应里 URL/HTML 片段不该被改写;
//  2. Unmarshal 系列启用 UseNumber —— 数字默认解码为 json.Number
//     (字符串形式),而非 float64,避免大整数/高精度小数丢精度
//     (如 int64 溢出、0.1 变 0.10000000000000001)。
//
// 错误信息带原始输入(unmarshal 失败时能直接看到坏数据)。
// ————————————————————————————————————————————————————————————————————————————
package jsonx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"strings"
)

// Marshal marshals v into json bytes, without escaping HTML and removes the trailing newline.
// 序列化为 JSON 字节:关掉 HTML 转义(& < > 原样输出),并去掉
// Encoder.Encode 自动附加的尾部换行。
func Marshal(v any) ([]byte, error) {
	// why not use json.Marshal?  https://github.com/golang/go/issues/28453
	// it changes the behavior of json.Marshal, like & -> \u0026, < -> \u003c, > -> \u003e
	// which is not what we want in API responses
	// 为什么不用 json.Marshal:标准库默认把 & < > 转义成 \u0026 等,
	// API 响应里的 URL/HTML 会被改写,这里用 Encoder 关掉该行为。
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return nil, err
	}

	bs := buf.Bytes()
	// Remove trailing newline added by json.Encoder.Encode
	// Encoder.Encode 会附加换行,去掉以保持与 json.Marshal 一致。
	if len(bs) > 0 && bs[len(bs)-1] == '\n' {
		bs = bs[:len(bs)-1]
	}

	return bs, nil
}

// MarshalToString marshals v into a string.
// 序列化为 JSON 字符串。
func MarshalToString(v any) (string, error) {
	data, err := Marshal(v)
	if err != nil {
		return "", err
	}

	return string(data), nil
}

// Unmarshal unmarshals data bytes into v.
// 反序列化字节到 v(UseNumber 防精度丢失;失败时错误带原始输入)。
func Unmarshal(data []byte, v any) error {
	decoder := json.NewDecoder(bytes.NewReader(data))
	if err := unmarshalUseNumber(decoder, v); err != nil {
		return formatError(string(data), err)
	}

	return nil
}

// UnmarshalFromString unmarshals v from str.
// 反序列化字符串到 v(语义同 Unmarshal)。
func UnmarshalFromString(str string, v any) error {
	decoder := json.NewDecoder(strings.NewReader(str))
	if err := unmarshalUseNumber(decoder, v); err != nil {
		return formatError(str, err)
	}

	return nil
}

// UnmarshalFromReader unmarshals v from reader.
// 从 reader 反序列化:TeeReader 边读边把原文拷进 buf,
// 失败时错误里能带上完整输入(流只能读一次,不提前读就拿不到)。
func UnmarshalFromReader(reader io.Reader, v any) error {
	var buf strings.Builder
	teeReader := io.TeeReader(reader, &buf)
	decoder := json.NewDecoder(teeReader)
	if err := unmarshalUseNumber(decoder, v); err != nil {
		return formatError(buf.String(), err)
	}

	return nil
}

// unmarshalUseNumber 用 UseNumber 模式解码:数字解析成
// json.Number(保留原文),而不是 float64 —— 大整数与高精度
// 小数不丢精度。
func unmarshalUseNumber(decoder *json.Decoder, v any) error {
	decoder.UseNumber()
	return decoder.Decode(v)
}

// formatError 把原始输入拼进错误信息,方便定位坏数据。
func formatError(v string, err error) error {
	return fmt.Errorf("string: `%s`, error: `%w`", v, err)
}
