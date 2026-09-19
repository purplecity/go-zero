// ————————————————————————————————————————————————————————————————————————————
// writer —— 日志写出层:Writer 接口与各实现、编码输出 —— 文件总结
//
// 一、类型关系
//
//	Writer(接口)          日志写出器抽象,用户可实现它把日志转发到
//	                       kafka/数据库/第三方日志系统;
//	├─ concreteWriter      真实实现:按级别分流到六股输出流
//	│                        infoLog(access) / errorLog / severeLog(fatal)
//	│                        / slowLog / statLog / stackLog(调用栈)
//	├─ comboWriter         多路广播:一条日志写给多个 Writer
//	└─ nopWriter           空实现(Disable 后使用)
//	atomicWriter           并发容器:给 Writer 加 RWMutex 的
//	                       Load/Store/StoreIfNil/Swap(logs.go 的 writer 变量)
//
// 二、流的路由(newConsoleWriter / newFileWriter)
//
//	info/debug/stat → infoLog;error/alert → errorLog;
//	severe → severeLog;slow → slowLog;Stack → stackLog,
//	且 stackLog 是 lessWriter 限频包装(默认 100ms 冷却),
//	防止连环报错时疯狂刷调用栈。file 模式下五股流对应
//	access.log / error.log / severe.log / slow.log / stat.log。
//
// 三、output():所有日志的最终编码出口
//
//	字符串超长截断(truncated=true)→ Sensitive 脱敏 → 处理字段值
//	(error/Stringer 等转字符串、防 panic)→ 按 encoding 输出
//	json(map 直接序列化)或 plain(时间\t级别\t内容\tk=v...)。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"bytes"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"path"
	"runtime/debug"
	"sync"
	"sync/atomic"
	"time"

	fatihcolor "github.com/fatih/color"
	"github.com/zeromicro/go-zero/core/color"
	"github.com/zeromicro/go-zero/core/errorx"
)

type (
	// Writer is the interface for writing logs.
	// It's designed to let users customize their own log writer,
	// such as writing logs to a kafka, a database, or using third-party loggers.
	// Writer 是日志写出器接口,logx 内置控制台/文件实现,
	// 用户也可实现它接入 kafka、数据库等自定义目的地,
	// 通过 logx.AddWriter/SetWriter 挂载。
	Writer interface {
		// Alert sends an alert message, if your writer implemented alerting functionality.
		// 告警消息(如 writer 接了告警系统则在此触发)。
		Alert(v any)
		// Close closes the writer.
		// 关闭并释放资源(文件场景刷盘)。
		Close() error
		// Debug logs a message at debug level.
		// 调试级别。
		Debug(v any, fields ...LogField)
		// Error logs a message at error level.
		// 错误级别。
		Error(v any, fields ...LogField)
		// Info logs a message at info level.
		// 信息级别。
		Info(v any, fields ...LogField)
		// Severe logs a message at severe level.
		// 致命级别(自动附栈,由上层拼好)。
		Severe(v any)
		// Slow logs a message at slow level.
		// 慢日志级别。
		Slow(v any, fields ...LogField)
		// Stack logs a message at error level.
		// 调用栈日志(限频输出)。
		Stack(v any)
		// Stat logs a message at stat level.
		// 统计级别。
		Stat(v any, fields ...LogField)
	}

	// atomicWriter 并发安全的 Writer 容器:
	// logx 全局的 writer 变量就是这个类型,
	// 内部 RWMutex 保证热替换 writer 时的读写安全。
	atomicWriter struct {
		writer Writer
		lock   sync.RWMutex
	}

	// comboWriter 多路广播 writer:把每个方法扇出到持有的
	// 全部 writer(AddWriter 追加目的地时创建)。
	comboWriter struct {
		writers []Writer
	}

	// concreteWriter 按级别分流的具体 writer:
	// 六股输出流按用途分开(见文件头"流的路由")。
	concreteWriter struct {
		infoLog   io.WriteCloser
		errorLog  io.WriteCloser
		severeLog io.WriteCloser
		slowLog   io.WriteCloser
		statLog   io.WriteCloser
		stackLog  io.Writer
	}
)

