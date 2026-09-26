// ————————————————————————————————————————————————————————————————————————————
// nopbreaker —— 空断路器:永不熔断 —— 文件总结
//
// 全部方法直通:Allow 恒发通行证(nopPromise 什么都不记),
// Do 系恒执行请求。两个用途:测试中替换真断路器;
// NoBreakerFor(name) 往注册表塞一个,等价于对该名字关闭熔断。
// ————————————————————————————————————————————————————————————
package breaker

import "context"

const nopBreakerName = "nopBreaker"

type nopBreaker struct{}

// NopBreaker returns a breaker that never trigger breaker circuit.
// 创建空断路器(无状态,零开销)。
func NopBreaker() Breaker {
	return nopBreaker{}
}

func (b nopBreaker) Name() string {
	return nopBreakerName
}

func (b nopBreaker) Allow() (Promise, error) {
	return nopPromise{}, nil
}

func (b nopBreaker) AllowCtx(_ context.Context) (Promise, error) {
	return nopPromise{}, nil
}

func (b nopBreaker) Do(req func() error) error {
	return req()
}

func (b nopBreaker) DoCtx(_ context.Context, req func() error) error {
	return req()
}

func (b nopBreaker) DoWithAcceptable(req func() error, _ Acceptable) error {
	return req()
}

func (b nopBreaker) DoWithAcceptableCtx(_ context.Context, req func() error, _ Acceptable) error {
	return req()
}

func (b nopBreaker) DoWithFallback(req func() error, _ Fallback) error {
	return req()
}

func (b nopBreaker) DoWithFallbackCtx(_ context.Context, req func() error, _ Fallback) error {
	return req()
}

func (b nopBreaker) DoWithFallbackAcceptable(req func() error, _ Fallback, _ Acceptable) error {
	return req()
}

func (b nopBreaker) DoWithFallbackAcceptableCtx(_ context.Context, req func() error,
	_ Fallback, _ Acceptable) error {
	return req()
}

// nopPromise 空通行证:回报成功/失败都是空操作(不记账)。
type nopPromise struct{}

func (p nopPromise) Accept() {
}

func (p nopPromise) Reject(_ string) {
}
