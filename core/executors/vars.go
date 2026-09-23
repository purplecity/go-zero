// ————————————————————————————————————————————————————————————————————————————
// executors/vars —— 执行器公共定义 —— 文件总结
//
// 包内所有执行器(Bulk/Chunk/Periodical)共享的常量与类型:
//
//	Execute          任务批的实际处理回调(拿到一批任务做业务,
//	                 如批量写库、批量发消息);
//	defaultFlushInterval 默认刷盘周期 1 秒 —— 任务攒着不足阈值
//	                 时,最多 1 秒也会强制刷出一次。
//
// ————————————————————————————————————————————————————————————————————————————
package executors

import "time"

// defaultFlushInterval 默认刷盘间隔。
const defaultFlushInterval = time.Second

// Execute defines the method to execute tasks.
// 任务批处理回调:tasks 是攒下的一批任务。
type Execute func(tasks []any)
