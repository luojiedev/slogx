package log

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"gopkg.in/natefinch/lumberjack.v2"
)

const (
	DefaultMaxSize    = 50              // 默认50MB
	DefaultMaxBackups = 100             // 默认保留100个备份
	DefaultMaxAge     = 30              // 默认保留30天
	DefaultLevel      = slog.LevelDebug // 默认日志级别
)

// SourceKey 是记录调用位置的属性名。
const SourceKey = "source"

// callerSkipOffset 是 log() 内部到用户调用点的固定栈深度：
// runtime.Callers -> (*Logger).log -> (*Logger).Debug/Info/... -> 用户代码
const callerSkipOffset = 3

// defaultLogger 使用原子指针保存，保证 SetDefaultLogger 与并发读取之间没有数据竞争。
var defaultLogger atomic.Pointer[Logger]

// getLogFileName 获取日志文件名，去除可能的.exe后缀
func getLogFileName() string {
	execName := filepath.Base(os.Args[0])
	// 去除可能的.exe后缀
	execName = strings.TrimSuffix(execName, ".exe")
	return execName + ".log"
}

// getEnvOrDefault 获取环境变量值，如果不存在或无法解析则返回默认值
func getEnvOrDefault(key string, defaultVal int) int {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	intVal, err := strconv.Atoi(val)
	if err != nil {
		fmt.Fprintf(os.Stderr, "slogx: 环境变量 %s=%q 不是合法整数，使用默认值 %d\n", key, val, defaultVal)
		return defaultVal
	}
	return intVal
}

// ParseLevel 解析日志级别，支持名称（debug/info/warn/error，大小写不敏感，
// 也支持 slog 的偏移写法如 "INFO+2"）以及 slog 的数值表示（debug=-4/info=0/warn=4/error=8）。
func ParseLevel(s string) (slog.Level, error) {
	s = strings.TrimSpace(s)
	var level slog.Level
	if err := level.UnmarshalText([]byte(s)); err == nil {
		return level, nil
	}
	if n, err := strconv.Atoi(s); err == nil {
		return slog.Level(n), nil
	}
	return 0, fmt.Errorf("slogx: 无法解析日志级别 %q", s)
}

// getEnvLevel 从环境变量读取日志级别，解析失败时回落到默认值并提示。
func getEnvLevel(key string, defaultVal slog.Level) slog.Level {
	val := os.Getenv(key)
	if val == "" {
		return defaultVal
	}
	level, err := ParseLevel(val)
	if err != nil {
		fmt.Fprintf(os.Stderr, "%v，使用默认级别 %s\n", err, defaultVal)
		return defaultVal
	}
	return level
}

// isProduction 判断是否为生产环境
func isProduction() bool {
	env := strings.ToLower(os.Getenv("GO_ENV"))
	return env == "prod" || env == "production"
}

func init() {
	// 获取环境变量配置
	maxSize := getEnvOrDefault("LOG_MAX_SIZE", DefaultMaxSize)
	maxBackups := getEnvOrDefault("LOG_MAX_BACKUPS", DefaultMaxBackups)
	maxAge := getEnvOrDefault("LOG_MAX_AGE", DefaultMaxAge)
	logLevel := getEnvLevel("LOG_LEVEL", DefaultLevel)

	// 根据环境设置压缩和标准输出
	isProd := isProduction()
	compress := isProd
	stdout := !isProd

	// 使用默认配置初始化全局logger
	defaultLogger.Store(NewLogger(Config{
		Level:      logLevel,
		Format:     "text",
		Filename:   filepath.Join("logs", getLogFileName()),
		MaxSize:    maxSize,
		MaxBackups: maxBackups,
		MaxAge:     maxAge,
		Compress:   compress,
		Stdout:     stdout,
	}))

	// 注册进程级信号监听（仅一次），用于运行时调整默认 Logger 的等级。
	EnableSignalLevelControl()
}

// Debug 提供包级别的日志函数
func Debug(msg string, args ...any) {
	GetDefaultLogger().log(context.Background(), slog.LevelDebug, msg, args...)
}

func Info(msg string, args ...any) {
	GetDefaultLogger().log(context.Background(), slog.LevelInfo, msg, args...)
}

func Warn(msg string, args ...any) {
	GetDefaultLogger().log(context.Background(), slog.LevelWarn, msg, args...)
}

func Error(msg string, args ...any) {
	GetDefaultLogger().log(context.Background(), slog.LevelError, msg, args...)
}

