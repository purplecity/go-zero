// ————————————————————————————————————————————————————————————————————————————
// logs —— logx 包的核心:全局日志 API 与初始化 —— 文件总结
//
// 一、整体分层(一条日志的旅程)
//
//	业务调用 logx.Info/Infof/... (本文件)
//	  → shallLog 级别过滤(原子读,级别关闭时连 fmt.Sprint 都不做,零浪费)
//	  → writeInfo/writeError/... :加 caller 字段 + 合并全局字段
//	  → getWriter() 取当前 Writer(writer.go)
//	  → concreteWriter 按 level 分流到 console 或五个分类文件
//	  → output() 统一编码(json 或 plain)后写出。
//
// 二、函数命名后缀约定(以 Info 为例,Debug/Error/Slow 同理)
//
//	Info(...)   基础版,fmt.Sprint 空格拼接;      Infof   fmt.Sprintf 格式化;
//	Infov(对象) 整值 JSON 编码进 content;        Infow   msg + 结构化字段;
//	Infofn(fn)  惰性求值:级别未开启时 fn 不执行,省掉昂贵的构造开销。
//	特别地:Severe 用于致命错误(自动附调用栈),Stat 用于统计,
//	ErrorStack 手动附带调用栈,Must 在 err != nil 时记日志并退出进程。
//
// 三、并发模型
//
//	日志级别、编码、截断上限、统计开关都是 uint32 + atomic 操作;
//	writer 是 atomicWriter(内部 RWMutex),SetUp 用 sync.Once 保证
//	多服务共存于一个进程时只有第一次初始化生效。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"fmt"
	"io"
	"log"
	"os"
	"path"
	"reflect"
	"runtime/debug"
	"sync"
	"sync/atomic"

	"github.com/zeromicro/go-zero/core/sysx"
)

// callerDepth 是 runtime.Caller 的回溯深度:
// 用户代码 → logx 公开 API → writeXxx → addCaller → getCaller,
// 固定取第 4 层,让日志的 caller 字段指向真实的业务调用位置。
const callerDepth = 4

var (
	// timeFormat 日志时间戳格式,可被 LogConf.TimeFormat 覆盖(SetUp)。
	timeFormat = "2006-01-02T15:04:05.000Z07:00"
	// encoding 当前编码(json/plain),uint32 是为了原子读写。
	encoding uint32 = jsonEncodingType
	// maxContentLength is used to truncate the log content, 0 for not truncating.
	// 单条 content 的最大长度,超长截断;0 表示不截断。
	maxContentLength uint32
	// use uint32 for atomic operations
	// 以下三个状态全部用 uint32 + atomic 操作,支持运行期无锁修改。
	disableStat uint32
	logLevel    uint32
	// options 轮转相关选项(keepDays/maxSize/gzip 等),SetUp 期间写入。
	options logOptions
	// writer 当前的日志写出器,初始为空,首次写日志时惰性落到控制台。
	writer = new(atomicWriter)
	// setupOnce 保证 SetUp 只真正执行一次(多服务共用进程时,
	// 后续 SetUp 调用不会覆盖首次配置)。
	setupOnce sync.Once
)

type (
	// LogField is a key-value pair that will be added to the log entry.
	// 结构化日志的最小单元:一个 key-value 对,
	// 通过 logx.Field("uid", 1001) 构造,配合 Infow/Errorw 使用。
	LogField struct {
		Key   string
		Value any
	}

	// LogOption defines the method to customize the logging.
	// 日志选项(函数式选项模式):WithGzip/WithKeepDays/WithMaxSize 等
	// 返回该类型,在 createOutput 时应用到 logOptions。
	LogOption func(options *logOptions)

	// logEntry 单条日志的临时载体:key → value,
	// json 编码时直接序列化它,plain 编码时转成 k=v 数组。
	logEntry map[string]any

	// logOptions 轮转与压缩相关的内部选项集合,
	// 由 LogConf 各字段经 WithXxx 选项填充,createOutput 消费。
	logOptions struct {
		gzipEnabled           bool
		logStackCooldownMills int
		keepDays              int
		maxBackups            int
		maxSize               int
		rotationRule          string
	}
)

