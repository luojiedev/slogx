// Package log 是一个基于标准库 log/slog 的结构化日志库，在 slog 之上补充了
// 日志轮转、环境感知配置、运行时调级，以及自动、准确的调用位置标注。
//
// # 导入方式
//
// 注意包名是 log 而不是 slogx，与标准库的 log 同名，因此请始终使用别名导入：
//
//	import slogx "github.com/luojiedev/slogx"
//	// 或
//	import log "github.com/luojiedev/slogx"
//
// # 快速开始
//
// 包级函数直接可用，无需初始化：
//
//	slogx.Info("应用启动")
//	slogx.Error("处理失败", "err", err)
//
//	logger := slogx.With("module", "user-service")
//	logger.Info("用户登录", "userId", 123)
//
// 默认写入 ./logs/<程序名>.log，非生产环境同时输出到标准输出。
// 进程退出前调用 Close 关闭日志文件。
//
// # 调用位置
//
// 每条日志都带有 source 属性，形如 [main.go:42]，指向产生该条日志的代码行。
// 经过 With、嵌套 With 以及包级函数调用都保持正确。
//
// 如果在本库之上再封装一层，用 WithCallerSkip 让 source 跳过封装函数本身，
// 指向真正的业务调用点：
//
//	logger := slogx.GetDefaultLogger().WithCallerSkip(1, "layer", "myapp")
//
// source 的解析结果按调用点 PC 缓存，因此该特性几乎不带来额外的内存分配。
//
// # 配置
//
// 全局 Logger 读取以下环境变量：
//
//	LOG_LEVEL        日志级别，debug/info/warn/error，或 slog 数值（-4/0/4/8）
//	LOG_MAX_SIZE     单个日志文件大小上限，单位 MB，默认 50
//	LOG_MAX_BACKUPS  保留的旧日志文件数量，默认 100
//	LOG_MAX_AGE      旧日志文件保留天数，默认 30
//	GO_ENV           production 或 prod 表示生产环境：只写文件并压缩旧文件
//
// 取值非法时回落到默认值，并在标准错误输出一条提示。
// 需要自定义配置时使用 NewLogger 与 Config。
//
// # 运行时调级
//
// 任何 Logger 都可以通过 SetLevel 调整级别。在类 Unix 平台上，本库还会在包初始化时
// 注册信号监听（SIGHUP→Debug、SIGUSR1→Info、SIGUSR2→Warn），该监听只作用于默认
// Logger。若宿主程序自身需要 SIGHUP，请调用 DisableSignalLevelControl 让出该信号。
// Windows 上不注册任何信号。
//
// # 并发
//
// Logger 可以被多个 goroutine 并发使用。SetDefaultLogger 与 GetDefaultLogger
// 之间通过原子指针同步。
package log
