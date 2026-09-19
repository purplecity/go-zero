// ————————————————————————————————————————————————————————————————————————————
// rotatelogger —— 日志文件轮转 —— 文件总结
//
// 一、两个角色
//
//	RotateRule(规则)   决定"何时轮转、备份叫什么名字、哪些文件过期":
//	  ├─ DailyRotateRule     按天轮转(默认):跨天时备份成
//	  │                      "access.log-2026-09-19" 这样的名字;
//	  └─ SizeLimitRotateRule 按大小轮转(内嵌按天):超过 MaxSize 时
//	                         备份成 "access.log-2026-09-19T10:00:00+08:00"
//	                         (RFC3339 时间戳,防同名覆盖),
//	                         同时支持 MaxBackups 个数上限与 KeepDays。
//	RotateLogger(执行器) 异步写文件的 logger:
//	  Write 只是把数据丢进 100 容量的 channel 就返回(调用方几乎零阻塞),
//	  后台 worker goroutine 消费 channel → 检查 ShallRotate →
//	  需要则 rotate()(关旧文件 → rename 成备份名 → 开新文件)→ 写文件。
//
// 二、生命周期细节
//   - postRotate 在新 goroutine 里异步做两件收尾:按需 gzip 压缩
//     刚轮转出的备份、删除过期文件(KeepDays/MaxBackups),不阻塞写入;
//   - Close 用 sync.Once 保证只关一次:关 done 通道 → 等 worker
//     排干 channel 里剩余日志(防丢日志)→ Sync 刷盘 → Close;
//   - initialize 打开已有文件用 O_APPEND 追加并记录当前大小
//     (供 size 规则判断),文件不存在则创建(目录一并 MkdirAll);
//   - CloseOnExec 防止子进程继承日志文件句柄。
//
// 三、并发模型
//
//	写入方(Write)与 worker 之间用 channel 通信,文件句柄/大小只被
//	worker 单线程触碰,天然无锁; Close 后再 Write 会降级打印到
//	标准 log 并返回 ErrLogFileClosed。
//
// ————————————————————————————————————————————————————————————————————————————
package logx

import (
	"compress/gzip"
	"errors"
	"fmt"
	"log"
	"os"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/zeromicro/go-zero/core/fs"
	"github.com/zeromicro/go-zero/core/lang"
)

const (
	// hoursPerDay 一天小时数(计算 KeepDays 边界用)。
	hoursPerDay = 24
	// bufferSize 写入 channel 的缓冲容量:写满前 Write 不阻塞。
	bufferSize = 100
	// defaultDirMode 日志目录权限 rwxr-xr-x。
	defaultDirMode = 0o755
	// defaultFileMode 日志文件权限 rw-------。
	defaultFileMode = 0o600
	// gzipExt 压缩备份文件的扩展名。
	gzipExt = ".gz"
	// megaBytes MB 的字节数(MaxSize 单位换算)。
	megaBytes = 1 << 20
)

var (
	// ErrLogFileClosed is an error that indicates the log file is already closed.
	// logger 关闭后仍有写入时返回此错误(数据已降级打到标准 log)。
	ErrLogFileClosed = errors.New("error: log file closed")

	// fileTimeFormat size 轮转备份名中的时间戳格式,可被配置覆盖。
	fileTimeFormat = time.RFC3339
)

