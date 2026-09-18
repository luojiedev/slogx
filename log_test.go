package log

import (
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

// newFileLogger 创建一个只写临时文件的 Logger，并返回读取其内容的函数。
func newFileLogger(t *testing.T, cfg Config) (*Logger, func() string) {
	t.Helper()

	if cfg.Filename == "" {
		cfg.Filename = filepath.Join(t.TempDir(), "test.log")
	}
	cfg.Stdout = false

	logger := NewLogger(cfg)
	t.Cleanup(func() { _ = logger.Close() })

	return logger, func() string {
		t.Helper()
		content, err := os.ReadFile(cfg.Filename)
		if err != nil {
			t.Fatalf("读取日志文件失败: %v", err)
		}
		return string(content)
	}
}

// useDefaultLogger 临时替换全局默认 Logger，测试结束后自动恢复，避免污染其他用例。
func useDefaultLogger(t *testing.T, l *Logger) {
	t.Helper()
	old := GetDefaultLogger()
	SetDefaultLogger(l)
	t.Cleanup(func() { SetDefaultLogger(old) })
}

// wantSource 构造期望的 source 属性值。
func wantSource(t *testing.T, line int) string {
	t.Helper()
	_, file, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("无法获取当前测试文件名")
	}
	return fmt.Sprintf("[%s:%d]", filepath.Base(file), line)
}

func TestCallerLocation(t *testing.T) {
	// 替换标准输出为我们的pipe，必须先重定向再创建logger，
	// 因为NewLogger内部通过io.MultiWriter在创建时捕获os.Stdout引用
	oldStdout := os.Stdout
	r, w, _ := os.Pipe()
	os.Stdout = w

	// 在重定向后创建测试logger，这样logger会写入pipe
	testLogger := NewLogger(Config{
		Level:    slog.LevelDebug,
		Format:   "text",
		Filename: "", // 不写文件
		Stdout:   true,
	})

	useDefaultLogger(t, testLogger)

	Debug("test contains time filed", "time", 321)
	Info("test message")

	// 关闭写入端并读取输出
	w.Close()
	output, err := io.ReadAll(r)
	if err != nil {
		t.Fatalf("Failed to read output: %v", err)
	}

	// 恢复标准输出
	os.Stdout = oldStdout

	outputStr := string(output)

	// 验证输出中包含正确的文件名和行号
	if !strings.Contains(outputStr, "log_test.go:") {
		t.Errorf("Expected log output to contain file name 'log_test.go', got: %s", outputStr)
	}

	// 验证输出中不包含日志库内部的文件名
	if strings.Contains(outputStr, "log.go:") {
		t.Errorf("Log output should not contain internal logger file name 'log.go', got: %s", outputStr)
	}
}

func TestCallerLocationInDifferentPackage(t *testing.T) {
	tmpLog, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, tmpLog)

	// 在不同的函数中调用日志
	func() {
		Debug("debug from nested function")
	}()

	if output := read(); !strings.Contains(output, "log_test.go:") {
		t.Errorf("Expected log output to contain file name 'log_test.go', got: %s", output)
	}
}

// TestCallerLocationIsExact 是主要的回归测试：包级函数、Logger 方法、With、
// 嵌套 With 以及 WithGroup 都必须精确指向用户的调用行。
func TestCallerLocationIsExact(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, logger)

	_, _, packageLine, _ := runtime.Caller(0)
	Info("package-level")

	_, _, methodLine, _ := runtime.Caller(0)
	logger.Info("direct method")

	with := logger.With("mod", "a")
	_, _, withLine, _ := runtime.Caller(0)
	with.Info("with")

	nested := with.With("mod2", "b").With("mod3", "c")
	_, _, nestedLine, _ := runtime.Caller(0)
	nested.Info("nested with")

	grouped := logger.WithGroup("g")
	_, _, groupLine, _ := runtime.Caller(0)
	grouped.Info("grouped")

	_, _, ctxLine, _ := runtime.Caller(0)
	logger.InfoContext(t.Context(), "with context")

	output := read()
	cases := []struct {
		name string
		line int
	}{
		{"包级函数", packageLine + 1},
		{"Logger 方法", methodLine + 1},
		{"With", withLine + 1},
		{"嵌套 With", nestedLine + 1},
		{"WithGroup", groupLine + 1},
		{"InfoContext", ctxLine + 1},
	}
	for _, c := range cases {
		if want := wantSource(t, c.line); !strings.Contains(output, want) {
			t.Errorf("%s 的调用位置错误，期望包含 %s，实际输出:\n%s", c.name, want, output)
		}
	}
}

