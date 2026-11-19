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

func (defaultLogger) DebugContext(ctx context.Context, msg string, args ...any) {
	slog.Default().DebugContext(ctx, msg, args...)
}

func (defaultLogger) InfoContext(ctx context.Context, msg string, args ...any) {
	slog.Default().InfoContext(ctx, msg, args...)
}

func (defaultLogger) WarnContext(ctx context.Context, msg string, args ...any) {
	slog.Default().WarnContext(ctx, msg, args...)
}

func (defaultLogger) ErrorContext(ctx context.Context, msg string, args ...any) {
	slog.Default().ErrorContext(ctx, msg, args...)
}

type discard struct{}

func (discard) DebugContext(context.Context, string, ...any) {}
func (discard) InfoContext(context.Context, string, ...any)  {}
func (discard) WarnContext(context.Context, string, ...any)  {}
func (discard) ErrorContext(context.Context, string, ...any) {}
