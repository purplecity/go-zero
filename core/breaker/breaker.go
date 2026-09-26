// ————————————————————————————————————————————————————————————————————————————
// breaker —— 断路器门面:用户 API + 日志装饰层 —— 文件总结
//
// 四层洋葱(装饰器模式,自外向内):
//
//	Breaker / circuitBreaker   用户 API:Do* / Allow*;
//	  └ throttle.loggedThrottle 装饰层:叠"最近 5 条错误原因"
//	                            环形日志 + 断路打开时 stat 上报;
//	      └ googleBreaker       算法核心(googlebreaker.go);
//	        └ RollingWindow    统计底盘(collection 包)。
//
// 三组扩展点:
//
//	Acceptable 哪些错误"算成功"(如业务 404 不计入故障);
//	Fallback   被拒后的降级(返回兜底值,不让错误上抛);
//	Promise    Allow 模式的通行证:自己管请求生命周期时,
//	           事后手动回报 Accept/Reject。
//
// Do 系列统一骨架:accept 判定 → 拒绝走 fallback / 返回
// ErrServiceUnavailable → 放行则执行请求、defer 记账
// (panic 也记失败再重抛)。Ctx 变体只是先查一遍 ctx.Done。
//
// 【go-zero 里的挂载点】不是独立部署的组件,而是嵌在每个
// 服务进程里的库,四处生效、方向有讲究:
//
//	zrpc 客户端拦截器  每个下游×方法一个(主战场 —— SRE 的
//	                  Client-Side:保护调用方不被下游拖垮;
//	                  坏哪个方法只熔那个方法);
//	rest 服务端中间件  每个路由一个(BreakerHandler:状态码
//	                  <500 算成功 —— 4xx 是客户端的错;被拒
//	                  返回 503);
//	zrpc 服务端拦截器  每个方法一个(自我保护:撑不住主动丢,
//	                  防硬扛到崩;超时计为故障);
//	rest 客户端 httpc  按目标服务。
//
// API 网关无需额外配置:它 = rest 服务端 + zrpc 客户端,
// 入向/出向两层钩子各司其职。与 core/load 的分工:breaker
// 看失败率(下游/业务健康度),adaptiveshedder 看 CPU 与
// 飞行请求数(自身负载),网关进程里经常两个都开。
//
// errorWindow 又一个环形数组:5 格存最近 5 条错误原因,
// index 前移 + (i+N)%N 反向遍历(新→旧)—— 与 ring.go 同款
// 手法,只是格子里存字符串。
// ————————————————————————————————————————————————————————————
package breaker

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/proc"
	"github.com/zeromicro/go-zero/core/stat"
	"github.com/zeromicro/go-zero/core/stringx"
)

// numHistoryReasons 错误环形日志的格数(只留最近 5 条)。
const numHistoryReasons = 5

// ErrServiceUnavailable is returned when the Breaker state is open.
// 拒绝请求的统一错误(经典术语叫"断路器打开")。
var ErrServiceUnavailable = errors.New("circuit breaker is open")