// AddWriter adds a new writer.
// If there is already a writer, the new writer will be added to the writer chain.
// For example, to write logs to both file and console, if there is already a file writer,
// ```go
// logx.AddWriter(logx.NewWriter(os.Stdout))
// ```
// 追加一个自定义 Writer(例如同时写文件和控制台、或转发到 kafka)。
// 若当前已有 writer,会把新旧打包成 comboWriter 做多路广播;
// 若当前没有(Reset 过),新 writer 直接生效。
func AddWriter(w Writer) {
	ow := Reset()
	if ow == nil {
		SetWriter(w)
	} else {
		// no need to check if the existing writer is a comboWriter,
		// because it is not common to add more than one writer.
		// even more than one writer, the behavior is the same.
		// 嵌套 comboWriter 也没关系:广播语义等价,不必展平。
		SetWriter(comboWriter{
			writers: []Writer{ow, w},
		})
	}
}

// Alert alerts v in alert level, and the message is written to error log.
// 告警级别:落在 error 流(error.log/error 控制台),
// 但 level 字段标为 "alert",便于告警系统单独订阅。
func Alert(v string) {
	getWriter().Alert(v)
}

// Close closes the logging.
// 关闭日志:原子取走当前 writer(取走后立即置空,后续日志走
// 惰性控制台兜底),并 Close 它(文件场景刷盘+关文件)。
// 进程退出前调用,避免丢失缓冲中的日志。
func Close() error {
	if w := writer.Swap(nil); w != nil {
		return w.(io.Closer).Close()
	}

	return nil
}

// Debug writes v into access log.
// 调试级别(基础版)。先查级别再拼串:级别关闭时
// fmt.Sprint 都不会执行,这是日志库性能的关键设计。
func Debug(v ...any) {
	if shallLog(DebugLevel) {
		writeDebug(fmt.Sprint(v...))
	}
}

// Debugf writes v with format into access log.
// 调试级别(格式化版)。
func Debugf(format string, v ...any) {
	if shallLog(DebugLevel) {
		writeDebug(fmt.Sprintf(format, v...))
	}
}

// Debugfn writes function result into access log if debug level enabled.
// This is useful when the function is expensive to call and debug level disabled.
// 调试级别(惰性版):级别开启才调用 fn 取日志内容,
// 适合内容构造昂贵的场景(如序列化大对象)。
func Debugfn(fn func() any) {
	if shallLog(DebugLevel) {
		writeDebug(fn())
	}
}

// Debugv writes v into access log with json content.
// 调试级别(对象版):任意值整体 JSON 编码进 content。
func Debugv(v any) {
	if shallLog(DebugLevel) {
		writeDebug(v)
	}
}

// Debugw writes msg along with fields into the access log.
// 调试级别(结构化字段版):msg + 若干 LogField。
func Debugw(msg string, fields ...LogField) {
	if shallLog(DebugLevel) {
		writeDebug(msg, fields...)
	}
}

// Disable disables the logging.
// 彻底关闭日志:级别设为 disableLevel,并把 writer 换成空实现。
// SetWriter 也会检查级别,防止关闭后被意外重新设置。
func Disable() {
	atomic.StoreUint32(&logLevel, disableLevel)
	writer.Store(nopWriter{})
}

// DisableStat disables the stat logs.
// 关闭统计日志(logx.Stat/Statf 静默),服务启动配置 Stat=false 时调用。
func DisableStat() {
	atomic.StoreUint32(&disableStat, 1)
}

// Error writes v into error log.
// 错误级别(基础版),写入 error 流。
func Error(v ...any) {
	if shallLog(ErrorLevel) {
		writeError(fmt.Sprint(v...))
	}
}

// Errorf writes v with format into error log.
// 错误级别(格式化版)。
func Errorf(format string, v ...any) {
	if shallLog(ErrorLevel) {
		writeError(fmt.Errorf(format, v...).Error())
	}
}

// Errorfn writes function result into error log.
// 错误级别(惰性版):级别开启才调用 fn。
func Errorfn(fn func() any) {
	if shallLog(ErrorLevel) {
		writeError(fn())
	}
}

// ErrorStack writes v along with call stack into error log.
// 错误级别 + 当前调用栈:排查偶发错误来源时使用。
func ErrorStack(v ...any) {
	if shallLog(ErrorLevel) {
		// there is newline in stack string
		writeStack(fmt.Sprint(v...))
	}
}

