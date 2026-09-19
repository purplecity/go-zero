// ————————————————————————————————————————————————————————————————————————————
// config —— 日志配置结构体 —— 文件总结
//
// LogConf 是 go-zero 服务的日志配置,来自配置文件(如 yaml)的
// logx 段,由 logs.go 的 SetUp 消费。tag 采用 go-zero 的配置规范:
// default=xxx(默认值)、optional(可选)、options=[...](枚举)。
//
// 三种输出模式(Mode):
//
//	console —— 输出到控制台,开发/容器场景默认值;
//	file    —— 输出到 Path 目录下的 access.log / error.log /
//	           severe.log / slow.log / stat.log 五个文件;
//	volume  —— k8s 挂载卷场景,实际目录为 Path/ServiceName/主机名,
//	           因此 volume 模式必须配置 ServiceName。
//
// 两种编码(Encoding):json(默认,供采集系统)与 plain(人类可读,
//
//	开发时用,带彩色级别标签,见 color.go)。
//
// 两种轮转规则(Rotation):daily 按天(默认)与 size 按大小,
//
//	详见 rotatelogger.go。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

type (
	// A LogConf is a logging config.
	// LogConf 是日志系统的配置结构体,对应配置文件中的 logx 段。
	LogConf struct {
		// ServiceName represents the service name.
		// 服务名。volume 模式必填(用于拼接日志目录)。
		ServiceName string `json:",optional"`
		// Mode represents the logging mode, default is `console`.
		// console: log to console.
		// file: log to file.
		// volume: used in k8s, prepend the hostname to the log file name.
		// 输出模式:console 控制台 / file 文件 / volume k8s 卷
		// (目录为 Path/ServiceName/主机名)。
		Mode string `json:",default=console,options=[console,file,volume]"`
		// Encoding represents the encoding type, default is `json`.
		// json: json encoding.
		// plain: plain text encoding, typically used in development.
		// 编码类型:json(默认,机器友好)或 plain(人类可读,开发用,
		// 级别标签带彩色,制表符分隔)。
		Encoding string `json:",default=json,options=[json,plain]"`
		// TimeFormat represents the time format, default is `2006-01-02T15:04:05.000Z07:00`.
		// 日志内容里时间戳的格式(Go 参考时间布局)。
		TimeFormat string `json:",optional"`
		// Path represents the log file path, default is `logs`.
		// 日志文件目录,file/volume 模式生效。
		Path string `json:",default=logs"`
		// Level represents the log level, default is `info`.
		// 日志级别:debug/info/error/severe。数字越小越详细,
		// 设为 error 后 debug、info 都不再输出(见 vars.go 的级别常量)。
		Level string `json:",default=info,options=[debug,info,error,severe]"`
		// MaxContentLength represents the max content bytes, default is no limit.
		// 单条日志 content 的最大字节数,超长截断并附加 truncated=true
		// 字段;0 表示不限制。
		MaxContentLength uint32 `json:",optional"`
		// Compress represents whether to compress the log file, default is `false`.
		// 轮转出的旧日志是否自动 gzip 压缩(压缩成功后删除原文件)。
		Compress bool `json:",optional"`
		// Stat represents whether to log statistics, default is `true`.
		// 是否输出统计日志(stat.log);false 时 logx.Stat/Statf 静默。
		Stat bool `json:",default=true"`
		// KeepDays represents how many days the log files will be kept. Default to keep all files.
		// Only take effect when Mode is `file` or `volume`, both work when Rotation is `daily` or `size`.
		// 日志保留天数,超过自动删除;0 表示永久保留。
		KeepDays int `json:",optional"`
		// StackCooldownMillis represents the cooldown time for stack logging, default is 100ms.
		// 堆栈日志的限频冷却时间(毫秒):防止连环出错时疯狂刷调用栈。
		StackCooldownMillis int `json:",default=100"`
		// MaxBackups represents how many backup log files will be kept. 0 means all files will be kept forever.
		// Only take effect when RotationRuleType is `size`.
		// Even though `MaxBackups` sets 0, log files will still be removed
		// if the `KeepDays` limitation is reached.
		// 最多保留多少个轮转备份文件;0 表示不限个数(但仍受 KeepDays
		// 约束)。仅 size 轮转规则生效。
		MaxBackups int `json:",default=0"`
		// MaxSize represents how much space the writing log file takes up. 0 means no limit. The unit is `MB`.
		// Only take effect when RotationRuleType is `size`
		// 单个日志文件大小上限(MB),超过触发轮转;0 表示不限。
		// 仅 size 轮转规则生效。
		MaxSize int `json:",default=0"`
		// Rotation represents the type of log rotation rule. Default is `daily`.
		// daily: daily rotation.
		// size: size limited rotation.
		// 轮转规则:daily 按天轮转(备份名带日期);size 按大小轮转
		// (备份名带 RFC3339 时间戳)。
		Rotation string `json:",default=daily,options=[daily,size]"`
		// FileTimeFormat represents the time format for file name, default is `2006-01-02T15:04:05.000Z07:00`.
		// size 轮转时备份文件名里时间戳的格式。
		FileTimeFormat string `json:",optional"`
		// FieldKeys represents the field keys.
		// 自定义日志 JSON 中各保留字段的 key 名(见下方 fieldKeyConf)。
		FieldKeys fieldKeyConf `json:",optional"`
	}

	// fieldKeyConf 自定义日志条目中保留字段的 JSON key。
	// 例如想把时间戳字段从 "@timestamp" 改名成 "time",
	// 在配置里设置 FieldKeys.TimestampKey 即可。
	fieldKeyConf struct {
		// CallerKey represents the caller key.
		// 调用位置字段名,默认 "caller"。
		CallerKey string `json:",default=caller"`
		// ContentKey represents the content key.
		// 日志内容字段名,默认 "content"。
		ContentKey string `json:",default=content"`
		// DurationKey represents the duration key.
		// 耗时字段名,默认 "duration"(WithDuration 时附加)。
		DurationKey string `json:",default=duration"`
		// LevelKey represents the level key.
		// 级别字段名,默认 "level"。
		LevelKey string `json:",default=level"`
		// SpanKey represents the context trace span id key.
		// span id 字段名,默认 "span"。
		SpanKey string `json:",default=span"`
		// TimestampKey represents the timestamp key.
		// 时间戳字段名,默认 "@timestamp"。
		TimestampKey string `json:",default=@timestamp"`
		// TraceKey represents the context trace id key.
		// trace id 字段名,默认 "trace"。
		TraceKey string `json:",default=trace"`
		// TruncatedKey represents the truncated key.
		// 截断标记字段名,默认 "truncated"(内容超长被截断时置 true)。
		TruncatedKey string `json:",default=truncated"`
	}
)
