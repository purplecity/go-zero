// ————————————————————————————————————————————————————————————————————————————
// ticker —— 可测试的定时器抽象 —— 文件总结
//
// 一、为什么包一层?
//
//	time.Ticker 是具体类型,单测里无法控制它的触发时机(只能真等)。
//	把它抽象成 Ticker 接口后,依赖定时触发的逻辑(如轮询、超时控制)
//	在测试中可注入 FakeTicker:用 Tick() 手动"拨表",无需 sleep,
//	测试既快又稳定。
//
// 二、两个实现
//
//	realTicker —— 生产用,直接包装标准库 time.Ticker;
//	fakeTicker —— 测试用,c 是带 1 缓冲的手动通道:
//	  Tick()  手动触发一次"到点"事件(非阻塞,缓冲 1 保证不卡);
//	  Stop()  关闭通道(与 time.Ticker.Stop 后管道不关闭不同,
//	          关闭让 range 消费方能自然退出);
//	  Done()  测试侧发出完成信号;
//	  Wait(d) 等待被测逻辑完成,超时(d)则返回 errTimeout,
//	          防止测试因逻辑 bug 而永久挂起。
//
// ————————————————————————————————————————————————————————————————————————————
package timex

import (
	"errors"
	"time"

	"github.com/zeromicro/go-zero/core/lang"
)

// errTimeout indicates a timeout.
// Wait 等待超时时返回的错误。
var errTimeout = errors.New("timeout")

type (
	// Ticker interface wraps the Chan and Stop methods.
	// Ticker 抽象:与 time.Ticker 等价的最小接口。
	Ticker interface {
		// Chan 返回到点事件的接收通道。
		Chan() <-chan time.Time
		// Stop 停止定时器。
		Stop()
	}

	// FakeTicker interface is used for unit testing.
	// FakeTicker 在 Ticker 基础上增加测试控制能力。
	FakeTicker interface {
		Ticker
		// Done 发出"处理完成"信号(解除 Wait 的阻塞)。
		Done()
		// Tick 手动触发一次到点事件。
		Tick()
		// Wait 等待完成信号,超过 d 返回 errTimeout。
		Wait(d time.Duration) error
	}

	// fakeTicker 测试实现:手动投递到点事件的通道 + 完成信号通道。
	fakeTicker struct {
		c    chan time.Time
		done chan lang.PlaceholderType
	}

	// realTicker 生产实现:包装标准库 time.Ticker。
	realTicker struct {
		*time.Ticker
	}
)

// NewTicker returns a Ticker.
// 创建真实定时器,每 d 触发一次。
func NewTicker(d time.Duration) Ticker {
	return &realTicker{
		Ticker: time.NewTicker(d),
	}
}

// Chan 返回标准库 ticker 的事件通道。
func (rt *realTicker) Chan() <-chan time.Time {
	return rt.C
}

// NewFakeTicker returns a FakeTicker.
// 创建手动控制的假定时器(仅测试用)。
func NewFakeTicker() FakeTicker {
	return &fakeTicker{
		// 缓冲 1:Tick() 投递后无需等待消费即可返回。
		c:    make(chan time.Time, 1),
		done: make(chan lang.PlaceholderType, 1),
	}
}

// Chan 返回手动事件通道。
func (ft *fakeTicker) Chan() <-chan time.Time {
	return ft.c
}

// Done 测试侧发出完成信号,解除被测逻辑中 Wait 的阻塞。
func (ft *fakeTicker) Done() {
	ft.done <- lang.Placeholder
}

// Stop 关闭事件通道,消费方 range 可自然退出。
func (ft *fakeTicker) Stop() {
	close(ft.c)
}

// Tick 手动向通道投递一次"到点"事件(缓冲 1,非阻塞)。
func (ft *fakeTicker) Tick() {
	ft.c <- time.Now()
}

// Wait 等待完成信号或超时:被测逻辑在 d 内完成返回 nil,
// 否则返回 errTimeout(防止测试永久挂起)。
func (ft *fakeTicker) Wait(d time.Duration) error {
	select {
	case <-time.After(d):
		return errTimeout
	case <-ft.done:
		return nil
	}
}
