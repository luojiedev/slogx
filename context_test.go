package log

import (
	"bytes"
	"context"
	"log/slog"
	"runtime"
	"strings"
	"testing"
)

func TestFromContextFallsBackToDefault(t *testing.T) {
	logger, _ := newFileLogger(t, Config{Level: slog.LevelDebug})
	useDefaultLogger(t, logger)

	if got := FromContext(context.Background()); got != logger {
		t.Error("context 中没有 Logger 时，FromContext 应返回默认 Logger")
	}
	//lint:ignore SA1012 刻意传 nil，验证不会 panic
	if got := FromContext(nil); got != logger { //nolint:staticcheck
		t.Error("ctx 为 nil 时，FromContext 应返回默认 Logger 而不是 panic")
	}
}

func TestContextWithLogger(t *testing.T) {
	defaultLog, _ := newFileLogger(t, Config{Level: slog.LevelDebug})
	useDefaultLogger(t, defaultLog)

	var buf bytes.Buffer
	scoped := NewLogger(Config{Level: slog.LevelDebug, Writer: &buf}).With("trace_id", "abc123")

	ctx := ContextWithLogger(context.Background(), scoped)

	if got := FromContext(ctx); got != scoped {
		t.Fatal("FromContext 应返回存入的 Logger")
	}

	// 存入 nil 时原样返回，不覆盖已有的值。
	if got := ContextWithLogger(ctx, nil); got != ctx {
		t.Error("logger 为 nil 时 ContextWithLogger 应原样返回 ctx")
	}

	_, _, line, _ := runtime.Caller(0)
	FromContext(ctx).Info("处理完成", "cost", 12)

	output := buf.String()
	if !strings.Contains(output, "trace_id=abc123") {
		t.Errorf("context 中 Logger 的字段丢失:\n%s", output)
	}
	if !strings.Contains(output, "cost=12") {
		t.Errorf("调用时传入的字段丢失:\n%s", output)
	}
	if want := wantSource(t, line+1); !strings.Contains(output, want) {
		t.Errorf("经由 FromContext 取出的 Logger 调用位置错误，期望 %s:\n%s", want, output)
	}
}

// TestContextLoggerNesting 验证调用链上逐层追加字段的用法。
func TestContextLoggerNesting(t *testing.T) {
	var buf bytes.Buffer
	base := NewLogger(Config{Level: slog.LevelDebug, Writer: &buf})

	ctx := ContextWithLogger(context.Background(), base.With("trace_id", "t1"))
	// 下游再叠加一层字段，重新放回 context
	ctx = ContextWithLogger(ctx, FromContext(ctx).With("handler", "user"))

	FromContext(ctx).Info("nested")

	output := buf.String()
	for _, want := range []string{"trace_id=t1", "handler=user", "nested"} {
		if !strings.Contains(output, want) {
			t.Errorf("缺少 %q:\n%s", want, output)
		}
	}
}

// TestContextWithNilParent 验证 ctx 为 nil 时不会 panic。
func TestContextWithNilParent(t *testing.T) {
	logger, _ := newFileLogger(t, Config{Level: slog.LevelDebug})
	//lint:ignore SA1012 刻意传 nil
	ctx := ContextWithLogger(nil, logger) //nolint:staticcheck
	if got := FromContext(ctx); got != logger {
		t.Error("父 ctx 为 nil 时应回落到 context.Background 并正常存取")
	}
}
