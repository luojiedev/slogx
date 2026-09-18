# Slogx

[![CI](https://github.com/luojiedev/slogx/actions/workflows/ci.yml/badge.svg)](https://github.com/luojiedev/slogx/actions/workflows/ci.yml)
[![Go Reference](https://pkg.go.dev/badge/github.com/luojiedev/slogx.svg)](https://pkg.go.dev/github.com/luojiedev/slogx)
[![Go Report Card](https://goreportcard.com/badge/github.com/luojiedev/slogx)](https://goreportcard.com/report/github.com/luojiedev/slogx)

Slogx 是一个基于 Go 1.25+ 内置的 `slog` 包封装的结构化日志库。它提供了简单易用的接口，同时具备日志轮转、环境感知和灵活配置等特性。

[English](README.md) · [更新日志](CHANGELOG.md)

## 特性

- 基于 Go 1.25+ 的 `slog` 包，支持结构化日志
- 自动日志轮转（基于 lumberjack）
- 支持 JSON 和文本两种输出格式
- 自动使用程序名作为日志文件名
- 环境感知（测试/生产环境自动配置）
- 支持通过环境变量配置
- 支持同时输出到文件和控制台
- 支持动态调整日志级别（类 Unix 用系统信号，仅作用于默认 Logger；全平台可用 SetLevel，作用于任意 Logger）
- 调用位置（source）精确，经过 With / 二次封装也不会错位
- 支持添加额外字段（With 方法）

## 安装

```bash
go get github.com/luojiedev/slogx
```

## 快速开始

```go
package main

import (
    "fmt"

    slogx "github.com/luojiedev/slogx"
)

func main() {
    // 程序退出前关闭日志文件
    defer slogx.Close()

    // 直接使用包级别的函数
    slogx.Info("应用启动")
    slogx.Debug("调试信息")
    slogx.Error("发生错误", "error", fmt.Errorf("boom"))

    // 使用 With 添加额外字段
    logger := slogx.With("module", "user-service")
    logger.Info("用户登录", "userId", 123)
}
```

> **注意：** 本库的包名是 `log` 而不是 `slogx`，导入时必须起别名，
> 例如 `slogx "github.com/luojiedev/slogx"` 或 `log "github.com/luojiedev/slogx"`。

## 配置说明

### 默认配置

- 日志文件位置：`./logs/<程序名>.log`
- 日志级别：Debug
- 输出格式：文本格式
- 单个日志文件大小：50MB
- 保留日志文件数：100
- 日志保留天数：30天

### 环境变量配置

可以通过以下环境变量调整配置：

| 环境变量 | 说明 | 默认值 |
|----------|------|--------|
| LOG_MAX_SIZE | 单个日志文件大小上限(MB) | 50 |
| LOG_MAX_BACKUPS | 保留的日志文件数量 | 100 |
| LOG_MAX_AGE | 日志文件保留天数 | 30 |
| LOG_LEVEL | 日志级别：`debug`/`info`/`warn`/`error`（大小写不敏感），也支持 slog 偏移写法如 `INFO+2`，或 slog 数值（`-4`/`0`/`4`/`8`） | debug |
| GO_ENV | 运行环境(production/prod表示生产环境) | - |

取值非法时会回落到默认值，并在 stderr 打印一条提示。

### 环境相关行为

测试环境（默认）：
- 同时输出到控制台和文件
- 不压缩旧日志文件

生产环境（GO_ENV=production/prod）：
- 只输出到文件
- 自动压缩旧日志文件

## 自定义配置

如果需要自定义配置，可以使用 `NewLogger` 函数：

```go
logger := slogx.NewLogger(slogx.Config{
    Level:      slog.LevelDebug, // 类型是 slog.Level，不是字符串
    Format:     slogx.FormatJSON, // 或 slogx.FormatText（默认）
    Filename:   "custom.log",
    MaxSize:    100,    // MB
    MaxBackups: 10,     // 文件个数
    MaxAge:     7,      // 天数
    Compress:   true,   // 是否压缩
    Stdout:     true,   // 是否输出到控制台
    Writer:     &buf,   // 可选的额外 io.Writer
})

// 设置为默认logger（可选）
slogx.SetDefaultLogger(logger)

// 用完后关闭底层日志文件
defer logger.Close()
```

三个输出目标可以叠加：`Filename`（文件）、`Stdout`（标准输出）和 `Writer`
（任意 `io.Writer`，可用于写单测的内存缓冲、syslog 或自定义 sink）。三者都未配置时
回落到标准输出。

若日志目录无法创建，Logger 会降级为仅输出到标准输出并在 stderr 打印提示，而不会 panic。
`Format` 取值无法识别时会回落到文本格式并打印提示，而不是静默退化。

## 与 context 配合

在调用链入口把带请求标识的 Logger 放进 context，下游直接取用，不必层层传 `*Logger`：

```go
// 在中间件里
ctx = slogx.ContextWithLogger(ctx, slogx.With("trace_id", traceID))

// 在下游任意位置
slogx.FromContext(ctx).Info("请求处理完成", "cost", cost)
```

`FromContext` 在 context 中没有 Logger 时返回默认 Logger，因此**永远不会返回 nil**，
调用方无需判空。需要逐层追加字段时，把追加后的 Logger 重新放回 context：

```go
ctx = slogx.ContextWithLogger(ctx, slogx.FromContext(ctx).With("handler", name))
```

## 调用位置（source）

每条日志都会带上 `source` 属性，指向产生这条日志的代码行。经过 `With`、嵌套 `With`
以及包级函数调用都保持正确。

如果你在本库之上再封装了一层，用 `WithCallerSkip` 让 `source` 指向业务调用点，
而不是你的封装函数：

```go
// 在你自己的日志封装里
logger := slogx.GetDefaultLogger().WithCallerSkip(1, "layer", "myapp")
logger.Info("发生了一些事") // source 指向调用该封装的那一行
```

注意 `WithGroup` 会把 `source` 一并归入分组（形如 `request.source=...`），
这是 `slog` 的分组语义。

## 动态调整日志级别

支持通过系统信号动态调整日志级别：

- `SIGHUP`: 设置为 Debug 级别
- `SIGUSR1`: 设置为 Info 级别
- `SIGUSR2`: 设置为 Warn 级别

> **作用范围：信号只对默认 Logger 生效。**
> 信号处理逻辑调用的是 `GetDefaultLogger().SetLevel(...)`，即信号到达那一刻的当前默认
> Logger（包括你通过 `SetDefaultLogger` 替换进去的那个）。用 `NewLogger` 单独创建、
> 且**没有**设为默认的 Logger **不会**被信号影响，需要单独调用 `logger.SetLevel(...)`。
> 这是有意设计：信号监听是进程级的、全局只有一份，因此只有一个作用目标。

注册细节：

- 监听在包初始化时**只注册一次**，只占用一个 goroutine。`NewLogger` 不再注册任何信号，
  因此创建再多 Logger 也不会增加 goroutine 或重复订阅信号。
- `EnableSignalLevelControl()` 是幂等的——`DisableSignalLevelControl()` 之后再调用可以
  重新开启监听，重复调用则不做任何事。
- 如果你的程序自己要用 `SIGHUP`（比如热加载配置），请调用
  `DisableSignalLevelControl()`——本库在包导入时就会占用该信号。
- Windows 上不注册任何信号（该平台没有 `SIGUSR1`/`SIGUSR2`），请改用 `SetLevel`。

示例（Unix/Linux）：
```bash
# 调整为 Debug 级别
kill -HUP <pid>

# 调整为 Info 级别
kill -USR1 <pid>

# 调整为 Warn 级别
kill -USR2 <pid>
```

## 性能

`source` 的调用位置从记录的 PC 解析，并按调用点缓存，因此相比原生 `slog` 几乎不带来额外
开销。被级别过滤掉的日志不会做栈回溯，也不会组装记录。

```
BenchmarkInfo           731 ns/op    40 B/op   2 allocs/op
BenchmarkLogAttrs       726 ns/op    40 B/op   2 allocs/op
BenchmarkRawSlog        756 ns/op    40 B/op   2 allocs/op   # 原生 slog，不带 source
BenchmarkDisabledDebug  4.4 ns/op     0 B/op   0 allocs/op   # 被级别过滤掉
```

可用 `go test -bench . -benchmem` 自行测量。以上数据来自 Apple M 系列机器、输出到
`io.Discard`，仅供横向对比，不代表绝对值。

## 依赖

- Go 1.25+
- gopkg.in/natefinch/lumberjack.v2

## 许可证

MIT License

## 贡献

欢迎提交 Issue 和 Pull Request！

## 作者

[luojiedev](https://github.com/luojiedev) 