// ErrorStackf writes v along with call stack in format into error log.
// 错误级别 + 调用栈(格式化版)。
func ErrorStackf(format string, v ...any) {
	if shallLog(ErrorLevel) {
		// there is newline in stack string
		writeStack(fmt.Sprintf(format, v...))
	}
}

// Errorv writes v into error log with json content.
// No call stack attached, because not elegant to pack the messages.
// 错误级别(对象版)。刻意不自动附调用栈:对结构化对象拼栈不优雅,
// 需要栈时请用 ErrorStack。
func Errorv(v any) {
	if shallLog(ErrorLevel) {
		writeError(v)
	}
}

// Errorw writes msg along with fields into the error log.
// 错误级别(结构化字段版),推荐用法:
// logx.Errorw("order failed", logx.Field("orderId", id))。
func Errorw(msg string, fields ...LogField) {
	if shallLog(ErrorLevel) {
		writeError(msg, fields...)
	}
}

// Field returns a LogField for the given key and value.
// 构造一个结构化日志字段。
func Field(key string, value any) LogField {
	return LogField{
		Key:   key,
		Value: value,
	}
}

// Info writes v into access log.
// 信息级别(基础版),日常业务日志。
func Info(v ...any) {
	if shallLog(InfoLevel) {
		writeInfo(fmt.Sprint(v...))
	}
}

// Infof writes v with format into access log.
// 信息级别(格式化版)。
func Infof(format string, v ...any) {
	if shallLog(InfoLevel) {
		writeInfo(fmt.Sprintf(format, v...))
	}
}

// Infofn writes function result into access log.
// This is useful when the function is expensive to call and info level disabled.
// 信息级别(惰性版)。
func Infofn(fn func() any) {
	if shallLog(InfoLevel) {
		writeInfo(fn())
	}
}

// Infov writes v into access log with json content.
// 信息级别(对象版)。
func Infov(v any) {
	if shallLog(InfoLevel) {
		writeInfo(v)
	}
}

// Infow writes msg along with fields into the access log.
// 信息级别(结构化字段版)。
func Infow(msg string, fields ...LogField) {
	if shallLog(InfoLevel) {
		writeInfo(msg, fields...)
	}
}

// Must checks if err is nil, otherwise logs the error and exits.
// err 非 nil 时:打印消息与调用栈到标准 log 和 severe 日志,
// 然后(默认)直接退出进程;测试中可通过 ExitOnFatal 改为 panic。
// 用于启动期"出错即无法继续"的强校验,不要在请求路径使用。
func Must(err error) {
	if err == nil {
		return
	}

	msg := fmt.Sprintf("%+v\n\n%s", err.Error(), debug.Stack())
	log.Print(msg)
	getWriter().Severe(msg)

	if ExitOnFatal.True() {
		os.Exit(1)
	} else {
		panic(msg)
	}
}

// MustSetup sets up logging with given config c. It exits on error.
// 初始化日志,失败即退出进程,服务启动阶段的标准用法。
func MustSetup(c LogConf) {
	Must(SetUp(c))
}

// Reset clears the writer and resets the log level.
// 原子取走并返回当前 writer(同时置空)。AddWriter/测试环境
// 用它实现"取旧的、换新的"。之后日志走惰性控制台兜底。
func Reset() Writer {
	return writer.Swap(nil)
}

// SetLevel sets the logging level. It can be used to suppress some logs.
// 运行期动态调整日志级别(原子写,立即对全部 goroutine 生效)。
func SetLevel(level uint32) {
	atomic.StoreUint32(&logLevel, level)
}

// SetWriter sets the logging writer. It can be used to customize the logging.
// 设置自定义 writer(如转发 kafka);日志处于 Disable 状态时拒绝设置,
// 防止绕过关闭开关。
func SetWriter(w Writer) {
	if atomic.LoadUint32(&logLevel) != disableLevel {
		writer.Store(w)
	}
}