// NewWriter creates a new Writer with the given io.Writer.
// 把任意 io.Writer(内存 buffer、网络连接、第三方客户端……)
// 包装成 Writer:六股流全部指向它,即"所有级别写同一目的地"。
// 测试里捕获日志(logtest 包)正是用 NewWriter(&buf)。
func NewWriter(w io.Writer) Writer {
	lw := newLogWriter(log.New(w, "", flags))

	return &concreteWriter{
		infoLog:   lw,
		errorLog:  lw,
		severeLog: lw,
		slowLog:   lw,
		statLog:   lw,
		stackLog:  lw,
	}
}

// Load 读取当前 writer(读锁)。
func (w *atomicWriter) Load() Writer {
	w.lock.RLock()
	defer w.lock.RUnlock()
	return w.writer
}

// Store 替换当前 writer(写锁)。
func (w *atomicWriter) Store(v Writer) {
	w.lock.Lock()
	defer w.lock.Unlock()
	w.writer = v
}

// StoreIfNil 仅当当前为空时写入(常用于惰性初始化占位),
// 无论是否写入都返回最终生效的 writer。
func (w *atomicWriter) StoreIfNil(v Writer) Writer {
	w.lock.Lock()
	defer w.lock.Unlock()

	if w.writer == nil {
		w.writer = v
	}

	return w.writer
}

// Swap 原子换出:返回旧 writer 并写入新值(Reset/Close 用)。
func (w *atomicWriter) Swap(v Writer) Writer {
	w.lock.Lock()
	defer w.lock.Unlock()
	old := w.writer
	w.writer = v
	return old
}

// —— comboWriter:每个方法向所有子 writer 扇出 ——

func (c comboWriter) Alert(v any) {
	for _, w := range c.writers {
		w.Alert(v)
	}
}

// Close 逐个关闭子 writer,收集全部错误打包返回。
func (c comboWriter) Close() error {
	var be errorx.BatchError
	for _, w := range c.writers {
		be.Add(w.Close())
	}
	return be.Err()
}

func (c comboWriter) Debug(v any, fields ...LogField) {
	for _, w := range c.writers {
		w.Debug(v, fields...)
	}
}

func (c comboWriter) Error(v any, fields ...LogField) {
	for _, w := range c.writers {
		w.Error(v, fields...)
	}
}

func (c comboWriter) Info(v any, fields ...LogField) {
	for _, w := range c.writers {
		w.Info(v, fields...)
	}
}

func (c comboWriter) Severe(v any) {
	for _, w := range c.writers {
		w.Severe(v)
	}
}

func (c comboWriter) Slow(v any, fields ...LogField) {
	for _, w := range c.writers {
		w.Slow(v, fields...)
	}
}

func (c comboWriter) Stack(v any) {
	for _, w := range c.writers {
		w.Stack(v)
	}
}

func (c comboWriter) Stat(v any, fields ...LogField) {
	for _, w := range c.writers {
		w.Stat(v, fields...)
	}
}

// newConsoleWriter 控制台 writer:
// 普通/统计日志走 stdout,error 系走 stderr(便于运维重定向),
// stackLog 用 lessWriter 按配置的冷却毫秒限频,防刷栈。
func newConsoleWriter() Writer {
	outLog := newLogWriter(log.New(fatihcolor.Output, "", flags))
	errLog := newLogWriter(log.New(fatihcolor.Error, "", flags))
	return &concreteWriter{
		infoLog:   outLog,
		errorLog:  errLog,
		severeLog: errLog,
		slowLog:   errLog,
		stackLog:  newLessWriter(errLog, options.logStackCooldownMills),
		statLog:   outLog,
	}
}