// Fatal 记录 Error 级别日志后退出进程。退出前会关闭日志文件，
// 因为 os.Exit 不会执行任何 defer。
func Fatal(msg string, args ...any) {
	logger := GetDefaultLogger()
	logger.log(context.Background(), slog.LevelError, msg, args...)
	_ = logger.Close()
	os.Exit(1)
}

// DebugContext 等带 context 的包级函数，行为与不带 context 的版本一致。
func DebugContext(ctx context.Context, msg string, args ...any) {
	GetDefaultLogger().log(ctx, slog.LevelDebug, msg, args...)
}

func InfoContext(ctx context.Context, msg string, args ...any) {
	GetDefaultLogger().log(ctx, slog.LevelInfo, msg, args...)
}

func WarnContext(ctx context.Context, msg string, args ...any) {
	GetDefaultLogger().log(ctx, slog.LevelWarn, msg, args...)
}

func ErrorContext(ctx context.Context, msg string, args ...any) {
	GetDefaultLogger().log(ctx, slog.LevelError, msg, args...)
}

// Log 以指定级别记录日志。
func Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	GetDefaultLogger().log(ctx, level, msg, args...)
}

// LogAttrs 以指定级别记录日志，接收 []slog.Attr，是分配开销最小的入口。
func LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	GetDefaultLogger().logAttrs(ctx, level, msg, attrs...)
}

// With returns a new Logger with the given attributes added to the global logger
func With(args ...any) *Logger {
	return GetDefaultLogger().With(args...)
}

// WithGroup returns a new Logger that starts a group on the global logger.
func WithGroup(name string) *Logger {
	return GetDefaultLogger().WithGroup(name)
}

// SetDefaultLogger allows users to replace the default logger with a custom one
func SetDefaultLogger(l *Logger) {
	if l == nil {
		return
	}
	defaultLogger.Store(l)
}

// GetDefaultLogger returns the current default logger
func GetDefaultLogger() *Logger {
	return defaultLogger.Load()
}

// GetLevel returns the current log level of the default logger.
func GetLevel() slog.Level {
	return GetDefaultLogger().GetLevel()
}

// SetLevel sets the log level of the default logger at runtime.
func SetLevel(level slog.Level) {
	GetDefaultLogger().SetLevel(level)
}

// Close 关闭默认 Logger 持有的日志文件。
func Close() error {
	return GetDefaultLogger().Close()
}

// Format 是日志的输出格式。
type Format string

const (
	// FormatText 以 key=value 的文本形式输出，是零值也是默认值。
	FormatText Format = "text"
	// FormatJSON 以 JSON 对象的形式输出。
	FormatJSON Format = "json"
)

// Config 定义日志库的配置。
//
// 三个输出目标可以叠加：Filename（日志文件）、Stdout（标准输出）、Writer（自定义）。
// 三者都未配置时回落到标准输出。
type Config struct {
	Level      slog.Level // 日志级别，如 slog.LevelDebug
	Format     Format     // 输出格式，FormatText（默认）或 FormatJSON
	Filename   string     // 日志文件路径，为空表示不写文件
	MaxSize    int        // 每个日志文件的最大兆字节数 (MB)
	MaxBackups int        // 保留的旧日志文件的最大数量
	MaxAge     int        // 保留旧日志文件的最大天数
	Compress   bool       // 是否压缩旧日志文件
	Stdout     bool       // 是否同时输出到标准输出
	Writer     io.Writer  // 额外的输出目标，与上面两者叠加；为 nil 表示不启用
}

// Logger 是我们封装的日志器
type Logger struct {
	*slog.Logger
	handler    slog.Handler
	level      *slog.LevelVar
	callerSkip int       // 额外跳过的调用栈层数，供包装本库的上层代码使用
	closer     io.Closer // 底层日志文件，派生出来的 Logger 共享同一个
}

// sourceCache 缓存 PC 到 "[file.go:line]" 的解析结果。
//
// 同一个调用点的 PC 是固定的，解析结果也就固定，而 runtime.CallersFrames 的解析
// 在实测中占了每条日志约 110ns / 272B / 3 次分配。缓存后该项降到约 6ns / 0 分配，
// 使 source 属性的分配开销与不带 source 的原生 slog 持平。
//
// 键的数量以程序中实际出现的日志调用点数量为上界（与代码规模同阶），不会无限增长。
var sourceCache sync.Map // uintptr -> string

// formatSource 把调用点 PC 格式化为 "[file.go:line]"，结果按 PC 缓存。
func formatSource(pc uintptr) string {
	if pc == 0 {
		return ""
	}
	if cached, ok := sourceCache.Load(pc); ok {
		return cached.(string)
	}
	source := resolveSource(pc)
	sourceCache.Store(pc, source)
	return source
}