// SetUp sets up the logx.
// If already set up, return nil.
// We allow SetUp to be called multiple times, because, for example,
// we need to allow different service frameworks to initialize logx respectively.
// 日志系统初始化入口(通常由 ServiceConf 自动调用):
//  1. 设置级别、字段 key 名、统计开关、时间格式、截断上限;
//  2. 按编码配置选择 json 或 plain;
//  3. 按模式装配 writer:console(默认)/ file(五分类文件)/
//     volume(k8s,目录加服务名与主机名)。
//
// 用 sync.Once 保证一个进程内多次调用只有第一次生效
// (多个服务框架各自初始化时不互相覆盖)。
func SetUp(c LogConf) (err error) {
	// Ignore the later SetUp calls.
	// Because multiple services in one process might call SetUp respectively.
	// Need to wait for the first caller to complete the execution.
	setupOnce.Do(func() {
		setupLogLevel(c.Level)
		setupFieldKeys(c.FieldKeys)

		if !c.Stat {
			DisableStat()
		}

		if len(c.TimeFormat) > 0 {
			timeFormat = c.TimeFormat
		}

		if len(c.FileTimeFormat) > 0 {
			fileTimeFormat = c.FileTimeFormat
		}

		atomic.StoreUint32(&maxContentLength, c.MaxContentLength)

		switch c.Encoding {
		case plainEncoding:
			atomic.StoreUint32(&encoding, plainEncodingType)
		default:
			atomic.StoreUint32(&encoding, jsonEncodingType)
		}

		switch c.Mode {
		case fileMode:
			err = setupWithFiles(c)
		case volumeMode:
			err = setupWithVolume(c)
		default:
			setupWithConsole()
		}
	})

	return
}

// Severe writes v into severe log.
// 致命错误级别:自动附当前调用栈,写入 severe.log。
func Severe(v ...any) {
	if shallLog(SevereLevel) {
		writeSevere(fmt.Sprint(v...))
	}
}

// Severef writes v with format into severe log.
// 致命错误级别(格式化版)。
func Severef(format string, v ...any) {
	if shallLog(SevereLevel) {
		writeSevere(fmt.Sprintf(format, v...))
	}
}

// Slow writes v into slow log.
// 慢日志:记录慢操作(如慢 SQL),注意门槛是 ErrorLevel
// (级别语义上 slow 与 error 同级,都会在 ErrorLevel 下输出)。
func Slow(v ...any) {
	if shallLog(ErrorLevel) {
		writeSlow(fmt.Sprint(v...))
	}
}

// Slowf writes v with format into slow log.
// 慢日志(格式化版)。
func Slowf(format string, v ...any) {
	if shallLog(ErrorLevel) {
		writeSlow(fmt.Sprintf(format, v...))
	}
}

// Slowfn writes function result into slow log.
// This is useful when the function is expensive to call and slow level disabled.
// 慢日志(惰性版)。
func Slowfn(fn func() any) {
	if shallLog(ErrorLevel) {
		writeSlow(fn())
	}
}

// Slowv writes v into slow log with json content.
// 慢日志(对象版)。
func Slowv(v any) {
	if shallLog(ErrorLevel) {
		writeSlow(v)
	}
}

// Sloww writes msg along with fields into slow log.
// 慢日志(结构化字段版),例如:
// logx.Slowf("slow sql, trace id %s", traceID)。
func Sloww(msg string, fields ...LogField) {
	if shallLog(ErrorLevel) {
		writeSlow(msg, fields...)
	}
}

// Stat writes v into stat log.
// 统计日志:周期性的业务/资源统计(qps、连接数等),
// 需同时满足"统计未关闭"与"级别达到 InfoLevel"。
func Stat(v ...any) {
	if shallLogStat() && shallLog(InfoLevel) {
		writeStat(fmt.Sprint(v...))
	}
}

// Statf writes v with format into stat log.
// 统计日志(格式化版)。
func Statf(format string, v ...any) {
	if shallLogStat() && shallLog(InfoLevel) {
		writeStat(fmt.Sprintf(format, v...))
	}
}

// WithCooldownMillis customizes logging on writing call stack interval.
// 选项:设置堆栈日志的限频冷却毫秒数(防连环报错刷栈)。
func WithCooldownMillis(millis int) LogOption {
	return func(opts *logOptions) {
		opts.logStackCooldownMills = millis
	}
}

// WithKeepDays customizes logging to keep logs with days.
// 选项:日志保留天数,超期自动删除。
func WithKeepDays(days int) LogOption {
	return func(opts *logOptions) {
		opts.keepDays = days
	}
}

