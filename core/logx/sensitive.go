// ————————————————————————————————————————————————————————————————————————————
// sensitive —— 日志脱敏接口 —— 文件总结
//
// 业务的值类型(如含密码、手机号的结构体)实现 Sensitive 接口后,
// 通过 Debugv/Errorv/Infov/Slowv 记录,或作为 LogField 的 Value 传入时,
// 输出前会自动替换为 MaskSensitive() 返回的脱敏副本。
// 实际生效点在 writer.go 的 output:对整值做一次断言、对每个字段值
// 做 maskSensitive 处理(先脱敏再查 Stringer,防止脱敏前就把明文
// 打进日志)。
// ————————————————————————————————————————————————————————————————————————————
package logx

// Sensitive is an interface that defines a method for masking sensitive information in logs.
// It is typically implemented by types that contain sensitive data,
// such as passwords or personal information.
// Infov, Errorv, Debugv, and Slowv methods will call this method to mask sensitive data.
// The values in LogField will also be masked if they implement the Sensitive interface.
// Sensitive 由包含敏感数据的类型实现(密码、手机号、身份证等),
// 日志输出时会自动调用 MaskSensitive 拿到脱敏后的替身值。
type Sensitive interface {
	// MaskSensitive masks sensitive information in the log.
	// 返回脱敏后的值,例如把 "13812345678" 变成 "138****5678"。
	MaskSensitive() any
}

// maskSensitive returns the value returned by MaskSensitive method,
// if the value implements Sensitive interface.
// 若 v 实现了 Sensitive 则返回脱敏值,否则原样返回。
// 所有日志字段输出前的统一脱敏入口。
func maskSensitive(v any) any {
	if s, ok := v.(Sensitive); ok {
		return s.MaskSensitive()
	}

	return v
}