type (
	// Acceptable is the func to check if the error can be accepted.
	// 错误是否"可接受"(可接受 = 记成功,如业务 404)。
	Acceptable func(err error) bool

	// A Breaker represents a circuit breaker.
	// 断路器门面接口(实现见 circuitBreaker)。
	Breaker interface {
		// Name returns the name of the Breaker.
		Name() string

		// Allow checks if the request is allowed.
		// If allowed, a promise will be returned,
		// otherwise ErrServiceUnavailable will be returned as the error.
		// The caller needs to call promise.Accept() on success,
		// or call promise.Reject() on failure.
		// 手动模式:拿通行证,事后自己回报 Accept/Reject。
		Allow() (Promise, error)
		// AllowCtx checks if the request is allowed when ctx isn't done.
		AllowCtx(ctx context.Context) (Promise, error)

		// Do runs the given request if the Breaker accepts it.
		// Do returns an error instantly if the Breaker rejects the request.
		// If a panic occurs in the request, the Breaker handles it as an error
		// and causes the same panic again.
		Do(req func() error) error
		// DoCtx runs the given request if the Breaker accepts it when ctx isn't done.
		DoCtx(ctx context.Context, req func() error) error

		// DoWithAcceptable runs the given request if the Breaker accepts it.
		// DoWithAcceptable returns an error instantly if the Breaker rejects the request.
		// If a panic occurs in the request, the Breaker handles it as an error
		// and causes the same panic again.
		// acceptable checks if it's a successful call, even if the error is not nil.
		DoWithAcceptable(req func() error, acceptable Acceptable) error
		// DoWithAcceptableCtx runs the given request if the Breaker accepts it when ctx isn't done.
		DoWithAcceptableCtx(ctx context.Context, req func() error, acceptable Acceptable) error

		// DoWithFallback runs the given request if the Breaker accepts it.
		// DoWithFallback runs the fallback if the Breaker rejects the request.
		// If a panic occurs in the request, the Breaker handles it as an error
		// and causes the same panic again.
		DoWithFallback(req func() error, fallback Fallback) error
		// DoWithFallbackCtx runs the given request if the Breaker accepts it when ctx isn't done.
		DoWithFallbackCtx(ctx context.Context, req func() error, fallback Fallback) error

		// DoWithFallbackAcceptable runs the given request if the Breaker accepts it.
		// DoWithFallbackAcceptable runs the fallback if the Breaker rejects the request.
		// If a panic occurs in the request, the Breaker handles it as an error
		// and causes the same panic again.
		// acceptable checks if it's a successful call, even if the error is not nil.
		DoWithFallbackAcceptable(req func() error, fallback Fallback, acceptable Acceptable) error
		// DoWithFallbackAcceptableCtx runs the given request if the Breaker accepts it when ctx isn't done.
		DoWithFallbackAcceptableCtx(ctx context.Context, req func() error, fallback Fallback,
			acceptable Acceptable) error
	}

	// Fallback is the func to be called if the request is rejected.
	// 降级回调:被拒时返回兜底结果。
	Fallback func(err error) error

	// Option defines the method to customize a Breaker.
	// 选项(目前只有 WithName)。
	Option func(breaker *circuitBreaker)

	// Promise interface defines the callbacks that returned by Breaker.Allow.
	// 通行证:Allow 放行后事后回报结果用。
	Promise interface {
		// Accept tells the Breaker that the call is successful.
		Accept()
		// Reject tells the Breaker that the call is failed.
		Reject(reason string)
	}

	// internalPromise 内层通行证(Reject 不带 reason 参数,
	// 由外层 promiseWithReason 装饰补上)。
	internalPromise interface {
		Accept()
		Reject()
	}

	// circuitBreaker Breaker 的标准实现 = 名字 + 节流器。
	circuitBreaker struct {
		name string
		throttle
	}

	// internalThrottle 内层节流接口(Reject 无 reason 参数,
	// 由外层装饰器补上 reason 再对外暴露)。
	internalThrottle interface {
		allow() (internalPromise, error)
		doReq(req func() error, fallback Fallback, acceptable Acceptable) error
	}

	// throttle 对外节流接口。
	throttle interface {
		allow() (Promise, error)
		doReq(req func() error, fallback Fallback, acceptable Acceptable) error
	}
)

// NewBreaker returns a Breaker object.
// opts can be used to customize the Breaker.
// 创建断路器:名字缺省随机;节流器 = 日志装饰(googleBreaker)。
func NewBreaker(opts ...Option) Breaker {
	var b circuitBreaker
	for _, opt := range opts {
		opt(&b)
	}
	if len(b.name) == 0 {
		b.name = stringx.Rand()
	}
	b.throttle = newLoggedThrottle(b.name, newGoogleBreaker())

	return &b
}

func (cb *circuitBreaker) Allow() (Promise, error) {
	return cb.throttle.allow()
}

// AllowCtx ctx 已取消则直接返回 ctx.Err(不再判定)。
func (cb *circuitBreaker) AllowCtx(ctx context.Context) (Promise, error) {
	select {
	case <-ctx.Done():
		return nil, ctx.Err()
	default:
		return cb.Allow()
	}
}

// Do 最朴素形态:err==nil 才算成功。
func (cb *circuitBreaker) Do(req func() error) error {
	return cb.throttle.doReq(req, nil, defaultAcceptable)
}

func (cb *circuitBreaker) DoCtx(ctx context.Context, req func() error) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.Do(req)
	}
}

// DoWithAcceptable 自定义"哪些错误算成功"。
func (cb *circuitBreaker) DoWithAcceptable(req func() error, acceptable Acceptable) error {
	return cb.throttle.doReq(req, nil, acceptable)
}

func (cb *circuitBreaker) DoWithAcceptableCtx(ctx context.Context, req func() error,
	acceptable Acceptable) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.DoWithAcceptable(req, acceptable)
	}
}

// DoWithFallback 自定义降级:被拒时返回兜底结果。
func (cb *circuitBreaker) DoWithFallback(req func() error, fallback Fallback) error {
	return cb.throttle.doReq(req, fallback, defaultAcceptable)
}

func (cb *circuitBreaker) DoWithFallbackCtx(ctx context.Context, req func() error,
	fallback Fallback) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.DoWithFallback(req, fallback)
	}
}

