//go:build windows

package log

import (
	"log/slog"
	"os"
)

// levelSignals 在 Windows 上没有可用的信号（SIGUSR1/SIGUSR2 不存在），
// 返回空映射表示不启用基于信号的动态调级，请改用 SetLevel。
func levelSignals() map[os.Signal]slog.Level {
	return nil
}
