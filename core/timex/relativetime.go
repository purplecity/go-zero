// ————————————————————————————————————————————————————————————————————————————
// relativetime —— 相对时间测量 —— 文件总结
//
// 用 time.Duration(而不是 time.Time)表达"当前时刻",解决两个问题:
//  1. 调用方只关心相对差值:判断"距上次是否超过 X"、"已经过了多久"
//     这类逻辑,用 Duration 相减即可,无需携带时区/墙钟语义;
//  2. 精度与零值问题:initTime 取一年前,保证 Now() 返回值远大于 0,
//     两次 Now() 之差不会因精度截断而等于 0(注释原意),同时
//     Duration 的运算比 time.Time 更轻量。
//
// go-zero 内部大量使用它做耗时统计、熔断/自适应限流的窗口计时等
// (如 utils.ElapsedTimer、load、breaker 都基于 timex.Now)。
// 注意:底层仍是 time.Since(单调时钟),不受系统改时间影响。
// ————————————————————————————————————————————————————————————————————————————
package timex

import "time"

// Use the long enough past time as start time, in case timex.Now() - lastTime equals 0.
// 时间原点:一年前。足够久远,保证 Now() 永远是正的大数,
// 两次采样之差不会归零。
var initTime = time.Now().AddDate(-1, -1, -1)

// Now returns a relative time duration since initTime, which is not important.
// The caller only needs to care about the relative value.
// 返回"当前时刻"(相对 initTime 的时长)。绝对值无意义,
// 只用于和其他 timex.Now 的结果做差/比较。
func Now() time.Duration {
	return time.Since(initTime)
}

// Since returns a diff since given d.
// 计算从 d(必须是先前某次 timex.Now() 的返回值)到现在经过了多久。
func Since(d time.Duration) time.Duration {
	return time.Since(initTime) - d
}