// DoWithFallbackAcceptable 降级 + 可接受错误,两个扩展点全开。
func (cb *circuitBreaker) DoWithFallbackAcceptable(req func() error, fallback Fallback,
	acceptable Acceptable) error {
	return cb.throttle.doReq(req, fallback, acceptable)
}

func (cb *circuitBreaker) DoWithFallbackAcceptableCtx(ctx context.Context, req func() error,
	fallback Fallback, acceptable Acceptable) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	default:
		return cb.DoWithFallbackAcceptable(req, fallback, acceptable)
	}
}

func (cb *circuitBreaker) Name() string {
	return cb.name
}

// WithName returns a function to set the name of a Breaker.
// 选项:指定名字(同名共享统计,见 breakers.go 注册表)。
func WithName(name string) Option {
	return func(b *circuitBreaker) {
		b.name = name
	}
}

// defaultAcceptable 默认判定:只有 err==nil 算成功。
func defaultAcceptable(err error) bool {
	return err == nil
}

// loggedThrottle 日志装饰层:在内层节流器之上叠两件事 ——
// 最近 5 条错误原因的环形日志,以及断路打开时的 stat 上报
// (把"为什么断"带到报警现场)。
type loggedThrottle struct {
	name string
	internalThrottle
	errWin *errorWindow
}

func newLoggedThrottle(name string, t internalThrottle) loggedThrottle {
	return loggedThrottle{
		name:             name,
		internalThrottle: t,
		errWin:           new(errorWindow),
	}
}

// allow 装饰 allow:内层判定 → 外面包一层带 reason 记录的
// 通行证 → 拒绝则顺手记日志上报。
func (lt loggedThrottle) allow() (Promise, error) {
	promise, err := lt.internalThrottle.allow()
	return promiseWithReason{
		promise: promise,
		errWin:  lt.errWin,
	}, lt.logError(err)
}

// doReq 装饰 doReq:把调用方的 acceptable 包一层 ——
// 判定结果照传,但"不可接受的错误"顺手记进环形日志。
func (lt loggedThrottle) doReq(req func() error, fallback Fallback, acceptable Acceptable) error {
	return lt.logError(lt.internalThrottle.doReq(req, fallback, func(err error) bool {
		accept := acceptable(err)
		if !accept && err != nil {
			lt.errWin.add(err.Error())
		}
		return accept
	}))
}

// logError 拒绝(ErrServiceUnavailable)时上报:附上最近
// 5 条真实错误原因 —— 断的原因往往在被断的请求之前。
func (lt loggedThrottle) logError(err error) error {
	if errors.Is(err, ErrServiceUnavailable) {
		// if circuit open, not possible to have empty error window
		stat.Report(fmt.Sprintf(
			"proc(%s/%d), callee: %s, breaker is open and requests dropped\nlast errors:\n%s",
			proc.ProcessName(), proc.Pid(), lt.name, lt.errWin))
	}

	return err
}

// errorWindow 最近 N 条错误原因的环形日志(5 格),
// ring.go 同款手法:index 前移 + 模长回卷,格子存字符串。
type errorWindow struct {
	reasons [numHistoryReasons]string
	index   int
	count   int
	lock    sync.Mutex
}

// add 记一条(带时间戳),index 前移,回卷重用最老的格子。
func (ew *errorWindow) add(reason string) {
	ew.lock.Lock()
	ew.reasons[ew.index] = fmt.Sprintf("%s %s", time.Now().Format(time.TimeOnly), reason)
	ew.index = (ew.index + 1) % numHistoryReasons
	ew.count = min(ew.count+1, numHistoryReasons)
	ew.lock.Unlock()
}

// String 从新到旧列出已记录的原因(反着走环,
// (i+N)%N 防 Go 负数取模出负下标)。
func (ew *errorWindow) String() string {
	reasons := make([]string, 0, ew.count)

	ew.lock.Lock()
	// reverse order
	for i := ew.index - 1; i >= ew.index-ew.count; i-- {
		reasons = append(reasons, ew.reasons[(i+numHistoryReasons)%numHistoryReasons])
	}
	ew.lock.Unlock()

	return strings.Join(reasons, "\n")
}

// promiseWithReason 带原因记录的通行证:Reject 时先把
// reason 写进环形日志,再透传给内层。
type promiseWithReason struct {
	promise internalPromise
	errWin  *errorWindow
}

func (p promiseWithReason) Accept() {
	p.promise.Accept()
}

// Reject 记原因 + 透传(报文进日志,断路打开时上报用)。
func (p promiseWithReason) Reject(reason string) {
	p.errWin.add(reason)
	p.promise.Reject()
}
