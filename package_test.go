package log

import (
	"errors"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
)

// TestPackageLevelFunctions 覆盖包级入口。这是本库最主要的使用方式，
// 但此前的测试几乎都直接调用 *Logger 的方法，包级函数没有被覆盖到。
func TestPackageLevelFunctions(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, logger)

	ctx := t.Context()

	_, _, line, _ := runtime.Caller(0)
	Debug("pkg debug")
	Info("pkg info")
	Warn("pkg warn")
	Error("pkg error")
	DebugContext(ctx, "pkg debug ctx")
	InfoContext(ctx, "pkg info ctx")
	WarnContext(ctx, "pkg warn ctx")
	ErrorContext(ctx, "pkg error ctx")
	Log(ctx, slog.LevelInfo, "pkg log")
	LogAttrs(ctx, slog.LevelInfo, "pkg log attrs", slog.Int("n", 1))

	output := read()

	wants := []string{
		"pkg debug", "pkg info", "pkg warn", "pkg error",
		"pkg debug ctx", "pkg info ctx", "pkg warn ctx", "pkg error ctx",
		"pkg log", "pkg log attrs",
	}
	for _, want := range wants {
		if !strings.Contains(output, want) {
			t.Errorf("缺少日志 %q:\n%s", want, output)
		}
	}
	if !strings.Contains(output, "n=1") {
		t.Errorf("LogAttrs 的属性丢失:\n%s", output)
	}

	// 每一条包级调用的 source 都应指向本测试文件里对应的那一行。
	for offset := 1; offset <= len(wants); offset++ {
		if want := wantSource(t, line+offset); !strings.Contains(output, want) {
			t.Errorf("包级函数第 %d 条日志的调用位置错误，期望 %s:\n%s", offset, want, output)
		}
	}
}

func TestPackageLevelWithAndLevel(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, logger)

	if got := GetLevel(); got != slog.LevelDebug {
		t.Errorf("GetLevel() = %v, 期望 %v", got, slog.LevelDebug)
	}

	_, _, withLine, _ := runtime.Caller(0)
	With("module", "pkg").Info("via package With")

	_, _, groupLine, _ := runtime.Caller(0)
	WithGroup("req").Info("via package WithGroup", "method", "GET")

	SetLevel(slog.LevelError)
	if got := GetLevel(); got != slog.LevelError {
		t.Errorf("SetLevel 后 GetLevel() = %v, 期望 %v", got, slog.LevelError)
	}
	Info("应当被过滤掉")

	if err := Close(); err != nil {
		t.Errorf("包级 Close() 返回错误: %v", err)
	}

	output := read()
	if !strings.Contains(output, "module=pkg") {
		t.Errorf("包级 With 的属性丢失:\n%s", output)
	}
	if !strings.Contains(output, "req.method=GET") {
		t.Errorf("包级 WithGroup 的分组丢失:\n%s", output)
	}
	if strings.Contains(output, "应当被过滤掉") {
		t.Errorf("包级 SetLevel 未生效:\n%s", output)
	}
	if want := wantSource(t, withLine+1); !strings.Contains(output, want) {
		t.Errorf("包级 With 的调用位置错误，期望 %s:\n%s", want, output)
	}
	if want := wantSource(t, groupLine+1); !strings.Contains(output, want) {
		t.Errorf("包级 WithGroup 的调用位置错误，期望 %s:\n%s", want, output)
	}
}

// TestLogAttrsCarriesSource 回归测试：Log 与 LogAttrs 此前由内嵌的 *slog.Logger
// 提升上来，不带 source，与其他方法输出不一致。
func TestLogAttrsCarriesSource(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	ctx := t.Context()

	_, _, attrsLine, _ := runtime.Caller(0)
	logger.LogAttrs(ctx, slog.LevelInfo, "via LogAttrs", slog.Int("n", 1))

	_, _, logLine, _ := runtime.Caller(0)
	logger.Log(ctx, slog.LevelInfo, "via Log", "n", 2)

	output := read()
	if want := wantSource(t, attrsLine+1); !strings.Contains(output, want) {
		t.Errorf("LogAttrs 缺少正确的 source，期望 %s:\n%s", want, output)
	}
	if want := wantSource(t, logLine+1); !strings.Contains(output, want) {
		t.Errorf("Log 缺少正确的 source，期望 %s:\n%s", want, output)
	}
}

