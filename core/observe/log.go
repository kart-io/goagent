package observe

import (
	"context"
	"log/slog"
	"strings"

	"github.com/kart-io/goagent/core/logging"
)

// NewLogObserver returns an Observer that writes every event to l as one
// structured record whose message is the event type. Events carry no raw
// inputs, outputs or provider error text, so neither do the records.
//
// Levels: failed executions (error kind "execution" or "panic") are Error;
// other failures, cancellations and model retries are Warn; finished,
// replayed and waiting lifecycles are Info; starts and standalone events
// such as usage and cache hits are Debug. A nil l writes through
// logging.Default().
func NewLogObserver(l logging.Logger) Observer {
	return logObserver{logging.OrDefault(l)}
}

// AddObserver binds o in addition to the observer already bound to ctx, so
// both receive every event; without one it is the same as WithObserver.
func AddObserver(ctx context.Context, o Observer) context.Context {
	if prev, ok := ctx.Value(observerKey{}).(Observer); ok && prev != nil {
		return WithObserver(ctx, tee{prev, o})
	}
	return WithObserver(ctx, o)
}

type tee [2]Observer

// Observe 逐个投递并各自隔离 panic，前一个观察者出错不影响后一个。
func (t tee) Observe(ctx context.Context, e Event) {
	for _, o := range t {
		func() {
			defer func() { _ = recover() }()
			o.Observe(ctx, cloneEvent(e))
		}()
	}
}

type logObserver struct{ l logging.Logger }

// levelByType 按事件类型或其后缀定级，先查完整类型再查后缀，都不匹配时为 Debug。
var (
	levelByType = map[string]slog.Level{
		"model_retry_start": slog.LevelWarn,
	}
	levelBySuffix = map[string]slog.Level{
		"_end":      slog.LevelInfo,
		"_replayed": slog.LevelInfo,
		"_waiting":  slog.LevelInfo,
	}
	// errorLevelByKind 是 *_error 事件按 ErrorKind 的定级；未列出的类别（取消、超时、待处理、缓存降级）为 Warn。
	errorLevelByKind = map[string]slog.Level{
		"execution": slog.LevelError,
		"panic":     slog.LevelError,
	}
)

func eventLevel(e Event) slog.Level {
	if lv, ok := levelByType[e.Type]; ok {
		return lv
	}
	if strings.HasSuffix(e.Type, "_error") {
		if lv, ok := errorLevelByKind[e.ErrorKind]; ok {
			return lv
		}
		return slog.LevelWarn
	}
	if i := strings.LastIndexByte(e.Type, '_'); i >= 0 {
		if lv, ok := levelBySuffix[e.Type[i:]]; ok {
			return lv
		}
	}
	return slog.LevelDebug
}

var logAt = map[slog.Level]func(logging.Logger, context.Context, string, ...any){
	slog.LevelDebug: logging.Logger.DebugContext,
	slog.LevelInfo:  logging.Logger.InfoContext,
	slog.LevelWarn:  logging.Logger.WarnContext,
	slog.LevelError: logging.Logger.ErrorContext,
}

func (o logObserver) Observe(ctx context.Context, e Event) {
	logAt[eventLevel(e)](o.l, ctx, e.Type, eventAttrs(e)...)
}

// eventAttrs 只输出非零字段，避免每条记录带一串空值。
func eventAttrs(e Event) []any {
	attrs := make([]any, 0, 12)
	add := func(a slog.Attr, set bool) {
		if set {
			attrs = append(attrs, a)
		}
	}
	add(slog.String("run_id", e.RunID), e.RunID != "")
	add(slog.String("path", e.Path), e.Path != "")
	add(slog.String("operation_key", e.OperationKey), e.OperationKey != "")
	add(slog.Duration("duration", e.Duration), e.Duration > 0)
	add(slog.String("error_kind", e.ErrorKind), e.ErrorKind != "")
	add(slog.Bool("replayed", true), e.Replayed)
	add(slog.Bool("cached", true), e.Cached)
	add(slog.Uint64("dropped", e.Dropped), e.Dropped > 0)
	add(slog.String("trace_id", e.TraceID), e.TraceID != "")
	add(slog.String("span_id", e.SpanID), e.SpanID != "")
	if a := e.Agent; a != nil {
		add(slog.String("agent_id", a.AgentID), a.AgentID != "")
		add(slog.String("agent_call_key", a.AgentCallKey), a.AgentCallKey != "")
	}
	if u := e.Usage; u != nil {
		attrs = append(attrs, slog.Group(
			"usage",
			slog.String("provider", u.Provider),
			slog.String("model", u.Model),
			slog.Int("attempt", u.Attempt),
			slog.Int("prompt_tokens", u.Tokens.PromptTokens),
			slog.Int("completion_tokens", u.Tokens.CompletionTokens),
			slog.Int("total_tokens", u.Tokens.TotalTokens),
		))
	}
	return attrs
}