type (
	// A RotateRule interface is used to define the log rotating rules.
	// RotateRule 轮转规则接口,可实现自定义规则。
	RotateRule interface {
		// BackupFileName returns the backup name on rotating.
		// 轮转时备份文件的命名。
		BackupFileName() string
		// MarkRotated marks the rotated time.
		// 轮转完成后更新规则内部的"上次轮转时间"锚点。
		MarkRotated()
		// OutdatedFiles returns the files exceeded keeping days/backups.
		// 计算已过期、应当删除的旧日志文件列表。
		OutdatedFiles() []string
		// ShallRotate checks if the file should be rotated.
		// 写入 size 字节前判断是否需要轮转。
		ShallRotate(size int64) bool
	}

	// A RotateLogger is a Logger that can rotate log files with given rules.
	// RotateLogger 异步写文件并按规则轮转的 logger(io.WriteCloser)。
	RotateLogger struct {
		// filename 当前活动日志文件路径(永远往它写)。
		filename string
		// backup 缓存下一个备份文件名,避免每次轮转重复生成。
		backup string
		// fp 当前活动文件句柄,仅 worker goroutine 触碰。
		fp *os.File
		// channel 写入队列:Write 投递,worker 消费。
		channel chan []byte
		// done 关闭信号:Close 时关闭它通知 worker 退出。
		done chan lang.PlaceholderType
		// rule 轮转规则。
		rule RotateRule
		// compress 是否 gzip 压缩轮转出的备份。
		compress bool
		// can't use threading.RoutineGroup because of cycle import
		// waitGroup 等待 worker 排干队列退出(Close 阻塞点)。
		waitGroup sync.WaitGroup
		// closeOnce 保证 Close 幂等。
		closeOnce sync.Once
		// currentSize 当前文件已写字节数(size 规则的判断依据)。
		currentSize int64
	}

	// A DailyRotateRule is a rule to daily rotate the log files.
	// DailyRotateRule 按天轮转规则。
	DailyRotateRule struct {
		// rotatedTime 上次轮转的日期(判断跨天用)。
		rotatedTime string
		// filename 活动日志文件路径。
		filename string
		// delimiter 主名与日期之间的分隔符("-")。
		delimiter string
		// days 保留天数(<=0 永久保留)。
		days int
		// gzip 备份是否压缩。
		gzip bool
	}

	// SizeLimitRotateRule a rotation rule that makes the log file rotated based on size
	// SizeLimitRotateRule 按大小轮转规则:超过 maxSize 轮转,
	// 同时保留按天过期(maxSize 单位 MB,内部转字节)。
	SizeLimitRotateRule struct {
		DailyRotateRule
		maxSize    int64
		maxBackups int
	}
)

// DefaultRotateRule is a default log rotating rule, currently DailyRotateRule.
// 创建默认的按天轮转规则:days 为保留天数,gzip 决定是否压缩。
func DefaultRotateRule(filename, delimiter string, days int, gzip bool) RotateRule {
	return &DailyRotateRule{
		rotatedTime: getNowDate(),
		filename:    filename,
		delimiter:   delimiter,
		days:        days,
		gzip:        gzip,
	}
}

// BackupFileName returns the backup filename on rotating.
// 备份名:"access.log-2026-09-19"(主名-今天日期)。
func (r *DailyRotateRule) BackupFileName() string {
	return fmt.Sprintf("%s%s%s", r.filename, r.delimiter, getNowDate())
}

// MarkRotated marks the rotated time of r to be the current time.
// 轮转完成后把锚点更新为今天,之后 ShallRotate 不再触发。
func (r *DailyRotateRule) MarkRotated() {
	r.rotatedTime = getNowDate()
}

// OutdatedFiles returns the files that exceeded the keeping days.
// 找出超过保留天数的旧日志:
// 用 Glob 匹配 "主名-*"(或 *.gz),按文件名字典序与边界日期比较
// (备份名日期格式固定,字典序即时间序),早于边界的就是过期文件。
// days <= 0 表示永久保留。
func (r *DailyRotateRule) OutdatedFiles() []string {
	if r.days <= 0 {
		return nil
	}

	var pattern string
	if r.gzip {
		pattern = fmt.Sprintf("%s%s*%s", r.filename, r.delimiter, gzipExt)
	} else {
		pattern = fmt.Sprintf("%s%s*", r.filename, r.delimiter)
	}

	files, err := filepath.Glob(pattern)
	if err != nil {
		Errorf("failed to delete outdated log files, error: %s", err)
		return nil
	}

	var buf strings.Builder
	// 边界文件名 = 主名-keepDays 天前的日期,字典序比较即可。
	boundary := time.Now().Add(-time.Hour * time.Duration(hoursPerDay*r.days)).Format(time.DateOnly)
	buf.WriteString(r.filename)
	buf.WriteString(r.delimiter)
	buf.WriteString(boundary)
	if r.gzip {
		buf.WriteString(gzipExt)
	}
	boundaryFile := buf.String()

	var outdates []string
	for _, file := range files {
		if file < boundaryFile {
			outdates = append(outdates, file)
		}
	}

	return outdates
}