// newFileWriter 文件 writer:按 LogConf 装配选项(冷却/压缩/保留天数/
// 备份数/大小上限/轮转规则),在 Path 下创建五个分类文件,
// stack 流复用 errorLog 并限频。
func newFileWriter(c LogConf) (Writer, error) {
	var err error
	var opts []LogOption
	var infoLog io.WriteCloser
	var errorLog io.WriteCloser
	var severeLog io.WriteCloser
	var slowLog io.WriteCloser
	var statLog io.WriteCloser
	var stackLog io.Writer

	if len(c.Path) == 0 {
		return nil, ErrLogPathNotSet
	}

	opts = append(opts, WithCooldownMillis(c.StackCooldownMillis))
	if c.Compress {
		opts = append(opts, WithGzip())
	}
	if c.KeepDays > 0 {
		opts = append(opts, WithKeepDays(c.KeepDays))
	}
	if c.MaxBackups > 0 {
		opts = append(opts, WithMaxBackups(c.MaxBackups))
	}
	if c.MaxSize > 0 {
		opts = append(opts, WithMaxSize(c.MaxSize))
	}

	opts = append(opts, WithRotation(c.Rotation))

	accessFile := path.Join(c.Path, accessFilename)
	errorFile := path.Join(c.Path, errorFilename)
	severeFile := path.Join(c.Path, severeFilename)
	slowFile := path.Join(c.Path, slowFilename)
	statFile := path.Join(c.Path, statFilename)

	handleOptions(opts)

	if infoLog, err = createOutput(accessFile); err != nil {
		return nil, err
	}

	if errorLog, err = createOutput(errorFile); err != nil {
		return nil, err
	}

	if severeLog, err = createOutput(severeFile); err != nil {
		return nil, err
	}

	if slowLog, err = createOutput(slowFile); err != nil {
		return nil, err
	}

	if statLog, err = createOutput(statFile); err != nil {
		return nil, err
	}

	stackLog = newLessWriter(errorLog, options.logStackCooldownMills)

	return &concreteWriter{
		infoLog:   infoLog,
		errorLog:  errorLog,
		severeLog: severeLog,
		slowLog:   slowLog,
		statLog:   statLog,
		stackLog:  stackLog,
	}, nil
}

// —— concreteWriter:各级别分流到对应输出流 ——

// Alert 告警写入 errorLog,级别标为 "alert"。
func (w *concreteWriter) Alert(v any) {
	output(w.errorLog, levelAlert, v)
}

// Close 依次关闭五股流(任一失败立即返回错误)。
func (w *concreteWriter) Close() error {
	if err := w.infoLog.Close(); err != nil {
		return err
	}

	if err := w.errorLog.Close(); err != nil {
		return err
	}

	if err := w.severeLog.Close(); err != nil {
		return err
	}

	if err := w.slowLog.Close(); err != nil {
		return err
	}

	return w.statLog.Close()
}

// Debug 写入 info 流(access),级别 "debug"。
func (w *concreteWriter) Debug(v any, fields ...LogField) {
	output(w.infoLog, levelDebug, v, fields...)
}

// Error 写入 error 流,级别 "error"。
func (w *concreteWriter) Error(v any, fields ...LogField) {
	output(w.errorLog, levelError, v, fields...)
}

// Info 写入 info 流,级别 "info"。
func (w *concreteWriter) Info(v any, fields ...LogField) {
	output(w.infoLog, levelInfo, v, fields...)
}

// Severe 写入 severe 流,级别标为 "fatal"。
func (w *concreteWriter) Severe(v any) {
	output(w.severeLog, levelFatal, v)
}

// Slow 写入 slow 流,级别 "slow"。
func (w *concreteWriter) Slow(v any, fields ...LogField) {
	output(w.slowLog, levelSlow, v, fields...)
}

// Stack 写入 stack 流(实际是限频的 errorLog),级别 "error"。
func (w *concreteWriter) Stack(v any) {
	output(w.stackLog, levelError, v)
}

// Stat 写入 stat 流,级别 "stat"。
func (w *concreteWriter) Stat(v any, fields ...LogField) {
	output(w.statLog, levelStat, v, fields...)
}

// nopWriter 空实现:Disable 后的全局 writer,所有方法静默。
type nopWriter struct{}

func (n nopWriter) Alert(_ any) {
}

func (n nopWriter) Close() error {
	return nil
}

func (n nopWriter) Debug(_ any, _ ...LogField) {
}

func (n nopWriter) Error(_ any, _ ...LogField) {
}