// TestSourceCache 验证缓存命中时结果与直接解析一致。
func TestSourceCache(t *testing.T) {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])

	direct := resolveSource(pcs[0])
	first := formatSource(pcs[0])  // 未命中，填充缓存
	second := formatSource(pcs[0]) // 命中缓存

	if first != direct || second != direct {
		t.Errorf("缓存结果不一致: direct=%q first=%q second=%q", direct, first, second)
	}
	if _, ok := sourceCache.Load(pcs[0]); !ok {
		t.Error("解析后 PC 未被写入缓存")
	}
	if got := formatSource(0); got != "" {
		t.Errorf("formatSource(0) = %q, 期望空字符串", got)
	}
}

// TestConcurrentLoggingAndLevelChange 在 -race 下验证并发写日志、并发换默认
// Logger、并发调级不会触发竞态。
func TestConcurrentLoggingAndLevelChange(t *testing.T) {
	logger, _ := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, logger)

	const goroutines = 16
	var wg sync.WaitGroup

	for i := range goroutines {
		wg.Add(1)
		go func(id int) {
			defer wg.Done()
			for j := range 50 {
				Info("concurrent", "goroutine", id, "i", j)
				logger.With("g", id).Debug("derived")
			}
		}(i)
	}

	wg.Add(1)
	go func() {
		defer wg.Done()
		for j := range 50 {
			if j%2 == 0 {
				SetLevel(slog.LevelInfo)
			} else {
				SetLevel(slog.LevelDebug)
			}
			_ = GetLevel()
			SetDefaultLogger(GetDefaultLogger())
		}
	}()

	wg.Wait()
}

// TestFatalExitsAndLogs 在子进程里验证 Fatal：记录 Error 日志、关闭文件、以退出码 1 结束。
func TestFatalExitsAndLogs(t *testing.T) {
	logFile := os.Getenv("SLOGX_TEST_FATAL_FILE")

	// 子进程分支
	if logFile != "" {
		logger := NewLogger(Config{Level: slog.LevelDebug, Format: "text", Filename: logFile})
		if os.Getenv("SLOGX_TEST_FATAL_METHOD") == "1" {
			logger.Fatal("fatal message", "code", 7)
		} else {
			SetDefaultLogger(logger)
			Fatal("fatal message", "code", 7)
		}
		return // 不应到达
	}

	// 父进程分支
	dir := t.TempDir()
	logFile = filepath.Join(dir, "fatal.log")

	cmd := exec.Command(os.Args[0], "-test.run=^TestFatalExitsAndLogs$")
	cmd.Env = append(os.Environ(),
		"SLOGX_TEST_FATAL_FILE="+logFile,
		"SLOGX_TEST_FATAL_METHOD="+os.Getenv("SLOGX_TEST_FATAL_METHOD"),
	)
	cmd.Dir = dir // 避免子进程在项目目录下创建 logs/

	err := cmd.Run()

	var exitErr *exec.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("Fatal 应以非零状态退出，实际: %v", err)
	}
	if code := exitErr.ExitCode(); code != 1 {
		t.Errorf("退出码 = %d, 期望 1", code)
	}

	content, readErr := os.ReadFile(logFile)
	if readErr != nil {
		t.Fatalf("Fatal 应在退出前把日志写入文件: %v", readErr)
	}
	output := string(content)
	if !strings.Contains(output, "fatal message") {
		t.Errorf("日志内容缺少消息:\n%s", output)
	}
	if !strings.Contains(output, "level=ERROR") {
		t.Errorf("Fatal 应按 ERROR 级别记录:\n%s", output)
	}
	if !strings.Contains(output, "code=7") {
		t.Errorf("Fatal 的属性丢失:\n%s", output)
	}
	if !strings.Contains(output, "package_test.go:") {
		t.Errorf("Fatal 的 source 应指向调用处:\n%s", output)
	}
}

