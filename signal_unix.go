//go:build !windows

package log

import (
	"log/slog"
	"os"
	"syscall"
)

// levelSignals 返回类 Unix 平台上用于动态调整日志级别的信号映射。
func levelSignals() map[os.Signal]slog.Level {
	return map[os.Signal]slog.Level{
		syscall.SIGHUP:  slog.LevelDebug,
		syscall.SIGUSR1: slog.LevelInfo,
		syscall.SIGUSR2: slog.LevelWarn,
	}
}
