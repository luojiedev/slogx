package main

import (
	"context"
	"log/slog"

	log "github.com/luojiedev/slogx"
)

func logSomething() {
	log.Debug("This is a debug message")
	log.Info("This is an info message")

	// 测试带字段的日志
	log.Info("User logged in", "userId", 123, "ip", "192.168.1.1")
}

// handleRequest 演示从 context 取 Logger：不必层层传参，且 context 中没有时
// FromContext 会回落到默认 Logger，无需判空。
func handleRequest(ctx context.Context) {
	log.FromContext(ctx).Info("handling request", "path", "/api/users")

	// 下游再叠加一层字段，重新放回 context
	ctx = log.ContextWithLogger(ctx, log.FromContext(ctx).With("handler", "user"))
	log.FromContext(ctx).Debug("query finished", "rows", 3)
}

// wrapped 演示 WithCallerSkip：日志里的 source 会指向 wrapped 的调用方，
// 而不是 wrapped 函数本身。
func wrapped(logger *log.Logger, msg string) {
	logger.Info(msg)
}

func main() {
	// 进程退出前关闭日志文件
	defer log.Close()

	// 直接调用包级别的函数
	log.Info("Application started")

	// 在不同的函数中调用
	logSomething()

	// 测试 With 功能（source 依然指向真正的调用行）
	logger := log.With("module", "auth")
	logger.Error("Authentication failed", "reason", "invalid_token")

	// 嵌套 With 也不会让调用位置漂移
	logger.With("sub", "token").Warn("Token about to expire", "ttl", 30)

	// WithGroup 把后续字段归入一个分组
	log.WithGroup("request").Info("Handled", "method", "GET", "status", 200)

	// WithCallerSkip 供二次封装使用：跳过封装函数自身那一层
	skipper := log.GetDefaultLogger().WithCallerSkip(1, "layer", "wrapper")
	wrapped(skipper, "logged through a wrapper")

	// 带 context 的版本
	log.InfoContext(context.Background(), "With context")

	// 把带请求标识的 Logger 放进 context，下游直接取用，不必层层传参
	ctx := log.ContextWithLogger(context.Background(), log.With("trace_id", "abc123"))
	handleRequest(ctx)

	// 测试不同级别的日志
	log.Debug("Debug level message")
	log.Info("Info level message")
	log.Warn("Warning level message")
	log.Error("Error level message")

	// 演示 GetLevel / SetLevel 动态调整日志等级
	currentLevel := log.GetLevel()
	log.Info("Current log level", "level", currentLevel)

	// 将日志等级提升为 INFO，DEBUG 级别日志将不再输出
	log.SetLevel(slog.LevelInfo)
	log.Info("Log level changed to INFO")
	log.Debug("This debug message should NOT appear")
	log.Info("This info message should still appear")

	// 通过 Logger 实例也能获取和设置等级
	logger2 := log.NewLogger(log.Config{
		Level:    slog.LevelWarn,
		Format:   log.FormatText,
		Filename: "",
		Stdout:   true,
	})
	defer logger2.Close()

	log.Info("New logger level", "level", logger2.GetLevel())
	logger2.SetLevel(slog.LevelError)
	logger2.Warn("This warn message should NOT appear")
	logger2.Error("This error message should still appear")

	// 从环境变量解析等级
	if level, err := log.ParseLevel("warn"); err == nil {
		log.Info("Parsed level", "level", level)
	}
}
