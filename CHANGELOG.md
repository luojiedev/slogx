# 更新日志

本项目遵循 [语义化版本](https://semver.org/lang/zh-CN/)。
格式参考 [Keep a Changelog](https://keepachangelog.com/zh-CN/1.1.0/)。

## [未发布]

## [0.3.0] - 2026-09-18

本次新增了与 `context` 的集成、自定义输出目标和 `Format` 类型常量。
**无破坏性 API 变更，但有一处行为变更**（见「变更」一节的轮转默认值）。

### 新增

- **与 `context` 集成**：`ContextWithLogger(ctx, logger)` 与 `FromContext(ctx)`。
  在调用链入口把带请求标识的 Logger 放进 context，下游直接取用，不必层层传 `*Logger`。
  `FromContext` 在 context 中没有 Logger 时返回默认 Logger，**永远不会返回 nil**，
  调用方无需判空；经它取出的 Logger，`source` 仍然指向真正的调用行。
- **`Config.Writer io.Writer`**：额外的输出目标，与 `Filename`、`Stdout` 叠加。
  此前输出目标写死为"文件 + 标准输出"，无法接入内存缓冲（写单测用）、syslog
  或自定义 sink。
- **`Format` 类型与常量** `FormatText` / `FormatJSON`。

### 变更

- `Config.Format` 的类型由 `string` 改为 `Format`（底层仍是 `string`）。
  字面量写法 `Format: "json"` 依然可用，只有传 `string` 变量的代码需要转换类型。
- `Format` 取值现在大小写不敏感并忽略首尾空格；无法识别的取值会回落到文本格式
  **并在 stderr 打印提示**，此前会静默退化成文本格式，写错大小写都察觉不到。
- **`Config` 中未设置的轮转参数现在会填入本库的默认值**（50MB / 100 个 / 30 天）。
  此前 `NewLogger(Config{Filename: "app.log"})` 会把零值透传给 lumberjack，拿到的是
  它自己的默认值——100MB、备份永不删除、不按天数清理——与本库文档声称的默认值不符。
  若此前依赖零值来实现"备份永久保留"，需要显式把 `MaxBackups` 设为一个足够大的值。

## [0.2.1] - 2026-09-18

本次为纯补丁版本，**无破坏性变更**，v0.2.0 可直接升级。

### 修复

- `Log` 和 `LogAttrs` 现在会带上 `source` 属性。这两个方法此前由内嵌的
  `*slog.Logger` 提升上来，输出与其他所有方法不一致。
- `Fatal` 在 `os.Exit` 前关闭日志文件（`os.Exit` 不会执行任何 defer）。

### 性能

- `source` 的调用位置解析结果按调用点 PC 缓存。同一行代码的 PC 固定，解析结果也固定，
  此前每条日志都要重新走一遍 `runtime.CallersFrames`。

  | | ns/op | B/op | allocs/op |
  |---|---|---|---|
  | 0.2.0 | 820 | 312 | 5 |
  | 0.2.1 | 731 | 40 | 2 |
  | 原生 slog（不带 source） | 756 | 40 | 2 |

  单看解析本身：111 ns / 272 B / 3 allocs → 6.3 ns / 0 B / 0 allocs。
  `source` 属性的分配开销现在与不带 `source` 的原生 `slog` 完全持平。

### 新增

- 包级 `Log` 和 `LogAttrs` 函数。
- `doc.go` 包级文档，`pkg.go.dev` 上不再空白；开头即说明"包名是 `log`，需别名导入"。
- 两份 README 新增性能章节。

### 其他

- 新增 GitHub Actions CI：ubuntu/macos/windows 三平台测试、类 Unix 平台竞态检测、
  六个 GOOS/GOARCH 组合交叉编译、gofmt / go vet / go mod tidy / staticcheck、
  以及基准测试冒烟运行。
- 新增基准测试套件（9 个，含原生 `slog` 对照组）。
- 测试覆盖率 77.9% → 94.1%，不再有停留在 0% 的函数。此前包级函数（本库最主要的
  使用方式）完全没有被测试覆盖。

## [0.2.0] - 2026-09-18

修复了 `source` 调用位置定位、goroutine 泄漏和 Windows 无法编译三个核心问题。
**包含破坏性变更。**

### 破坏性变更

- `DefaultLevel` 常量类型由 `string`（`"debug"`）改为 `slog.Level`（`slog.LevelDebug`）。
  原常量与 `Config.Level slog.Level` 类型对不上、无法使用。
- 信号（`SIGHUP`/`SIGUSR1`/`SIGUSR2`）现在只作用于默认 Logger。此前每次 `NewLogger`
  都会注册一次监听，一个信号会改掉进程内所有 Logger 的等级。
- `go.mod` 要求提升到 Go 1.25+。
- 日志文本格式（`source` 位置与 `[file.go:line]` 形式）保持不变，已有的日志采集规则不受影响。

### 修复

- **调用位置（`source`）**：此前只有通过包级函数调用时才正确，其余路径全部错位——
  `logger.Info` 指向 `testing.go`，`With(...).Info` 指向 runtime 汇编，嵌套 `With`
  直接得到空字符串。根因是写死的栈深度加上 `With()` 每次调用都递增 `callerSkip`，
  而 `slog.Logger.With()` 并不增加调用栈层级。现改为从 `runtime.Callers` 取 PC。
- `WithCallerSkip` 的 `skip` 参数此前被完全忽略，现已真正生效。
- **goroutine 泄漏**：`NewLogger` 每次都会起一个永不退出的信号监听 goroutine。
- **Windows 无法编译**：`SIGUSR1`/`SIGUSR2` 在该平台不存在，已按平台拆分文件。
- **数据竞争**：`defaultLogger` 改用 `atomic.Pointer`。
- `init()` 不再 panic：目录创建下放到 `NewLogger`，失败时降级为标准输出并提示。
- 等级被禁用时不再做栈回溯，`Debug()` 在 Info 级别下几乎零开销。
- `LOG_LEVEL` 此前只认 slog 数值，写 `LOG_LEVEL=debug` 会被静默忽略。
- 两份 README 的自定义配置示例此前无法编译（`Level: "debug"`），中文 README 的
  import 也缺少别名。
- handler 写入失败不再被静默丢弃。

### 新增

- `Close()` / `(*Logger).Close()`。
- `ParseLevel(string) (slog.Level, error)`。
- `DebugContext` / `InfoContext` / `WarnContext` / `ErrorContext`（实例级与包级）。
- `WithGroup(name)` 返回 `*Logger`。
- `EnableSignalLevelControl()` / `DisableSignalLevelControl()`。
- 导出常量 `SourceKey`。

## [0.1.6] - 2026-06-10

- 更新 GitHub 用户名为 `luojiedev`。

## [0.1.5] - 2026-04-30

- 新增通过系统信号动态调整日志级别。
- 补充新建 Logger 实例的配置示例。

## [0.1.4] - 2025-11-28

- 关闭 handler 自带的 source 信息。
- README 示例改用 `fmt.Errorf` 记录错误。

## [0.1.3] - 2025-11-28

- `Config.Level` 改用 `slog.Level` 类型。
- 支持记录 time 字段。

## [0.1.2] - 2025-06-10

- Logger 支持记录调用位置，引入 `callerSkip`。
- 新增 `wrappedHandler` 处理日志记录。

## [0.1.1] - 2025-06-03

- 新增 `.gitignore` 与测试。
- Logger 支持调用位置。

## [0.1.0] - 2025-06-03

- 模块由 `MyLog` 更名为 `Slogx`。

[未发布]: https://github.com/luojiedev/slogx/compare/v0.3.0...HEAD
[0.3.0]: https://github.com/luojiedev/slogx/compare/v0.2.1...v0.3.0
[0.2.1]: https://github.com/luojiedev/slogx/compare/v0.2.0...v0.2.1
[0.2.0]: https://github.com/luojiedev/slogx/compare/v0.1.6...v0.2.0
[0.1.6]: https://github.com/luojiedev/slogx/compare/v0.1.5...v0.1.6
[0.1.5]: https://github.com/luojiedev/slogx/compare/v0.1.4...v0.1.5
[0.1.4]: https://github.com/luojiedev/slogx/compare/v0.1.3...v0.1.4
[0.1.3]: https://github.com/luojiedev/slogx/compare/v0.1.2...v0.1.3
[0.1.2]: https://github.com/luojiedev/slogx/compare/v0.1.1...v0.1.2
[0.1.1]: https://github.com/luojiedev/slogx/compare/v0.1.0...v0.1.1
[0.1.0]: https://github.com/luojiedev/slogx/releases/tag/v0.1.0
