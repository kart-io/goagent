package middleware

import (
	"context"

	loggercore "github.com/kart-io/logger/core"

	"github.com/kart-io/goagent/core/logging"
)

// kartLogger 让已弃用的 kart-io/logger 选项继续可用；它没有 ctx 参数，ctx 被丢弃。
type kartLogger struct{ l loggercore.Logger }

func fromKartLogger(l loggercore.Logger) logging.Logger {
	if l == nil {
		return nil
	}
	return kartLogger{l}
}

func (k kartLogger) DebugContext(_ context.Context, msg string, args ...any) {
	k.l.Debugw(msg, args...)
}

func (k kartLogger) InfoContext(_ context.Context, msg string, args ...any) { k.l.Infow(msg, args...) }

func (k kartLogger) WarnContext(_ context.Context, msg string, args ...any) { k.l.Warnw(msg, args...) }

func (k kartLogger) ErrorContext(_ context.Context, msg string, args ...any) {
	k.l.Errorw(msg, args...)
}

// pickLogger 优先用新字段，未设置时退回已弃用的 kart-io/logger 字段。
func pickLogger(l logging.Logger, legacy loggercore.Logger) logging.Logger {
	if l != nil {
		return l
	}
	return fromKartLogger(legacy)
}

func (m *CachingMiddleware) log() logging.Logger {
	return pickLogger(m.config.ContextLogger, m.config.Logger)
}

func (m *RateLimitMiddleware) log() logging.Logger {
	return pickLogger(m.config.ContextLogger, m.config.Logger)
}
