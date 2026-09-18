package log

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigWriter(t *testing.T) {
	var buf bytes.Buffer
	logger := NewLogger(Config{Level: slog.LevelDebug, Writer: &buf})
	t.Cleanup(func() { _ = logger.Close() })

	logger.Info("到自定义 writer")

	if !strings.Contains(buf.String(), "到自定义 writer") {
		t.Errorf("Config.Writer 未收到日志:\n%s", buf.String())
	}
}

// TestConfigWriterCombinesWithFile 验证三个输出目标可以叠加。
func TestConfigWriterCombinesWithFile(t *testing.T) {
	var buf bytes.Buffer
	logFile := filepath.Join(t.TempDir(), "combined.log")

	logger := NewLogger(Config{
		Level:    slog.LevelDebug,
		Filename: logFile,
		Writer:   &buf,
	})
	logger.Info("同时写文件和 writer")
	if err := logger.Close(); err != nil {
		t.Fatalf("Close 失败: %v", err)
	}

	content, err := os.ReadFile(logFile)
	if err != nil {
		t.Fatalf("读取日志文件失败: %v", err)
	}
	if !strings.Contains(string(content), "同时写文件和 writer") {
		t.Errorf("文件未收到日志:\n%s", content)
	}
	if !strings.Contains(buf.String(), "同时写文件和 writer") {
		t.Errorf("Writer 未收到日志:\n%s", buf.String())
	}
}

func TestFormatConstants(t *testing.T) {
	cases := []struct {
		name   string
		format Format
		isJSON bool
	}{
		{"FormatJSON", FormatJSON, true},
		{"FormatText", FormatText, false},
		{"零值回落 text", "", false},
		{"大写 JSON 也识别", "JSON", true},
		{"带空格也识别", "  json  ", true},
		{"未知格式回落 text", "yaml", false},
	}

	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			var buf bytes.Buffer
			logger := NewLogger(Config{Level: slog.LevelDebug, Format: c.format, Writer: &buf})
			logger.Info("格式测试", "k", "v")

			output := strings.TrimSpace(buf.String())
			var entry map[string]any
			gotJSON := json.Unmarshal([]byte(output), &entry) == nil

			if gotJSON != c.isJSON {
				t.Errorf("Format=%q 的输出 JSON=%v，期望 %v:\n%s", c.format, gotJSON, c.isJSON, output)
			}
			if !c.isJSON && !strings.Contains(output, "k=v") {
				t.Errorf("Format=%q 期望文本格式:\n%s", c.format, output)
			}
		})
	}
}

// TestRotationDefaults 验证未设置的轮转参数会填入本库的默认值，
// 而不是把零值透传给 lumberjack（那样拿到的是 100MB / 备份永不删 / 不按天清理）。
func TestRotationDefaults(t *testing.T) {
	got := Config{Filename: "app.log"}.withRotationDefaults()

	if got.MaxSize != DefaultMaxSize {
		t.Errorf("MaxSize = %d, 期望 %d", got.MaxSize, DefaultMaxSize)
	}
	if got.MaxBackups != DefaultMaxBackups {
		t.Errorf("MaxBackups = %d, 期望 %d", got.MaxBackups, DefaultMaxBackups)
	}
	if got.MaxAge != DefaultMaxAge {
		t.Errorf("MaxAge = %d, 期望 %d", got.MaxAge, DefaultMaxAge)
	}

	// 显式设置的值不被覆盖。
	explicit := Config{MaxSize: 1, MaxBackups: 2, MaxAge: 3}.withRotationDefaults()
	if explicit.MaxSize != 1 || explicit.MaxBackups != 2 || explicit.MaxAge != 3 {
		t.Errorf("显式设置的值被覆盖: %+v", explicit)
	}

	// 负值同样按未设置处理。
	negative := Config{MaxSize: -1, MaxBackups: -1, MaxAge: -1}.withRotationDefaults()
	if negative.MaxSize != DefaultMaxSize || negative.MaxBackups != DefaultMaxBackups || negative.MaxAge != DefaultMaxAge {
		t.Errorf("负值未按未设置处理: %+v", negative)
	}
}