func (n nopWriter) Info(_ any, _ ...LogField) {
}

func (n nopWriter) Severe(_ any) {
}

func (n nopWriter) Slow(_ any, _ ...LogField) {
}

func (n nopWriter) Stack(_ any) {
}

func (n nopWriter) Stat(_ any, _ ...LogField) {
}

// buildPlainFields 把字段 map 转成 "k=v" 字符串数组(plain 编码用)。
// map 遍历顺序随机,但 plain 是人类阅读用途,顺序无所谓。
func buildPlainFields(fields logEntry) []string {
	items := make([]string, 0, len(fields))
	for k, v := range fields {
		items = append(items, fmt.Sprintf("%s=%+v", k, v))
	}

	return items
}

// marshalJson JSON 编码:禁用 HTML 转义(日志要可读,不是嵌网页),
// 并去掉 Encoder 自动追加的结尾换行(换行由 writeJson 统一加)。
func marshalJson(t interface{}) ([]byte, error) {
	var buf bytes.Buffer
	encoder := json.NewEncoder(&buf)
	encoder.SetEscapeHTML(false)
	err := encoder.Encode(t)
	// go 1.5+ will append a newline to the end of the json string
	// https://github.com/golang/go/issues/13520
	if l := buf.Len(); l > 0 && buf.Bytes()[l-1] == '\n' {
		buf.Truncate(l - 1)
	}

	return buf.Bytes(), err
}

// mergeGlobalFields 把进程级全局字段(fields.go)合并到日志字段前部,
// 无全局字段时直接原样返回,避免无谓的内存分配。
func mergeGlobalFields(fields []LogField) []LogField {
	globals := globalFields.Load()
	if globals == nil {
		return fields
	}

	gf := globals.([]LogField)
	ret := make([]LogField, 0, len(gf)+len(fields))
	ret = append(ret, gf...)
	ret = append(ret, fields...)

	return ret
}

// output 是所有日志的最终编码出口(见文件头第三节):
// writer:目标输出流;level:级别字符串;val:日志内容;fields:业务字段。
func output(writer io.Writer, level string, val any, fields ...LogField) {
	switch v := val.(type) {
	case string:
		// only truncate string content, don't know how to truncate the values of other types.
		// 只有字符串能安全截断,其他类型不截。
		maxLen := atomic.LoadUint32(&maxContentLength)
		if maxLen > 0 && len(v) > int(maxLen) {
			val = v[:maxLen]
			fields = append(fields, truncatedField)
		}
	case Sensitive:
		// 整值是敏感类型时整体脱敏。
		val = v.MaskSensitive()
	}

	// +3 for timestamp, level and content
	// 构建字段表:先对每个字段值脱敏,再做类型适配处理。
	entry := make(logEntry, len(fields)+3)
	for _, field := range fields {
		// mask sensitive data before processing types,
		// in case field.Value is a sensitive type and also implemented fmt.Stringer.
		// 先脱敏再查 Stringer,避免脱敏前就把明文打进日志。
		mval := maskSensitive(field.Value)
		entry[field.Key] = processFieldValue(mval)
	}

	switch atomic.LoadUint32(&encoding) {
	case plainEncodingType:
		// plain:时间\t级别(彩色)\t内容\tk=v...
		plainFields := buildPlainFields(entry)
		writePlainAny(writer, level, val, plainFields...)
	default:
		// json:内容、时间、级别一并放进 map 后整体序列化。
		entry[timestampKey] = getTimestamp()
		entry[levelKey] = level
		entry[contentKey] = val
		writeJson(writer, entry)
	}
}