// resolveSource 真正解析 PC，仅在缓存未命中时调用。
func resolveSource(pc uintptr) string {
	frame, _ := runtime.CallersFrames([]uintptr{pc}).Next()
	if frame.File == "" {
		return ""
	}
	return "[" + filepath.Base(frame.File) + ":" + strconv.Itoa(frame.Line) + "]"
}

// log 是所有日志方法的统一入口：先判级别再取调用栈，避免被丢弃的日志白白付出
// runtime 栈回溯的开销；调用位置来自 Record 的 PC，因此 With/嵌套调用都不会错位。
func (l *Logger) log(ctx context.Context, level slog.Level, msg string, args ...any) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.Logger.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(callerSkipOffset+l.callerSkip, pcs[:])

	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.Add(args...)
	record.AddAttrs(slog.String(SourceKey, formatSource(pcs[0])))

	if err := l.Logger.Handler().Handle(ctx, record); err != nil {
		fmt.Fprintf(os.Stderr, "slogx: 写入日志失败: %v\n", err)
	}
}

// logAttrs 与 log 等价，但接收 []slog.Attr，避免 ...any 的装箱分配，
// 供 LogAttrs 这类对分配敏感的入口使用。
func (l *Logger) logAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	if ctx == nil {
		ctx = context.Background()
	}
	if !l.Logger.Enabled(ctx, level) {
		return
	}

	var pcs [1]uintptr
	runtime.Callers(callerSkipOffset+l.callerSkip, pcs[:])

	record := slog.NewRecord(time.Now(), level, msg, pcs[0])
	record.AddAttrs(attrs...)
	record.AddAttrs(slog.String(SourceKey, formatSource(pcs[0])))

	if err := l.Logger.Handler().Handle(ctx, record); err != nil {
		fmt.Fprintf(os.Stderr, "slogx: 写入日志失败: %v\n", err)
	}
}

// 以下是封装的日志方法，在 slog.Logger 的基础上自动附加调用位置。
func (l *Logger) Debug(msg string, args ...any) {
	l.log(context.Background(), slog.LevelDebug, msg, args...)
}

func (l *Logger) Info(msg string, args ...any) {
	l.log(context.Background(), slog.LevelInfo, msg, args...)
}

func (l *Logger) Warn(msg string, args ...any) {
	l.log(context.Background(), slog.LevelWarn, msg, args...)
}

func (l *Logger) Error(msg string, args ...any) {
	l.log(context.Background(), slog.LevelError, msg, args...)
}

// Fatal 级别，通常在记录后退出程序。
// slog 没有内置 fatal 级别，按 Error 记录后 os.Exit；退出前关闭日志文件，
// 因为 os.Exit 不会执行任何 defer。
func (l *Logger) Fatal(msg string, args ...any) {
	l.log(context.Background(), slog.LevelError, msg, args...)
	_ = l.Close()
	os.Exit(1)
}

func (l *Logger) DebugContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelDebug, msg, args...)
}

func (l *Logger) InfoContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelInfo, msg, args...)
}

func (l *Logger) WarnContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelWarn, msg, args...)
}

func (l *Logger) ErrorContext(ctx context.Context, msg string, args ...any) {
	l.log(ctx, slog.LevelError, msg, args...)
}

// Log 以指定级别记录日志。覆写内嵌的 slog.Logger.Log，否则该方法不会带上 source。
func (l *Logger) Log(ctx context.Context, level slog.Level, msg string, args ...any) {
	l.log(ctx, level, msg, args...)
}

// LogAttrs 以指定级别记录日志，接收 []slog.Attr，是分配开销最小的入口。
// 覆写内嵌的 slog.Logger.LogAttrs，否则该方法不会带上 source。
func (l *Logger) LogAttrs(ctx context.Context, level slog.Level, msg string, attrs ...slog.Attr) {
	l.logAttrs(ctx, level, msg, attrs...)
}

// clone 基于新的 slog.Logger 派生出一个 Logger，共享等级变量与底层文件。
func (l *Logger) clone(inner *slog.Logger, callerSkip int) *Logger {
	return &Logger{
		Logger:     inner,
		handler:    inner.Handler(),
		level:      l.level,
		callerSkip: callerSkip,
		closer:     l.closer,
	}
}

// With 为 Logger 添加额外的属性。注意：With 不会改变调用栈深度，
// 因此这里必须原样保留 callerSkip，否则调用位置会逐层偏移。
func (l *Logger) With(args ...any) *Logger {
	if len(args) == 0 {
		return l
	}
	return l.clone(l.Logger.With(args...), l.callerSkip)
}