func TestWithCallerSkip(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})

	// 模拟在本库之上再包一层的场景：skip=1 时应跳过 wrapper，指向 wrapper 的调用方。
	wrapper := logger.WithCallerSkip(1, "layer", "wrapper")
	logIt := func(msg string) { wrapper.Info(msg) }

	_, _, callLine, _ := runtime.Caller(0)
	logIt("through wrapper")

	output := read()
	if want := wantSource(t, callLine+1); !strings.Contains(output, want) {
		t.Errorf("WithCallerSkip(1) 应指向 wrapper 的调用方 %s，实际输出:\n%s", want, output)
	}
	if !strings.Contains(output, "layer=wrapper") {
		t.Errorf("WithCallerSkip 传入的属性丢失，实际输出:\n%s", output)
	}
}

func TestWithFieldSource(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, logger)

	fieldLogger := WithField("field", 1)
	_, _, line, _ := runtime.Caller(0)
	fieldLogger.Info("with field")

	output := read()
	if want := wantSource(t, line+1); !strings.Contains(output, want) {
		t.Errorf("WithField 的调用位置错误，期望包含 %s，实际输出:\n%s", want, output)
	}
	if !strings.Contains(output, "field=1") {
		t.Errorf("WithField 的字段丢失，实际输出:\n%s", output)
	}
}

func TestLevelFiltering(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelWarn, Format: "text"})

	logger.Debug("debug should be dropped")
	logger.Info("info should be dropped")
	logger.Warn("warn should appear")
	logger.Error("error should appear")

	output := read()
	for _, dropped := range []string{"debug should be dropped", "info should be dropped"} {
		if strings.Contains(output, dropped) {
			t.Errorf("低于配置级别的日志不应输出: %q\n%s", dropped, output)
		}
	}
	for _, kept := range []string{"warn should appear", "error should appear"} {
		if !strings.Contains(output, kept) {
			t.Errorf("不低于配置级别的日志应输出: %q\n%s", kept, output)
		}
	}
}

func TestSetLevelAtRuntime(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})

	if got := logger.GetLevel(); got != slog.LevelDebug {
		t.Fatalf("GetLevel() = %v, 期望 %v", got, slog.LevelDebug)
	}

	logger.Debug("before raising level")
	logger.SetLevel(slog.LevelError)
	if got := logger.GetLevel(); got != slog.LevelError {
		t.Fatalf("SetLevel 后 GetLevel() = %v, 期望 %v", got, slog.LevelError)
	}
	logger.Debug("after raising level")

	// 派生 Logger 与原 Logger 共享等级变量。
	derived := logger.With("k", "v")
	if got := derived.GetLevel(); got != slog.LevelError {
		t.Errorf("派生 Logger 的等级 = %v, 期望与原 Logger 一致 %v", got, slog.LevelError)
	}

	output := read()
	if !strings.Contains(output, "before raising level") {
		t.Errorf("调级前的日志应输出:\n%s", output)
	}
	if strings.Contains(output, "after raising level") {
		t.Errorf("调级后低级别日志不应输出:\n%s", output)
	}
}

func TestJSONFormat(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelInfo, Format: "json"})

	_, _, line, _ := runtime.Caller(0)
	logger.Info("json message", "userId", 123)

	output := strings.TrimSpace(read())
	var entry map[string]any
	if err := json.Unmarshal([]byte(output), &entry); err != nil {
		t.Fatalf("JSON 格式输出无法解析: %v\n%s", err, output)
	}
	if entry["msg"] != "json message" {
		t.Errorf("msg 字段 = %v, 期望 %q", entry["msg"], "json message")
	}
	if entry["level"] != "INFO" {
		t.Errorf("level 字段 = %v, 期望 INFO", entry["level"])
	}
	if entry["userId"] != float64(123) {
		t.Errorf("userId 字段 = %v, 期望 123", entry["userId"])
	}
	if want := wantSource(t, line+1); entry[SourceKey] != want {
		t.Errorf("source 字段 = %v, 期望 %s", entry[SourceKey], want)
	}
	if _, ok := entry["time"]; !ok {
		t.Errorf("缺少 time 字段:\n%s", output)
	}
}