// TestLoggerContextMethods 覆盖 *Logger 上带 context 的方法（包级版本已在别处覆盖）。
func TestLoggerContextMethods(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	ctx := t.Context()

	_, _, line, _ := runtime.Caller(0)
	logger.DebugContext(ctx, "logger debug ctx")
	logger.InfoContext(ctx, "logger info ctx")
	logger.WarnContext(ctx, "logger warn ctx")
	logger.ErrorContext(ctx, "logger error ctx")

	output := read()
	for i, msg := range []string{
		"logger debug ctx", "logger info ctx", "logger warn ctx", "logger error ctx",
	} {
		if !strings.Contains(output, msg) {
			t.Errorf("缺少日志 %q:\n%s", msg, output)
		}
		if want := wantSource(t, line+1+i); !strings.Contains(output, want) {
			t.Errorf("%q 的调用位置错误，期望 %s:\n%s", msg, want, output)
		}
	}
}

// TestSourceHandlerWithAttrsAndGroup 覆盖 WithField 返回的 *slog.Logger 上的
// With / WithGroup，这两条路径走的是 sourceHandler 的包装方法，必须保持包装，
// 否则派生出的 logger 会丢掉 source。
func TestSourceHandlerWithAttrsAndGroup(t *testing.T) {
	logger, read := newFileLogger(t, Config{Level: slog.LevelDebug, Format: "text"})
	useDefaultLogger(t, logger)

	base := WithField("base", "b")

	_, _, attrLine, _ := runtime.Caller(0)
	base.With("extra", "e").Info("derived with attrs")

	_, _, groupLine, _ := runtime.Caller(0)
	base.WithGroup("grp").Info("derived with group", "k", "v")

	output := read()
	if !strings.Contains(output, "extra=e") {
		t.Errorf("With 派生的属性丢失:\n%s", output)
	}
	if !strings.Contains(output, "grp.k=v") {
		t.Errorf("WithGroup 派生的分组丢失:\n%s", output)
	}
	if want := wantSource(t, attrLine+1); !strings.Contains(output, want) {
		t.Errorf("With 派生后丢失 source，期望 %s:\n%s", want, output)
	}
	if want := wantSource(t, groupLine+1); !strings.Contains(output, want) {
		t.Errorf("WithGroup 派生后丢失 source，期望 %s:\n%s", want, output)
	}
}

// TestDisableAndReEnableSignalLevelControl 覆盖注销路径，并确保测试结束后
// 恢复包初始化时的监听状态。
func TestDisableAndReEnableSignalLevelControl(t *testing.T) {
	t.Cleanup(EnableSignalLevelControl)

	DisableSignalLevelControl()
	signalMu.Lock()
	stopped := signalCh == nil
	signalMu.Unlock()
	if !stopped {
		t.Error("DisableSignalLevelControl 后监听应已停止")
	}

	// 重复注销是安全的。
	DisableSignalLevelControl()

	EnableSignalLevelControl()
	signalMu.Lock()
	restarted := signalCh != nil
	signalMu.Unlock()
	if !restarted && len(levelSignals()) > 0 {
		t.Error("EnableSignalLevelControl 后监听应已重新启动")
	}
}

// TestLoggerFatalExits 复用上面的子进程流程，覆盖 (*Logger).Fatal 这条路径。
func TestLoggerFatalExits(t *testing.T) {
	if os.Getenv("SLOGX_TEST_FATAL_FILE") != "" {
		t.Skip("子进程分支由 TestFatalExitsAndLogs 驱动")
	}
	t.Setenv("SLOGX_TEST_FATAL_METHOD", "1")
	TestFatalExitsAndLogs(t)
}