// WithGroup 返回一个把后续属性归入指定分组的 Logger。
// 注意：分组生效后 source 属性也会落在该分组下（如 request.source），
// 这是 slog 的分组语义，需要 source 留在顶层时请不要对其使用 WithGroup。
func (l *Logger) WithGroup(name string) *Logger {
	if name == "" {
		return l
	}
	return l.clone(l.Logger.WithGroup(name), l.callerSkip)
}

// WithCallerSkip 返回一个额外向上跳过 skip 层调用栈的 Logger，
// 供在本库之上再做一层封装的代码定位到真正的业务调用点。
func (l *Logger) WithCallerSkip(skip int, args ...any) *Logger {
	inner := l.Logger
	if len(args) > 0 {
		inner = inner.With(args...)
	}
	return l.clone(inner, l.callerSkip+skip)
}

// GetLevel returns the current log level of the logger.
func (l *Logger) GetLevel() slog.Level {
	return l.level.Level()
}

// SetLevel sets the log level of the logger at runtime.
func (l *Logger) SetLevel(level slog.Level) {
	l.level.Set(level)
}

// Close 关闭底层日志文件。派生出的 Logger 共享同一个文件，关闭其中任意一个即可，
// 关闭后不应再继续写入。未配置 Filename 时返回 nil。
func (l *Logger) Close() error {
	if l.closer == nil {
		return nil
	}
	return l.closer.Close()
}

// sourceHandler 包装原有的 handler，为记录补上调用位置。
// 位置直接取自 Record 自带的 PC，无需猜测栈深度。
type sourceHandler struct {
	slog.Handler
}

func (h sourceHandler) Handle(ctx context.Context, r slog.Record) error {
	r = r.Clone()
	r.AddAttrs(slog.String(SourceKey, formatSource(r.PC)))
	return h.Handler.Handle(ctx, r)
}

func (h sourceHandler) WithAttrs(attrs []slog.Attr) slog.Handler {
	return sourceHandler{Handler: h.Handler.WithAttrs(attrs)}
}

func (h sourceHandler) WithGroup(name string) slog.Handler {
	return sourceHandler{Handler: h.Handler.WithGroup(name)}
}

// loggerContextKey 是 Logger 在 context 中的键。用私有的空结构体类型作为键，
// 避免与其他包的 context 键冲突。
type loggerContextKey struct{}

// ContextWithLogger 返回一个携带 logger 的新 context，供调用链下游取用。
// 典型用法是在中间件里把带请求标识的 Logger 放进 context：
//
//	ctx = slogx.ContextWithLogger(ctx, slogx.With("trace_id", traceID))
//
// logger 为 nil 时原样返回 ctx。
func ContextWithLogger(ctx context.Context, logger *Logger) context.Context {
	if ctx == nil {
		ctx = context.Background()
	}
	if logger == nil {
		return ctx
	}
	return context.WithValue(ctx, loggerContextKey{}, logger)
}

// FromContext 取出 ctx 中由 ContextWithLogger 存入的 Logger。
// ctx 中没有（或 ctx 为 nil）时返回默认 Logger，因此返回值永远不为 nil，
// 调用方无需判空：
//
//	slogx.FromContext(ctx).Info("处理完成", "cost", cost)
//
// 返回的 Logger 与直接使用的 Logger 行为一致，source 仍然指向真正的调用行。
func FromContext(ctx context.Context) *Logger {
	if ctx != nil {
		if logger, ok := ctx.Value(loggerContextKey{}).(*Logger); ok && logger != nil {
			return logger
		}
	}
	return GetDefaultLogger()
}

// WithField 基于默认 Logger 创建一个带单个字段的原生 *slog.Logger。
//
// Deprecated: 推荐使用 With，它返回 *Logger，接口更完整。
func WithField(key string, value any) *slog.Logger {
	origLogger := GetDefaultLogger().With(key, value)
	return slog.New(sourceHandler{Handler: origLogger.Handler()})
}

// timeFormat 是日志中 time 字段的格式。
const timeFormat = "2006-01-02 15:04:05.000000"