func TestParseLevel(t *testing.T) {
	cases := []struct {
		in      string
		want    slog.Level
		wantErr bool
	}{
		{in: "debug", want: slog.LevelDebug},
		{in: "DEBUG", want: slog.LevelDebug},
		{in: " info ", want: slog.LevelInfo},
		{in: "warn", want: slog.LevelWarn},
		{in: "error", want: slog.LevelError},
		{in: "-4", want: slog.LevelDebug},
		{in: "0", want: slog.LevelInfo},
		{in: "8", want: slog.LevelError},
		{in: "INFO+2", want: slog.LevelInfo + 2},
		{in: "nonsense", wantErr: true},
		{in: "", wantErr: true},
	}
	for _, c := range cases {
		got, err := ParseLevel(c.in)
		if c.wantErr {
			if err == nil {
				t.Errorf("ParseLevel(%q) 期望报错，实际得到 %v", c.in, got)
			}
			continue
		}
		if err != nil {
			t.Errorf("ParseLevel(%q) 意外报错: %v", c.in, err)
			continue
		}
		if got != c.want {
			t.Errorf("ParseLevel(%q) = %v, 期望 %v", c.in, got, c.want)
		}
	}
}

func TestGetEnvLevel(t *testing.T) {
	t.Setenv("LOG_LEVEL", "warn")
	if got := getEnvLevel("LOG_LEVEL", DefaultLevel); got != slog.LevelWarn {
		t.Errorf("getEnvLevel = %v, 期望 %v", got, slog.LevelWarn)
	}

	// 非法值应回落到默认值而不是 panic 或静默变成 0。
	t.Setenv("LOG_LEVEL", "not-a-level")
	if got := getEnvLevel("LOG_LEVEL", slog.LevelError); got != slog.LevelError {
		t.Errorf("非法 LOG_LEVEL 时 getEnvLevel = %v, 期望回落到 %v", got, slog.LevelError)
	}

	t.Setenv("LOG_MAX_SIZE", "7")
	if got := getEnvOrDefault("LOG_MAX_SIZE", DefaultMaxSize); got != 7 {
		t.Errorf("getEnvOrDefault = %v, 期望 7", got)
	}
	t.Setenv("LOG_MAX_SIZE", "seven")
	if got := getEnvOrDefault("LOG_MAX_SIZE", DefaultMaxSize); got != DefaultMaxSize {
		t.Errorf("非法 LOG_MAX_SIZE 时 getEnvOrDefault = %v, 期望回落到 %v", got, DefaultMaxSize)
	}
}

// TestNewLoggerDoesNotLeakGoroutines 回归测试：信号监听 goroutine 只应存在一份，
// 不再随每次 NewLogger 增长。这里不配置 Filename，以免把 lumberjack 自己的
// 后台清理 goroutine 计入。
func TestNewLoggerDoesNotLeakGoroutines(t *testing.T) {
	before := runtime.NumGoroutine()

	for range 20 {
		logger := NewLogger(Config{
			Level:    slog.LevelDebug,
			Format:   "text",
			Filename: "",
			Stdout:   false,
		})
		if err := logger.Close(); err != nil {
			t.Fatalf("Close 失败: %v", err)
		}
	}

	if after := runtime.NumGoroutine(); after > before+1 {
		t.Errorf("创建 20 个 Logger 后 goroutine 数量从 %d 涨到 %d，疑似泄漏", before, after)
	}
}

// TestNewLoggerFallsBackWhenDirUnavailable 回归测试：日志目录不可创建时
// 应降级到标准输出，而不是 panic。
func TestNewLoggerFallsBackWhenDirUnavailable(t *testing.T) {
	blocker := filepath.Join(t.TempDir(), "not-a-dir")
	if err := os.WriteFile(blocker, []byte("x"), 0o600); err != nil {
		t.Fatalf("准备测试文件失败: %v", err)
	}

	logger := NewLogger(Config{
		Level:    slog.LevelDebug,
		Format:   "text",
		Filename: filepath.Join(blocker, "sub", "app.log"),
	})
	if logger == nil {
		t.Fatal("NewLogger 返回 nil")
	}
	logger.Info("仍然可以记录日志")

	if err := logger.Close(); err != nil {
		t.Errorf("降级后 Close 应返回 nil, 实际: %v", err)
	}
}

func TestSignalLevelControlIsIdempotent(t *testing.T) {
	// init 已经注册过一次，重复调用不应再起新的 goroutine。
	before := runtime.NumGoroutine()
	for range 5 {
		EnableSignalLevelControl()
	}
	if after := runtime.NumGoroutine(); after > before+1 {
		t.Errorf("重复 EnableSignalLevelControl 后 goroutine 从 %d 涨到 %d", before, after)
	}
}

func TestCloseWithoutFile(t *testing.T) {
	logger := NewLogger(Config{Level: slog.LevelDebug, Format: "text", Filename: "", Stdout: false})
	if err := logger.Close(); err != nil {
		t.Errorf("未配置文件时 Close 应返回 nil, 实际: %v", err)
	}
}