// ShallRotate checks if the file should be rotated.
// 当前日期与锚点不同(跨天)时轮转;size 参数不参与判断。
func (r *DailyRotateRule) ShallRotate(_ int64) bool {
	return len(r.rotatedTime) > 0 && getNowDate() != r.rotatedTime
}

// NewSizeLimitRotateRule returns the rotation rule with size limit
// 创建按大小轮转规则:maxSize 单位 MB,maxBackups 备份个数上限。
func NewSizeLimitRotateRule(filename, delimiter string, days, maxSize, maxBackups int, gzip bool) RotateRule {
	return &SizeLimitRotateRule{
		DailyRotateRule: DailyRotateRule{
			// 锚点用 RFC3339(带时分秒),因为同一天内可能轮转多次。
			rotatedTime: getNowDateInRFC3339Format(),
			filename:    filename,
			delimiter:   delimiter,
			days:        days,
			gzip:        gzip,
		},
		maxSize:    int64(maxSize) * megaBytes,
		maxBackups: maxBackups,
	}
}

// BackupFileName 备份名:"access.log-2026-09-19T10:00:00+08:00"
// (主名-RFC3339 时间戳.原扩展名),同一天多次轮转也不重名。
func (r *SizeLimitRotateRule) BackupFileName() string {
	dir := filepath.Dir(r.filename)
	prefix, ext := r.parseFilename()
	timestamp := getNowDateInRFC3339Format()
	return filepath.Join(dir, fmt.Sprintf("%s%s%s%s", prefix, r.delimiter, timestamp, ext))
}

// MarkRotated 更新锚点为当前 RFC3339 时间。
func (r *SizeLimitRotateRule) MarkRotated() {
	r.rotatedTime = getNowDateInRFC3339Format()
}

// OutdatedFiles 找过期备份,两条删除规则取并集:
//  1. 个数超限:maxBackups > 0 时,最老的(文件名排序)超量部分删除;
//  2. 时间过期:超过 KeepDays 的删除。
//
// 用 map 去重(同一文件可能同时命中两条规则)。
func (r *SizeLimitRotateRule) OutdatedFiles() []string {
	dir := filepath.Dir(r.filename)
	prefix, ext := r.parseFilename()

	var pattern string
	if r.gzip {
		pattern = fmt.Sprintf("%s%s%s%s*%s%s", dir, string(filepath.Separator),
			prefix, r.delimiter, ext, gzipExt)
	} else {
		pattern = fmt.Sprintf("%s%s%s%s*%s", dir, string(filepath.Separator),
			prefix, r.delimiter, ext)
	}

	files, err := filepath.Glob(pattern)
	if err != nil {
		Errorf("failed to delete outdated log files, error: %s", err)
		return nil
	}

	// RFC3339 时间戳文件名排序即时间排序。
	sort.Strings(files)

	outdated := make(map[string]lang.PlaceholderType)

	// test if too many backups
	// 规则一:按个数,最老的超量文件标记删除。
	if r.maxBackups > 0 && len(files) > r.maxBackups {
		for _, f := range files[:len(files)-r.maxBackups] {
			outdated[f] = lang.Placeholder
		}
		files = files[len(files)-r.maxBackups:]
	}

	// test if any too old backups
	// 规则二:按保留天数,早于边界的删除。
	if r.days > 0 {
		boundary := time.Now().Add(-time.Hour * time.Duration(hoursPerDay*r.days)).Format(fileTimeFormat)
		boundaryFile := filepath.Join(dir, fmt.Sprintf("%s%s%s%s", prefix, r.delimiter, boundary, ext))
		if r.gzip {
			boundaryFile += gzipExt
		}
		for _, f := range files {
			if f >= boundaryFile {
				break
			}
			outdated[f] = lang.Placeholder
		}
	}

	result := make([]string, 0, len(outdated))
	for k := range outdated {
		result = append(result, k)
	}
	return result
}

