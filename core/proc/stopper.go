// ————————————————————————————————————————————————————————————————————————————
// stopper —— Stop 接口与空实现 —— 文件总结
//
// Stopper 是"可停止"的极简接口;nilStopper 是空实现,
// 用作占位返回值:StartProfile 在 Windows 平台、或重复启动时
// 返回 noopStopper —— 调用方无差别地调用 Stop(),不需要判空。
// 这是空对象模式(Null Object Pattern)的典型应用。
// ————————————————————————————————————————————————————————————————————————————
package proc

var noopStopper nilStopper

type (
	// Stopper interface wraps the method Stop.
	// 可停止接口。
	Stopper interface {
		Stop()
	}

	// nilStopper 空实现,Stop 什么都不做。
	nilStopper struct{}
)

// Stop 空实现。
func (ns nilStopper) Stop() {
}
