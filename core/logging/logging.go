// Package logging defines the structured logger goagent writes through.
//
// Logger has the same method set as the context-aware methods of
// *slog.Logger, so a *slog.Logger, or any logger that embeds one such as
// *slogx.Logger from github.com/kart-io/slogx, is passed in directly without
// an adapter. Library code never configures output itself: when no logger is
// given it writes through slog.Default(), which the host configures, for
// example with slogx.Init.
package logging

import (
	"context"
	"log/slog"
	"runtime"
	"time"
)

// Logger is a leveled, structured logger. args are alternating keys and
// values or slog.Attr values, as in slog. Implementations must be safe for
// concurrent use.
type Logger interface {
	DebugContext(ctx context.Context, msg string, args ...any)
	InfoContext(ctx context.Context, msg string, args ...any)
	WarnContext(ctx context.Context, msg string, args ...any)
	ErrorContext(ctx context.Context, msg string, args ...any)
}

// Default returns a Logger that writes through slog.Default() at the time of
// each call, so a later slog.SetDefault (or slogx.Init) also redirects
// loggers obtained earlier.
func Default() Logger { return defaultLogger{} }

// OrDefault returns l, or Default() when l is nil.
func OrDefault(l Logger) Logger {
	if l == nil {
		return Default()
	}
	return l
}

// Discard returns a Logger that drops every record.
func Discard() Logger { return discard{} }

type defaultLogger struct{}

func (d defaultLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	d.log(ctx, slog.LevelDebug, msg, args)
}

func (d defaultLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	d.log(ctx, slog.LevelInfo, msg, args)
}

func (d defaultLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	d.log(ctx, slog.LevelWarn, msg, args)
}

func (d defaultLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	d.log(ctx, slog.LevelError, msg, args)
}

// Handler 让需要自行构造记录的调用方（如 observe 的日志观察者）拿到当前默认处理器。
func (defaultLogger) Handler() slog.Handler { return slog.Default().Handler() }

// log 记录调用方的 PC，否则处理器的 source/caller 恒为本文件。
func (defaultLogger) log(ctx context.Context, level slog.Level, msg string, args []any) {
	if ctx == nil {
		ctx = context.Background()
	}
	h := slog.Default().Handler()
	if !h.Enabled(ctx, level) {
		return
	}
	var pcs [1]uintptr
	runtime.Callers(3, pcs[:]) // 跳过 runtime.Callers、log 与 XxxContext
	r := slog.NewRecord(time.Now(), level, msg, pcs[0])
	r.Add(args...)
	_ = h.Handle(ctx, r)
}

type discard struct{}

func (discard) DebugContext(context.Context, string, ...any) {}
func (discard) InfoContext(context.Context, string, ...any)  {}
func (discard) WarnContext(context.Context, string, ...any)  {}
func (discard) ErrorContext(context.Context, string, ...any) {}
