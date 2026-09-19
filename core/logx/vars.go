// ————————————————————————————————————————————————————————————————————————————
// vars —— 全部常量、级别定义与包级变量 —— 文件总结
//
// 一、日志级别(uint32,数值越小越详细)
//
//	DebugLevel(0) → InfoLevel(1) → ErrorLevel(2) → SevereLevel(3)
//	→ disableLevel(0xff,完全关闭)。
//	shallLog 的判断是 logLevel <= level,即"配置级别之下的全打":
//	例如设为 ErrorLevel 时,error/slow/stack 都输出,debug/info 被屏蔽。
//	注意:slow 日志用 ErrorLevel 门槛(慢查询和错误同等重要),
//	debug 用 DebugLevel,info/stat 用 InfoLevel。
//
// 二、编码与轮转
//
//	jsonEncodingType / plainEncodingType 对应 LogConf.Encoding;
//	sizeRotationRule 对应 LogConf.Rotation 的 "size"。
//
// 三、文件名与模式
//
//	file/volume 模式下按类别落五个文件:
//	access.log(info/debug)、error.log(error/alert/stack)、
//	severe.log(fatal)、slow.log(slow)、stat.log(stat)。
//
// 四、字段 key
//
//	default*Key 是 JSON 日志的保留字段名,可用 LogConf.FieldKeys
//	覆盖(setupFieldKeys 会改写下面的 callerKey 等包级变量)。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"errors"

	"github.com/zeromicro/go-zero/core/syncx"
)

const (
	// DebugLevel logs everything
	// 最详细级别,一切日志都输出。
	DebugLevel uint32 = iota
	// InfoLevel does not include debugs
	// 输出 info 及以上,屏蔽 debug。
	InfoLevel
	// ErrorLevel includes errors, slows, stacks
	// 输出 error、slow、stack,屏蔽 info/debug。
	ErrorLevel
	// SevereLevel only log severe messages
	// 只输出 severe(fatal)级别的日志。
	SevereLevel
	// disableLevel doesn't log any messages
	// 完全关闭日志(Disable 使用),不属于级别序列。
	disableLevel = 0xff
)

const (
	// jsonEncodingType JSON 编码(默认),机器/采集系统友好。
	jsonEncodingType = iota
	// plainEncodingType 纯文本编码,人类可读,级别带彩色。
	plainEncodingType
)

const (
	// plainEncoding 编码名 "plain"(LogConf.Encoding 取值)。
	plainEncoding = "plain"
	// plainEncodingSep plain 编码的字段分隔符(制表符)。
	plainEncodingSep = '\t'
	// sizeRotationRule 轮转规则名 "size"(按大小轮转)。
	sizeRotationRule = "size"

	// file/volume 模式下按类别写入的五个固定文件名。
	accessFilename = "access.log"
	errorFilename  = "error.log"
	severeFilename = "severe.log"
	slowFilename   = "slow.log"
	statFilename   = "stat.log"

	// 日志输出模式名(LogConf.Mode 取值)。
	fileMode   = "file"
	volumeMode = "volume"

	// 日志级别字符串(写入日志 level 字段的取值)。
	levelAlert  = "alert"
	levelInfo   = "info"
	levelError  = "error"
	levelSevere = "severe"
	levelFatal  = "fatal"
	levelSlow   = "slow"
	levelStat   = "stat"
	levelDebug  = "debug"

	// backupFileDelimiter 轮转备份文件名中主名与日期/时间戳的分隔符。
	backupFileDelimiter = "-"
	// nilAngleString 对 nil 指针调用方法 panic 时的替代输出 "<nil>"。
	nilAngleString = "<nil>"
	// flags 标准库 log.Logger 的前缀标志,0 表示不自动加时间前缀。
	flags = 0x0
)

const (
	// JSON 日志保留字段的默认 key 名,可被 LogConf.FieldKeys 覆盖。
	defaultCallerKey    = "caller"
	defaultContentKey   = "content"
	defaultDurationKey  = "duration"
	defaultLevelKey     = "level"
	defaultSpanKey      = "span"
	defaultTimestampKey = "@timestamp"
	defaultTraceKey     = "trace"
	defaultTruncatedKey = "truncated"
)

var (
	// ErrLogPathNotSet is an error that indicates the log path is not set.
	// file/volume 模式下未配置日志路径时报错。
	ErrLogPathNotSet = errors.New("log path must be set")
	// ErrLogServiceNameNotSet is an error that indicates that the service name is not set.
	// volume 模式下未配置服务名时报错。
	ErrLogServiceNameNotSet = errors.New("log service name must be set")
	// ExitOnFatal defines whether to exit on fatal errors, defined here to make it easier to test.
	// Must 出错时的行为开关:true 退出进程, false panic。
	// 提成原子变量是为了测试中能临时改成 panic(logtest.PanicOnFatal)。
	ExitOnFatal = syncx.ForAtomicBool(true)

	// truncatedField 内容超长被截断时附加的标记字段 truncated=true。
	truncatedField = Field(truncatedKey, true)
)

var (
	// 日志条目保留字段的当前 key 名,初始为默认值,
	// SetUp 时可被 LogConf.FieldKeys 覆盖(setupFieldKeys)。
	callerKey    = defaultCallerKey
	contentKey   = defaultContentKey
	durationKey  = defaultDurationKey
	levelKey     = defaultLevelKey
	spanKey      = defaultSpanKey
	timestampKey = defaultTimestampKey
	traceKey     = defaultTraceKey
	truncatedKey = defaultTruncatedKey
)
