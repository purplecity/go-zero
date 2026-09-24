// ————————————————————————————————————————————————————————————————————————————
// int —— 整数最值的历史封装 —— 文件总结
//
// Go 1.21 起内置了 min/max,这两个函数已无必要,仅为兼容保留。
// ————————————————————————————————————————————————————————————————————————————
package mathx

// MaxInt returns the larger one of a and b.
// Deprecated: use builtin max instead.
// 两数取大(已废弃:直接用内置 max)。
func MaxInt(a, b int) int {
	return max(a, b)
}

// MinInt returns the smaller one of a and b.
// Deprecated: use builtin min instead.
// 两数取小(已废弃:直接用内置 min)。
func MinInt(a, b int) int {
	return min(a, b)
}