// ShallRotate 预写入后大小超过上限时轮转;maxSize=0 不限。
func (r *SizeLimitRotateRule) ShallRotate(size int64) bool {
	return r.maxSize > 0 && r.maxSize < size
}

// parseFilename 拆出日志主名与扩展名("a.log" → "a", ".log")。
func (r *SizeLimitRotateRule) parseFilename() (prefix, ext string) {
	logName := filepath.Base(r.filename)
	ext = filepath.Ext(r.filename)
	prefix = logName[:len(logName)-len(ext)]
	return
}

// NewLogger returns a RotateLogger with given filename and rule, etc.
// 创建轮转 logger:打开/创建文件,启动后台写 worker。
func NewLogger(filename string, rule RotateRule, compress bool) (*RotateLogger, error) {
	l := &RotateLogger{
		filename: filename,
		channel:  make(chan []byte, bufferSize),
		done:     make(chan lang.PlaceholderType),
		rule:     rule,
		compress: compress,
	}
	if err := l.initialize(); err != nil {
		return nil, err
	}

	l.startWorker()
	return l, nil
}

// Close closes l.
// 关闭:通知 worker 退出并等它排干队列(不丢日志),
// 刷盘后关文件;sync.Once 保证幂等。
func (l *RotateLogger) Close() error {
	var err error

	l.closeOnce.Do(func() {
		close(l.done)
		l.waitGroup.Wait()

		if err = l.fp.Sync(); err != nil {
			return
		}

		err = l.fp.Close()
	})

	return err
}

// Write 把日志投递进队列即返回(异步,几乎零阻塞);
// logger 已关闭时降级打印到标准 log 并返回 ErrLogFileClosed。
func (l *RotateLogger) Write(data []byte) (int, error) {
	select {
	case l.channel <- data:
		return len(data), nil
	case <-l.done:
		log.Println(string(data))
		return 0, ErrLogFileClosed
	}
}

// getBackupFilename 返回待用的备份名(缓存优先)。
func (l *RotateLogger) getBackupFilename() string {
	if len(l.backup) == 0 {
		return l.rule.BackupFileName()
	}

	return l.backup
}

// initialize 打开活动日志文件:
//
//	不存在 → 递归建目录后创建;
//	已存在 → 追加打开,并记录当前大小(size 规则据此判断)。
//
// 最后 CloseOnExec,防止 fork 的子进程继承句柄。
func (l *RotateLogger) initialize() error {
	l.backup = l.rule.BackupFileName()

	if fileInfo, err := os.Stat(l.filename); err != nil {
		basePath := path.Dir(l.filename)
		if _, err = os.Stat(basePath); err != nil {
			if err = os.MkdirAll(basePath, defaultDirMode); err != nil {
				return err
			}
		}

		if l.fp, err = os.Create(l.filename); err != nil {
			return err
		}
	} else {
		if l.fp, err = os.OpenFile(l.filename, os.O_APPEND|os.O_WRONLY, defaultFileMode); err != nil {
			return err
		}

		l.currentSize = fileInfo.Size()
	}

	fs.CloseOnExec(l.fp)

	return nil
}

// maybeCompressFile 按需 gzip 压缩备份文件,失败不影响主流程;
// 文件不存在(可能已被外部处理)则跳过。
func (l *RotateLogger) maybeCompressFile(file string) {
	if !l.compress {
		return
	}

	// 压缩属于收尾工作,任何 panic 都不能带崩 worker。
	defer func() {
		if r := recover(); r != nil {
			ErrorStack(r)
		}
	}()

	if _, err := os.Stat(file); err != nil {
		// file doesn't exist or another error, ignore compression
		return
	}

	compressLogFile(file)
}

// maybeDeleteOutdatedFiles 删除规则判定为过期的文件,
// 单个删除失败仅记日志,不中断。
func (l *RotateLogger) maybeDeleteOutdatedFiles() {
	files := l.rule.OutdatedFiles()
	for _, file := range files {
		if err := os.Remove(file); err != nil {
			Errorf("failed to remove outdated file: %s", file)
		}
	}
}

