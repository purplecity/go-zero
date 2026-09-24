// ————————————————————————————————————————————————————————————————————————————
// range —— 数值钳制(clamp)泛型工具 —— 文件总结
//
// 三个钳制函数,约束 Numerical(全部整数+浮点类型):
//
//	AtLeast(x, lower)  下限钳制:max(x, lower);
//	AtMost(x, upper)   上限钳制:min(x, upper);
//	Between(x, l, u)   双向钳制:夹进 [lower, upper]。
//
// 用途:把用户输入/计算结果限制进合法区间(超时下限、重试
// 上限、并发数范围等)。~int 波浪号表示"底层类型为 int 的
// 所有命名类型"(如 time.Duration 底层 int64 也满足约束)。
// ————————————————————————————————————————————————————————————————————————————
package mathx

// Numerical is a constraint that permits any numeric type.
// 数值类型约束:全部有符号/无符号整数与浮点(~T 含底层类型
// 为 T 的命名类型,如 time.Duration)。
type Numerical interface {
	~int | ~int8 | ~int16 | ~int32 | ~int64 |
		~uint | ~uint8 | ~uint16 | ~uint32 | ~uint64 |
		~float32 | ~float64
}

// AtLeast returns the greater of x or lower.
// 下限钳制:x 小于 lower 就取 lower。
func AtLeast[T Numerical](x, lower T) T {
	if x < lower {
		return lower
	}
	return x
}

// AtMost returns the smaller of x or upper.
// 上限钳制:x 大于 upper 就取 upper。
func AtMost[T Numerical](x, upper T) T {
	if x > upper {
		return upper
	}
	return x
}

// Between returns the value of x clamped to the range [lower, upper].
// 双向钳制:夹进 [lower, upper] 闭区间。
func Between[T Numerical](x, lower, upper T) T {
	if x < lower {
		return lower
	}
	if x > upper {
		return upper
	}
	return x
}
