//go:build !windows

package log

import (
	"log/slog"
	"os"
	"syscall"
	"testing"
	"time"
)

// TestSignalAdjustsDefaultLevel 验证信号确实能调整默认 Logger 的等级，
// 并且作用于当前的默认 Logger（而不是注册时的那一个）。
func TestSignalAdjustsDefaultLevel(t *testing.T) {
	logger, _ := newFileLogger(t, Config{Level: slog.LevelError, Format: "text"})
	useDefaultLogger(t, logger)

	EnableSignalLevelControl() // 幂等，init 已注册

	cases := []struct {
		sig  syscall.Signal
		want slog.Level
	}{
		{syscall.SIGHUP, slog.LevelDebug},
		{syscall.SIGUSR1, slog.LevelInfo},
		{syscall.SIGUSR2, slog.LevelWarn},
	}

	for _, c := range cases {
		if err := syscall.Kill(os.Getpid(), c.sig); err != nil {
			t.Fatalf("发送信号 %v 失败: %v", c.sig, err)
		}
		if !waitForLevel(logger, c.want) {
			t.Errorf("收到 %v 后等级 = %v, 期望 %v", c.sig, logger.GetLevel(), c.want)
		}
	}
}

// waitForLevel 轮询等待等级变为 want，信号投递是异步的。
func waitForLevel(l *Logger, want slog.Level) bool {
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if l.GetLevel() == want {
			return true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return false
}