// processFieldValue 对字段值做类型适配,让 JSON 输出更友好:
// error/[]error → 错误文本;Duration/[]Duration、[]Time → 字符串;
// Stringer → String() 文本(防 panic);json.Marshaler 保持原样。
func processFieldValue(value any) any {
	switch val := value.(type) {
	case error:
		return encodeError(val)
	case []error:
		var errs []string
		for _, err := range val {
			errs = append(errs, encodeError(err))
		}
		return errs
	case time.Duration:
		return fmt.Sprint(val)
	case []time.Duration:
		var durs []string
		for _, dur := range val {
			durs = append(durs, fmt.Sprint(dur))
		}
		return durs
	case []time.Time:
		var times []string
		for _, t := range val {
			times = append(times, fmt.Sprint(t))
		}
		return times
	case json.Marshaler:
		return val
	case fmt.Stringer:
		return encodeStringer(val)
	case []fmt.Stringer:
		var strs []string
		for _, str := range val {
			strs = append(strs, encodeStringer(str))
		}
		return strs
	default:
		return val
	}
}

// wrapLevelWithColor 给级别标签上色(仅 plain 编码调用):
// error 系红色、info 蓝色、slow/debug 黄色、stat 绿色。
func wrapLevelWithColor(level string) string {
	var colour color.Color
	switch level {
	case levelAlert:
		colour = color.FgRed
	case levelError:
		colour = color.FgRed
	case levelSevere:
		colour = color.FgRed
	case levelFatal:
		colour = color.FgRed
	case levelInfo:
		colour = color.FgBlue
	case levelSlow:
		colour = color.FgYellow
	case levelDebug:
		colour = color.FgYellow
	case levelStat:
		colour = color.FgGreen
	}

	if colour == color.NoColor {
		return level
	}

	return color.WithColorPadding(level, colour)
}

// writeJson json 编码出口:序列化失败降级到标准 log(带调用栈),
// writer 为 nil 也降级到标准 log;正常时写出一行 JSON。
func writeJson(writer io.Writer, info any) {
	if content, err := marshalJson(info); err != nil {
		log.Printf("err: %s\n\n%s", err.Error(), debug.Stack())
	} else if writer == nil {
		log.Println(string(content))
	} else {
		if _, err := writer.Write(append(content, '\n')); err != nil {
			log.Println(err.Error())
		}
	}
}

// writePlainAny plain 编码的内容分发:
// string/error/Stringer 取文本,其他类型 JSON 序列化兜底。
func writePlainAny(writer io.Writer, level string, val any, fields ...string) {
	level = wrapLevelWithColor(level)

	switch v := val.(type) {
	case string:
		writePlainText(writer, level, v, fields...)
	case error:
		writePlainText(writer, level, v.Error(), fields...)
	case fmt.Stringer:
		writePlainText(writer, level, v.String(), fields...)
	default:
		writePlainValue(writer, level, v, fields...)
	}
}

// writePlainText 输出一行 plain 日志:
// 时间 \t 级别 \t 消息 [\t k=v]...\n
func writePlainText(writer io.Writer, level, msg string, fields ...string) {
	var buf bytes.Buffer
	buf.WriteString(getTimestamp())
	buf.WriteByte(plainEncodingSep)
	buf.WriteString(level)
	buf.WriteByte(plainEncodingSep)
	buf.WriteString(msg)
	for _, item := range fields {
		buf.WriteByte(plainEncodingSep)
		buf.WriteString(item)
	}
	buf.WriteByte('\n')
	if writer == nil {
		log.Println(buf.String())
		return
	}

	if _, err := writer.Write(buf.Bytes()); err != nil {
		log.Println(err.Error())
	}
}

// writePlainValue 输出一行 plain 日志(内容为非文本值时):
// 值部分 JSON 序列化,其余同 writePlainText。
func writePlainValue(writer io.Writer, level string, val any, fields ...string) {
	var buf bytes.Buffer
	buf.WriteString(getTimestamp())
	buf.WriteByte(plainEncodingSep)
	buf.WriteString(level)
	buf.WriteByte(plainEncodingSep)
	if err := json.NewEncoder(&buf).Encode(val); err != nil {
		log.Printf("err: %s\n\n%s", err.Error(), debug.Stack())
		return
	}

	for _, item := range fields {
		buf.WriteByte(plainEncodingSep)
		buf.WriteString(item)
	}
	buf.WriteByte('\n')
	if writer == nil {
		log.Println(buf.String())
		return
	}

	if _, err := writer.Write(buf.Bytes()); err != nil {
		log.Println(err.Error())
	}
}
