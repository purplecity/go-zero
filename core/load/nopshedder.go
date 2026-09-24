// ————————————————————————————————————————————————————————————————————————————
// nopshedder —— 负载脱落的空实现(空对象模式) —— 文件总结
//
// Disable() 后 NewAdaptiveShedder 返回本实现:
// Allow 恒放行,Pass/Fail 空操作 —— 调用方代码完全无感、
// 零开销,不需要 if 判断"脱落是否开启"。
// ————————————————————————————————————————————————————————————————————————————
package load

// nopShedder 空脱落器:永远放行。
type nopShedder struct{}

// newNopShedder 构造空实现。
func newNopShedder() Shedder {
	return nopShedder{}
}

// Allow 恒放行,返回空允诺(永不返回过载错误)。
func (s nopShedder) Allow() (Promise, error) {
	return nopPromise{}, nil
}

// nopPromise 空允诺:回报方法都是空操作。
type nopPromise struct{}

// Pass 空操作。
func (p nopPromise) Pass() {
}

// Fail 空操作。
func (p nopPromise) Fail() {
}