// WithGzip customizes logging to automatically gzip the log files.
// 选项:轮转出的旧日志自动 gzip 压缩。
func WithGzip() LogOption {
	return func(opts *logOptions) {
		opts.gzipEnabled = true
	}
}

// WithMaxBackups customizes how many log files backups will be kept.
// 选项:最多保留的备份文件个数(仅 size 轮转生效)。
func WithMaxBackups(count int) LogOption {
	return func(opts *logOptions) {
		opts.maxBackups = count
	}
}

// WithMaxSize customizes how much space the writing log file can take up.
// 选项:单文件大小上限 MB(仅 size 轮转生效)。
func WithMaxSize(size int) LogOption {
	return func(opts *logOptions) {
		opts.maxSize = size
	}
}

// WithRotation customizes which log rotation rule to use.
// 选项:选择轮转规则 daily / size。
func WithRotation(r string) LogOption {
	return func(opts *logOptions) {
		opts.rotationRule = r
	}
}

// addCaller 给字段追加 caller(调用位置)。
func addCaller(fields ...LogField) []LogField {
	return append(fields, Field(callerKey, getCaller(callerDepth)))
}

// createOutput 为指定文件路径创建带轮转规则的 RotateLogger:
// 按 options.rotationRule 选择 size 规则或默认按天规则。
func createOutput(path string) (io.WriteCloser, error) {
	if len(path) == 0 {
		return nil, ErrLogPathNotSet
	}

	var rule RotateRule
	switch options.rotationRule {
	case sizeRotationRule:
		rule = NewSizeLimitRotateRule(path, backupFileDelimiter, options.keepDays, options.maxSize,
			options.maxBackups, options.gzipEnabled)
	default:
		rule = DefaultRotateRule(path, backupFileDelimiter, options.keepDays, options.gzipEnabled)
	}

	return NewLogger(path, rule, options.gzipEnabled)
}

// encodeError 安全地取 error 文本:error.Error() panic 时兜底。
func encodeError(err error) (ret string) {
	return encodeWithRecover(err, func() string {
		return err.Error()
	})
}

// encodeStringer 安全地取 Stringer 文本:String() panic 时兜底。
func encodeStringer(v fmt.Stringer) (ret string) {
	return encodeWithRecover(v, func() string {
		return v.String()
	})
}

// encodeWithRecover 执行 fn,捕获其中的 panic:
// 入参是 nil 指针时输出 "<nil>",其他 panic 输出 "panic: 原因"。
// 防止业务类型的 Error()/String() 实现有 bug 时拖垮日志调用方。
func encodeWithRecover(arg any, fn func() string) (ret string) {
	defer func() {
		if err := recover(); err != nil {
			if v := reflect.ValueOf(arg); v.Kind() == reflect.Ptr && v.IsNil() {
				ret = nilAngleString
			} else {
				ret = fmt.Sprintf("panic: %v", err)
			}
		}
	}()

	return fn()
}

// getWriter 取当前 writer;为空(未 SetUp)时惰性创建控制台 writer
// 并用 StoreIfNil 原子占位 —— 并发下只会有一个生效,其余直接复用。
func getWriter() Writer {
	w := writer.Load()
	if w == nil {
		w = writer.StoreIfNil(newConsoleWriter())
	}

	return w
}

// handleOptions 依次应用函数式选项到全局 options。
func handleOptions(opts []LogOption) {
	for _, opt := range opts {
		opt(&options)
	}
}

// setupFieldKeys 用配置覆盖日志保留字段的 key 名,
// 只覆盖非空配置项,未配置的保持默认值。
func setupFieldKeys(c fieldKeyConf) {
	if len(c.CallerKey) > 0 {
		callerKey = c.CallerKey
	}
	if len(c.ContentKey) > 0 {
		contentKey = c.ContentKey
	}
	if len(c.DurationKey) > 0 {
		durationKey = c.DurationKey
	}
	if len(c.LevelKey) > 0 {
		levelKey = c.LevelKey
	}
	if len(c.SpanKey) > 0 {
		spanKey = c.SpanKey
	}
	if len(c.TimestampKey) > 0 {
		timestampKey = c.TimestampKey
	}
	if len(c.TraceKey) > 0 {
		traceKey = c.TraceKey
	}
	if len(c.TruncatedKey) > 0 {
		truncatedKey = c.TruncatedKey
	}
}