// postRotate 轮转后的异步收尾(压缩 + 清理过期文件),
// 新 goroutine 执行,不阻塞写主路径。
func (l *RotateLogger) postRotate(file string) {
	go func() {
		// we cannot use threading.GoSafe here, because of import cycle.
		l.maybeCompressFile(file)
		l.maybeDeleteOutdatedFiles()
	}()
}

// rotate 执行轮转:关旧句柄 → 旧文件 rename 成备份名(存在时)→
// 异步收尾 → 生成新备份名 → 创建新的活动文件。
func (l *RotateLogger) rotate() error {
	if l.fp != nil {
		err := l.fp.Close()
		l.fp = nil
		if err != nil {
			return err
		}
	}

	_, err := os.Stat(l.filename)
	if err == nil && len(l.backup) > 0 {
		backupFilename := l.getBackupFilename()
		err = os.Rename(l.filename, backupFilename)
		if err != nil {
			return err
		}

		l.postRotate(backupFilename)
	}

	l.backup = l.rule.BackupFileName()
	if l.fp, err = os.Create(l.filename); err == nil {
		fs.CloseOnExec(l.fp)
	}

	return err
}

// startWorker 启动后台写 goroutine:
// 正常循环消费 channel;收到 done 信号后继续排干剩余日志再退出
// (Close 时避免丢失已投递未落盘的日志)。
func (l *RotateLogger) startWorker() {
	l.waitGroup.Add(1)

	go func() {
		defer l.waitGroup.Done()

		for {
			select {
			case event := <-l.channel:
				l.write(event)
			case <-l.done:
				// avoid losing logs before closing.
				for {
					select {
					case event := <-l.channel:
						l.write(event)
					default:
						return
					}
				}
			}
		}
	}()
}

// write worker 的实际写入:先判断是否需要轮转(写入后大小),
// 轮转成功则更新锚点、大小清零;句柄有效时写文件并累计大小。
func (l *RotateLogger) write(v []byte) {
	if l.rule.ShallRotate(l.currentSize + int64(len(v))) {
		if err := l.rotate(); err != nil {
			log.Println(err)
		} else {
			l.rule.MarkRotated()
			l.currentSize = 0
		}
	}
	if l.fp != nil {
		l.fp.Write(v)
		l.currentSize += int64(len(v))
	}
}

// compressLogFile 压缩单个日志文件,起止均记 Info 日志,失败记 Error。
func compressLogFile(file string) {
	start := time.Now()
	Infof("compressing log file: %s", file)
	if err := gzipFile(file, fileSys); err != nil {
		Errorf("compress error: %s", err)
	} else {
		Infof("compressed log file: %s, took %s", file, time.Since(start))
	}
}

// getNowDate 当前日期(DateOnly,"2026-09-19"),按天轮转用。
func getNowDate() string {
	return time.Now().Format(time.DateOnly)
}

// getNowDateInRFC3339Format 当前 RFC3339 时间,size 轮转备份名用。
func getNowDateInRFC3339Format() string {
	return time.Now().Format(fileTimeFormat)
}

// gzipFile 把 file 压缩成 file+".gz":
// 打开原文件 → 创建 .gz 目标 → gzip 流拷贝 → 关闭校验;
// 只有压缩全程成功才删除原文件(通过 fileSystem 抽象,便于测试)。
func gzipFile(file string, fsys fileSystem) (err error) {
	in, err := fsys.Open(file)
	if err != nil {
		return err
	}
	defer func() {
		if e := fsys.Close(in); e != nil {
			Errorf("failed to close file: %s, error: %v", file, e)
		}
		if err == nil {
			// only remove the original file when compression is successful
			err = fsys.Remove(file)
		}
	}()

	out, err := fsys.Create(fmt.Sprintf("%s%s", file, gzipExt))
	if err != nil {
		return err
	}
	defer func() {
		e := fsys.Close(out)
		if err == nil {
			err = e
		}
	}()

	w := gzip.NewWriter(out)
	if _, err = fsys.Copy(w, in); err != nil {
		// failed to copy, no need to close w
		return err
	}

	// Close gzip writer 刷新压缩流的尾部,这一步不能省。
	return fsys.Close(w)
}