// newHandler 按格式构造 slog.Handler。
// AddSource 保持关闭：调用位置由 log/logAttrs 以 source 属性形式追加，
// 这样 source 的解析可以走 PC 缓存，且位置固定在属性末尾。
func newHandler(format Format, w io.Writer, level slog.Leveler) slog.Handler {
	opts := &slog.HandlerOptions{
		AddSource: false,
		Level:     level,
		ReplaceAttr: func(groups []string, a slog.Attr) slog.Attr {
			if a.Key == slog.TimeKey && a.Value.Kind() == slog.KindTime {
				return slog.Attr{
					Key:   "time",
					Value: slog.StringValue(a.Value.Time().Format(timeFormat)),
				}
			}
			return a
		},
	}

	switch Format(strings.ToLower(strings.TrimSpace(string(format)))) {
	case FormatJSON:
		return slog.NewJSONHandler(w, opts)
	case FormatText, "":
		return slog.NewTextHandler(w, opts)
	default:
		// 此前未知格式会静默退化成 text，写错大小写都察觉不到。
		fmt.Fprintf(os.Stderr, "slogx: 未知的日志格式 %q，回落到 %q\n", format, FormatText)
		return slog.NewTextHandler(w, opts)
	}
}

// NewLogger 初始化并返回一个 Logger 实例
func NewLogger(cfg Config) *Logger {
	var writers []io.Writer
	var closer io.Closer

	// 配置 lumberjack。目录创建失败时降级为标准输出，而不是让整个进程崩掉。
	if cfg.Filename != "" {
		dir := filepath.Dir(cfg.Filename)
		if err := os.MkdirAll(dir, 0o755); err != nil {
			fmt.Fprintf(os.Stderr, "slogx: 创建日志目录 %s 失败: %v，本次日志仅输出到标准输出\n", dir, err)
			cfg.Filename = ""
			cfg.Stdout = true
		}
	}

	if cfg.Filename != "" {
		lumberjackLogger := &lumberjack.Logger{
			Filename:   cfg.Filename,
			MaxSize:    cfg.MaxSize,
			MaxBackups: cfg.MaxBackups,
			MaxAge:     cfg.MaxAge,
			Compress:   cfg.Compress,
		}
		writers = append(writers, lumberjackLogger)
		closer = lumberjackLogger
	}

	// 是否同时输出到标准输出
	if cfg.Stdout {
		writers = append(writers, os.Stdout)
	}

	// 额外的自定义输出目标
	if cfg.Writer != nil {
		writers = append(writers, cfg.Writer)
	}

	// 如果没有配置任何输出，则默认输出到标准输出
	if len(writers) == 0 {
		writers = append(writers, os.Stdout)
	}

	// 创建一个 MultiWriter 来同时写入多个目标
	multiWriter := io.MultiWriter(writers...)

	// 设置日志级别
	level := &slog.LevelVar{}
	level.Set(cfg.Level)

	handler := newHandler(cfg.Format, multiWriter, level)

	return &Logger{
		Logger:     slog.New(handler),
		handler:    handler,
		level:      level,
		callerSkip: 0,
		closer:     closer,
	}
}

var (
	signalMu sync.Mutex
	signalCh chan os.Signal
)

// EnableSignalLevelControl 注册进程级信号监听，用于在运行时调整日志等级。
//
// 作用范围：只对默认 Logger 生效。处理逻辑取的是信号到达那一刻的 GetDefaultLogger()，
// 因此通过 SetDefaultLogger 换掉默认 Logger 后，信号会作用到新的那个；而用 NewLogger
// 单独创建、未设为默认的 Logger 不受影响，需自行调用 SetLevel。
//
// 包初始化时会自动调用一次，重复调用是幂等的（不会重复注册或重复起 goroutine）。
// 具体支持哪些信号由平台决定，Windows 上为空操作。
// 若宿主程序自身需要使用 SIGHUP，请调用 DisableSignalLevelControl 让出该信号。
func EnableSignalLevelControl() {
	signalMu.Lock()
	defer signalMu.Unlock()

	if signalCh != nil {
		return
	}

	signals := levelSignals()
	if len(signals) == 0 {
		return
	}

	watched := make([]os.Signal, 0, len(signals))
	for sig := range signals {
		watched = append(watched, sig)
	}

	ch := make(chan os.Signal, 1)
	signal.Notify(ch, watched...)
	signalCh = ch

	go func() {
		for sig := range ch {
			level, ok := signals[sig]
			if !ok {
				continue
			}
			logger := GetDefaultLogger()
			logger.SetLevel(level)
			logger.Warn("Log level changed", "signal", sig.String(), "level", level.String())
		}
	}()
}

// DisableSignalLevelControl 注销信号监听并结束对应的 goroutine。
func DisableSignalLevelControl() {
	signalMu.Lock()
	defer signalMu.Unlock()

	if signalCh == nil {
		return
	}
	signal.Stop(signalCh)
	close(signalCh)
	signalCh = nil
}