// setupLogLevel 把配置里的级别字符串映射成级别常量,
// 未识别的值保持默认 InfoLevel。
func setupLogLevel(level string) {
	switch level {
	case levelDebug:
		SetLevel(DebugLevel)
	case levelInfo:
		SetLevel(InfoLevel)
	case levelError:
		SetLevel(ErrorLevel)
	case levelSevere:
		SetLevel(SevereLevel)
	}
}

// setupWithConsole 控制台模式装配(默认)。
func setupWithConsole() {
	SetWriter(newConsoleWriter())
}

// setupWithFiles 文件模式装配:在 Path 下创建五分类文件 writer。
func setupWithFiles(c LogConf) error {
	w, err := newFileWriter(c)
	if err != nil {
		return err
	}

	SetWriter(w)
	return nil
}

// setupWithVolume k8s 卷模式装配:目录加上服务名与主机名
// (Path/ServiceName/hostname),便于按服务、按节点归集日志。
func setupWithVolume(c LogConf) error {
	if len(c.ServiceName) == 0 {
		return ErrLogServiceNameNotSet
	}

	c.Path = path.Join(c.Path, c.ServiceName, sysx.Hostname())
	return setupWithFiles(c)
}

// shallLog 判断给定级别是否应该输出:
// logLevel <= level 即"配置级别之下的全打"(级别数值越小越详细)。
func shallLog(level uint32) bool {
	return atomic.LoadUint32(&logLevel) <= level
}

// shallLogStat 判断统计日志是否被关闭。
func shallLogStat() bool {
	return atomic.LoadUint32(&disableStat) == 0
}

// —— 以下 writeXxx 是各级别的内部写出函数 ——

// writeDebug writes v into debug log.
// Not checking shallLog here is for performance consideration.
// If we check here, the fmt.Sprint might be called even if the log level is not enabled.
// The caller should check shallLog before calling this function.
// 调试日志:附加 caller + 合并全局字段后交给 writer。
// 刻意不再检查级别 —— 级别过滤已在公开 API 里完成,
// 这里若重复检查,fmt.Sprint 的拼接成本就白花了。
func writeDebug(val any, fields ...LogField) {
	getWriter().Debug(val, mergeGlobalFields(addCaller(fields...))...)
}

// writeError writes v into the error log.
// Not checking shallLog here is for performance consideration.
// The caller should check shallLog before calling this function.
// 错误日志:同 writeDebug,分流到 error 流。
func writeError(val any, fields ...LogField) {
	getWriter().Error(val, mergeGlobalFields(addCaller(fields...))...)
}

// writeInfo writes v into info log.
// Not checking shallLog here is for performance consideration.
// The caller should check shallLog before calling this function.
// 信息日志:同 writeDebug,分流到 access 流。
func writeInfo(val any, fields ...LogField) {
	getWriter().Info(val, mergeGlobalFields(addCaller(fields...))...)
}

// writeSevere writes v into severe log.
// Not checking shallLog here is for performance consideration.
// The caller should check shallLog before calling this function.
// 致命错误日志:消息后拼接当前调用栈。
func writeSevere(msg string) {
	getWriter().Severe(fmt.Sprintf("%s\n%s", msg, string(debug.Stack())))
}

// writeSlow writes v into slow log.
// Not checking shallLog here is for performance consideration.
// The caller should check shallLog before calling this function.
// 慢日志:同 writeDebug,分流到 slow 流。
func writeSlow(val any, fields ...LogField) {
	getWriter().Slow(val, mergeGlobalFields(addCaller(fields...))...)
}

// writeStack writes v into stack log.
// Not checking shallLog here is for performance consideration.
// The caller should check shallLog before calling this function.
// 错误 + 调用栈日志(ErrorStack 系列):经 lessWriter 限频输出,
// 防止连环报错时疯狂刷栈。
func writeStack(msg string) {
	getWriter().Stack(fmt.Sprintf("%s\n%s", msg, string(debug.Stack())))
}

// writeStat writes v into the stat log.
// Not checking shallLog here is for performance consideration.
// The caller should check shallLog before calling this function.
// 统计日志:分流到 stat 流。
func writeStat(msg string) {
	getWriter().Stat(msg, mergeGlobalFields(addCaller())...)
}
