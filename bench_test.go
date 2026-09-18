package log

import (
	"context"
	"io"
	"log/slog"
	"runtime"
	"testing"
)

// discardLogger 构造一个写入 io.Discard 的 Logger，用于隔离出日志组装本身的开销。
func discardLogger(level slog.Level, format string) *Logger {
	lv := &slog.LevelVar{}
	lv.Set(level)
	handler := newHandler(format, io.Discard, lv)
	return &Logger{Logger: slog.New(handler), handler: handler, level: lv}
}

func BenchmarkInfo(b *testing.B) {
	logger := discardLogger(slog.LevelDebug, "text")
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("a message", "userId", 123, "ip", "10.0.0.1")
	}
}

func BenchmarkInfoJSON(b *testing.B) {
	logger := discardLogger(slog.LevelDebug, "json")
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("a message", "userId", 123, "ip", "10.0.0.1")
	}
}

// BenchmarkLogAttrs 是分配开销最小的入口，不需要把参数装箱成 any。
func BenchmarkLogAttrs(b *testing.B) {
	logger := discardLogger(slog.LevelDebug, "text")
	ctx := context.Background()
	b.ReportAllocs()
	for b.Loop() {
		logger.LogAttrs(ctx, slog.LevelInfo, "a message",
			slog.Int("userId", 123), slog.String("ip", "10.0.0.1"))
	}
}

// BenchmarkDisabledDebug 验证被级别过滤掉的日志几乎零开销：
// 既不做栈回溯，也不组装记录。
func BenchmarkDisabledDebug(b *testing.B) {
	logger := discardLogger(slog.LevelError, "text")
	b.ReportAllocs()
	for b.Loop() {
		logger.Debug("a message", "userId", 123, "ip", "10.0.0.1")
	}
}

func BenchmarkWith(b *testing.B) {
	logger := discardLogger(slog.LevelDebug, "text").With("module", "bench")
	b.ReportAllocs()
	for b.Loop() {
		logger.Info("a message", "userId", 123)
	}
}

// BenchmarkRawSlog 是对照组：不带 source 的原生 slog，用于衡量 source 的成本。
func BenchmarkRawSlog(b *testing.B) {
	logger := discardLogger(slog.LevelDebug, "text")
	b.ReportAllocs()
	for b.Loop() {
		logger.Logger.Info("a message", "userId", 123, "ip", "10.0.0.1")
	}
}

// BenchmarkFormatSource 走 PC 缓存命中路径。
func BenchmarkFormatSource(b *testing.B) {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	_ = formatSource(pcs[0]) // 预热缓存
	b.ReportAllocs()
	for b.Loop() {
		_ = formatSource(pcs[0])
	}
}

// BenchmarkResolveSource 是未缓存时的解析开销，用于对照缓存收益。
func BenchmarkResolveSource(b *testing.B) {
	var pcs [1]uintptr
	runtime.Callers(1, pcs[:])
	b.ReportAllocs()
	for b.Loop() {
		_ = resolveSource(pcs[0])
	}
}

func BenchmarkConcurrentInfo(b *testing.B) {
	logger := discardLogger(slog.LevelDebug, "text")
	b.ReportAllocs()
	b.RunParallel(func(pb *testing.PB) {
		for pb.Next() {
			logger.Info("a message", "userId", 123)
		}
	})
}
