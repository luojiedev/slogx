# Slogx

A structured logging library for Go, built on top of `slog` with automatic log rotation, environment awareness, and flexible configuration.

[中文文档](README_zh.md)

## Features

- Built on Go 1.25+ `slog` package
- Automatic log rotation (powered by lumberjack)
- JSON and text output formats
- Auto-naming log files based on program name
- Environment-aware configuration (test/production)
- Environment variables support
- Console and file output support
- Dynamic log level adjustment: signals on Unix (default logger only) or `SetLevel` (any logger, all platforms)
- Accurate caller location (`source`) that stays correct through `With` / wrappers
- Structured logging with field support

## Installation

```bash
go get github.com/luojiedev/slogx
```

## Quick Start

```go
package main

import (
    "fmt"
    slogx "github.com/luojiedev/slogx"
)

func main() {
    // Flush and close the log file on exit
    defer slogx.Close()

    // Use package-level functions
    slogx.Info("Application started")
    slogx.Debug("Debug information")
    slogx.Error("Error occurred", "error", fmt.Errorf("321"))

    // Use With to add extra fields
    logger := slogx.With("module", "user-service")
    logger.Info("User logged in", "userId", 123)
}
```

> **Note:** the package name is `log`, not `slogx`. Always import it with an alias,
> e.g. `slogx "github.com/luojiedev/slogx"` or `log "github.com/luojiedev/slogx"`.

## Configuration

### Default Settings

- Log file location: `./logs/<program-name>.log`
- Log level: Debug
- Output format: Text
- Single log file size: 50MB
- Number of backup files: 100
- Log retention days: 30 days

### Environment Variables

Configure logging behavior through environment variables:

| Variable | Description | Default |
|----------|-------------|---------|
| LOG_MAX_SIZE | Maximum size of each log file (MB) | 50 |
| LOG_MAX_BACKUPS | Maximum number of old log files | 100 |
| LOG_MAX_AGE | Days to retain old log files | 30 |
| LOG_LEVEL | Log level: `debug` / `info` / `warn` / `error` (case-insensitive), slog offsets like `INFO+2`, or the numeric slog value (`-4`/`0`/`4`/`8`) | debug |
| GO_ENV | Runtime environment (production/prod for production) | - |

Invalid values fall back to the default and print a warning to stderr.

### Environment-Specific Behavior

Test Environment (Default):
- Outputs to both console and file
- No compression for old log files

Production Environment (GO_ENV=production/prod):
- File output only
- Automatic compression for old log files

## Custom Configuration

Use `NewLogger` function for custom configuration:

```go
logger := slogx.NewLogger(slogx.Config{
    Level:      slog.LevelDebug, // slog.Level, not a string
    Format:     "json",
    Filename:   "custom.log",
    MaxSize:    100,    // MB
    MaxBackups: 10,     // number of files
    MaxAge:     7,      // days
    Compress:   true,   // compress old files
    Stdout:     true,   // console output
})

// Set as default logger (optional)
slogx.SetDefaultLogger(logger)

// Close the underlying log file when done
defer logger.Close()
```

If the log directory cannot be created, the logger falls back to stdout and prints a
warning to stderr instead of panicking.

## Caller Location

Every record carries a `source` attribute pointing at the line that produced it. This
stays correct through `With`, nested `With`, and package-level functions.

If you wrap this library in your own helper, use `WithCallerSkip` so `source` points at
your caller rather than at the wrapper:

```go
// inside your own logging helper
logger := slogx.GetDefaultLogger().WithCallerSkip(1, "layer", "myapp")
logger.Info("something happened") // source points at the helper's caller
```

Note that `WithGroup` nests `source` inside the group (`request.source=...`), which is
standard `slog` group behaviour.

## Dynamic Log Level Adjustment

Adjust log levels at runtime using system signals:

- `SIGHUP`: Set to Debug level
- `SIGUSR1`: Set to Info level
- `SIGUSR2`: Set to Warn level

> **Scope: signals only affect the default logger.**
> The signal handler calls `GetDefaultLogger().SetLevel(...)`, so it changes the level of
> whatever logger is current at the moment the signal arrives — including one you
> installed yourself via `SetDefaultLogger`. Loggers you created with `NewLogger` and did
> **not** make the default are *not* affected; adjust those explicitly with
> `logger.SetLevel(...)`. This is intentional: the signal handler is process-wide and
> single-instance, so it has exactly one target.

Registration details:

- The handler is registered **once** at package init, in a single goroutine. `NewLogger`
  does not register anything, so creating many loggers costs no extra goroutines and no
  extra signal subscriptions.
- `EnableSignalLevelControl()` is idempotent — calling it again after
  `DisableSignalLevelControl()` re-arms the handler; calling it repeatedly does nothing.
- Call `DisableSignalLevelControl()` if your application needs `SIGHUP` for its own
  purposes (config reload, for example) — this library claims the signal at import time.
- On Windows no signal is registered (`SIGUSR1`/`SIGUSR2` do not exist there). Use
  `SetLevel` instead.

Example (Unix/Linux):
```bash
# Switch to Debug level
kill -HUP <pid>

# Switch to Info level
kill -USR1 <pid>

# Switch to Warn level
kill -USR2 <pid>
```

## Dependencies

- Go 1.25+
- gopkg.in/natefinch/lumberjack.v2

## License

MIT License

## Contributing

Issues and Pull Requests are welcome!

## Author

[luojiedev](https://github.com/luojiedev)
