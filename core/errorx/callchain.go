// ————————————————————————————————————————————————————————————————————————————
// callchain —— 顺序调用链(短路式) —— 文件总结
//
// 依次执行 fns,任何一个返回错误立即停止并返回该错误
// (类似 && 短路);全部成功返回 nil。
// 典型用途:把一串必须按序执行的初始化/校验步骤串起来。
// 与 mr.Finish 相反:Finish 是并行且聚合错误,Chain 是串行且短路。
// ————————————————————————————————————————————————————————————————————————————
package errorx

// Chain runs funs one by one until an error occurred.
// 依次执行,任一失败即短路返回该错误;全部成功返回 nil。
func Chain(fns ...func() error) error {
	for _, fn := range fns {
		if err := fn(); err != nil {
			return err
		}
	}

	return nil
}
