// Package errors defines GoAgent's structured error type AgentError, its
// error codes, and the sentinel errors returned by the framework.
package errors

import (
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/url"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"unicode"

	"go.uber.org/zap/zapcore"
)

// AgentError is the structured error type for all agent operations
//
// It provides:
// - Error codes for categorization
// - Context preservation through the error chain
// - Stack trace information for debugging
// - Structured metadata for logging and monitoring
// - Operation-specific context
type AgentError struct {
	// Code categorizes the error type
	Code ErrorCode

	// Message is the human-readable error message
	Message string

	// Operation identifies what was being attempted
	Operation string

	// Component identifies which component raised the error
	Component string

	// Context provides structured metadata about the error
	Context map[string]interface{}

	// Cause is the underlying error (for error chain)
	Cause error

	// pcs 只记录创建处的程序计数器，符号解析推迟到 StackFrames/FormatStack，避免每个错误都付出解析开销
	pcs []uintptr

	modelArgument bool
}

// StackFrame represents a single frame in a stack trace
type StackFrame struct {
	File     string
	Line     int
	Function string
}

// Error implements the error interface. The text flows into logs, events, and
// model input, so it excludes Context; use GetContext or structured logging
// (LogValue) when Context is needed.
func (e *AgentError) Error() string {
	if e == nil {
		return "<nil>"
	}
	var sb strings.Builder

	sb.WriteByte('[')
	sb.WriteString(string(e.Code))
	sb.WriteByte(']')

	if e.Component != "" {
		sb.WriteString(" [")
		sb.WriteString(e.Component)
		sb.WriteByte(']')
	}

	if e.Operation != "" {
		sb.WriteString(" operation=")
		sb.WriteString(e.Operation)
	}

	sb.WriteString(": ")
	sb.WriteString(e.Message)

	if e.Cause != nil {
		// 经 fmt 输出：Cause 可能是值为 nil 的自定义错误指针，fmt 会把其 Error() 的 panic 转成文本
		_, _ = fmt.Fprintf(&sb, ": %v", e.Cause)
	}

	return sb.String()
}

// LogValue implements slog.LogValuer, emitting the error's structured fields
// (including Context) as a group for structured logging.
func (e *AgentError) LogValue() slog.Value {
	if e == nil {
		return slog.StringValue("<nil>")
	}
	attrs := []slog.Attr{slog.String("code", string(e.Code))}
	if e.Component != "" {
		attrs = append(attrs, slog.String("component", e.Component))
	}
	if e.Operation != "" {
		attrs = append(attrs, slog.String("operation", e.Operation))
	}
	attrs = append(attrs, slog.String("message", e.Message))

	if len(e.Context) > 0 {
		keys := e.sortedContextKeys()
		ctxAttrs := make([]any, 0, len(keys))
		for _, k := range keys {
			ctxAttrs = append(ctxAttrs, slog.Any(k, e.Context[k]))
		}
		attrs = append(attrs, slog.Group("context", ctxAttrs...))
	}

	if e.Cause != nil {
		attrs = append(attrs, slog.String("cause", fmt.Sprint(e.Cause)))
	}

	return slog.GroupValue(attrs...)
}

// MarshalLogObject implements zapcore.ObjectMarshaler, emitting the same fields
// as LogValue (including Context), so that zap and zap-based logging engines
// record more than the Error() text for *AgentError.
func (e *AgentError) MarshalLogObject(enc zapcore.ObjectEncoder) error {
	if e == nil {
		return nil
	}
	enc.AddString("code", string(e.Code))
	if e.Component != "" {
		enc.AddString("component", e.Component)
	}
	if e.Operation != "" {
		enc.AddString("operation", e.Operation)
	}
	enc.AddString("message", e.Message)

	if len(e.Context) > 0 {
		keys := e.sortedContextKeys()
		if err := enc.AddObject("context", zapcore.ObjectMarshalerFunc(func(ctxEnc zapcore.ObjectEncoder) error {
			for _, k := range keys {
				if err := ctxEnc.AddReflected(k, e.Context[k]); err != nil {
					return err
				}
			}
			return nil
		})); err != nil {
			return err
		}
	}

	if e.Cause != nil {
		enc.AddString("cause", fmt.Sprint(e.Cause))
	}
	return nil
}

// Format implements fmt.Formatter: %v and %s are the same as Error(), and %q is
// its quoted form; %+v appends Context sorted by key after Error(), for local
// debugging and logs only, and should not be given to the model.
func (e *AgentError) Format(s fmt.State, verb rune) {
	switch verb {
	case 'v':
		_, _ = io.WriteString(s, e.Error())
		if s.Flag('+') && e != nil && len(e.Context) > 0 {
			keys := e.sortedContextKeys()
			pairs := make([]string, 0, len(keys))
			for _, k := range keys {
				pairs = append(pairs, fmt.Sprintf("%s=%v", k, e.Context[k]))
			}
			_, _ = io.WriteString(s, " context: "+strings.Join(pairs, ", "))
		}
	case 's':
		_, _ = io.WriteString(s, e.Error())
	case 'q':
		_, _ = fmt.Fprintf(s, "%q", e.Error())
	default:
		_, _ = fmt.Fprintf(s, "%%!%c(*errors.AgentError=%s)", verb, e.Error())
	}
}

func (e *AgentError) sortedContextKeys() []string {
	keys := make([]string, 0, len(e.Context))
	for k := range e.Context {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// RedactURL strips userinfo, query, and fragment from a URL, for redacting an
// address before writing it into an error Context.
//   - Input whose host cannot be parsed as a URL (such as host:port) is trimmed
//     as a string by the same rules;
//   - comma-separated multiple addresses (such as a NATS cluster) are processed
//     segment by segment, and segments after a comma inside query/fragment are
//     dropped as well;
//   - when the password contains unescaped /, ?, or #, everything after the
//     scheme up to the last @ before the first whitespace is dropped, so a
//     host:port/path address whose path contains @ is over-trimmed;
//   - key=value DSNs (such as "host=h password=p dbname=d") keep only host,
//     port, and dbname.
//
// URLs that carry credentials in the path (such as Slack webhooks or Telegram
// bot tokens) are not handled; such addresses should not be written into
// errors or logs.
func RedactURL(raw string) string {
	if isKeyValueDSN(raw) {
		return redactKeyValueDSN(raw)
	}
	if !strings.Contains(raw, ",") {
		return redactSingleURL(raw)
	}

	parts := strings.Split(raw, ",")
	out := make([]string, 0, len(parts))
	inQuery := false
	for _, part := range parts {
		// 上一段已进入 query/fragment 时，不含 scheme 的后续片段仍属于它
		if inQuery && !strings.Contains(part, "://") {
			continue
		}
		inQuery = strings.ContainsAny(stripUserinfo(part), "?#")
		out = append(out, redactSingleURL(part))
	}
	return strings.Join(out, ",")
}

func redactSingleURL(raw string) string {
	prefix, rest := "", raw
	if i := strings.Index(raw, "://"); i >= 0 {
		prefix, rest = raw[:i+3], raw[i+3:]
	}
	raw = prefix + stripUserinfo(rest)

	if u, err := url.Parse(raw); err == nil && u.Host != "" {
		u.User = nil
		u.RawQuery = ""
		u.ForceQuery = false
		u.Fragment = ""
		u.RawFragment = ""
		return u.String()
	}

	if i := strings.IndexAny(raw, "?#"); i >= 0 {
		raw = raw[:i]
	}
	return raw
}

// stripUserinfo 去掉 scheme 之后部分开头的 userinfo。authority 内有 @ 时去到其中最后一个 @；
// 否则若首个 @ 之前含 :（密码含未转义的 /、?、# 使 authority 被提前截断），去到首个空白前最后一个 @。
func stripUserinfo(rest string) string {
	span := rest
	if i := strings.IndexFunc(rest, unicode.IsSpace); i >= 0 {
		span = rest[:i]
	}
	authority := span
	if i := strings.IndexAny(span, "/?#"); i >= 0 {
		authority = span[:i]
	}
	if i := strings.LastIndex(authority, "@"); i >= 0 {
		return rest[i+1:]
	}
	if at := strings.Index(span, "@"); at >= 0 && strings.Contains(span[:at], ":") {
		return rest[strings.LastIndex(span, "@")+1:]
	}
	return rest
}

// isKeyValueDSN 判断是否为 libpq 风格的 key=value DSN：不含 ://，且首个字段形如 identifier=value。
func isKeyValueDSN(raw string) bool {
	if strings.Contains(raw, "://") {
		return false
	}
	fields := strings.Fields(raw)
	if len(fields) == 0 {
		return false
	}
	key, _, ok := strings.Cut(fields[0], "=")
	if !ok || key == "" {
		return false
	}
	for _, r := range key {
		if r != '_' && !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

func redactKeyValueDSN(raw string) string {
	kept := make([]string, 0, 3)
	for _, field := range strings.Fields(raw) {
		key, _, ok := strings.Cut(field, "=")
		if !ok {
			continue
		}
		switch key {
		case "host", "port", "dbname":
			kept = append(kept, field)
		}
	}
	return strings.Join(kept, " ")
}

// RedactURLError returns an error in which the URL of the first *url.Error in
// the chain is redacted by RedactURL, for use before returning low-level HTTP
// errors to callers or writing them into tool output or logs (resty and
// net/http error text contains the full request URL and credentials in the
// query). The original error is not modified; err is returned unchanged when it
// is nil, the chain has no *url.Error, or the URL needs no redaction.
//   - *url.Error at the top level: a copy is returned; Unwrap still yields the
//     original underlying error (such as *net.OpError), and Timeout() and
//     errors.Is(context.DeadlineExceeded) are unaffected;
//   - under an *AgentError: a shallow copy of that *AgentError is returned with
//     its Cause redacted recursively; Code and other fields are unchanged;
//   - under other wrappers: the URL is replaced in the returned error text,
//     errors.Is checks against the original chain, and errors.As can only reach
//     the redacted *url.Error and its underlying errors; the intermediate
//     wrapper types are no longer visible.
func RedactURLError(err error) error {
	var ue *url.Error
	if !errors.As(err, &ue) {
		return err
	}
	safe := RedactURL(ue.URL)
	if safe == ue.URL {
		return err
	}
	redacted := &url.Error{Op: ue.Op, URL: safe, Err: ue.Err}

	switch top := err.(type) {
	case *url.Error:
		return redacted
	case *AgentError:
		cp := *top
		cp.Cause = RedactURLError(top.Cause)
		return &cp
	}

	msg := strings.ReplaceAll(err.Error(), strconv.Quote(ue.URL), strconv.Quote(safe))
	msg = strings.ReplaceAll(msg, ue.URL, safe)
	return &urlRedactedError{msg: msg, orig: err, redacted: redacted}
}

type urlRedactedError struct {
	msg      string
	orig     error
	redacted *url.Error
}

func (e *urlRedactedError) Error() string { return e.msg }

func (e *urlRedactedError) Unwrap() error { return e.redacted }

func (e *urlRedactedError) Is(target error) bool { return errors.Is(e.orig, target) }

// Unwrap returns the underlying cause for error chain support
func (e *AgentError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

// Is supports error comparison with errors.Is
func (e *AgentError) Is(target error) bool {
	t, ok := target.(*AgentError)
	if !ok || e == nil || t == nil {
		return false
	}
	return e.Code == t.Code
}

// New creates a new AgentError with the given code and message
func New(code ErrorCode, message string) *AgentError {
	return &AgentError{
		Code:    code,
		Message: message,
		pcs:     callers(),
	}
}

// Newf creates a new AgentError with formatted message
func Newf(code ErrorCode, format string, args ...interface{}) *AgentError {
	return &AgentError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		pcs:     callers(),
	}
}

// Wrap wraps an existing error with context.
// When err is nil it returns a nil *AgentError, which With* chained methods
// handle safely; but do not return the result directly as an error, because a
// nil *AgentError stored in an error interface is not nil. When cause may be
// nil, check before wrapping.
func Wrap(err error, code ErrorCode, message string) *AgentError {
	if err == nil {
		return nil
	}

	return &AgentError{
		Code:    code,
		Message: message,
		Cause:   err,
		pcs:     callers(),
	}
}

// Wrapf wraps an existing error with formatted message
func Wrapf(err error, code ErrorCode, format string, args ...interface{}) *AgentError {
	if err == nil {
		return nil
	}

	return &AgentError{
		Code:    code,
		Message: fmt.Sprintf(format, args...),
		Cause:   err,
		pcs:     callers(),
	}
}

// WithOperation sets the operation context
func (e *AgentError) WithOperation(operation string) *AgentError {
	if e == nil {
		return nil
	}
	e.Operation = operation
	return e
}

// WithComponent sets the component context
func (e *AgentError) WithComponent(component string) *AgentError {
	if e == nil {
		return nil
	}
	e.Component = component
	return e
}

// MarkModelArgument marks this error as caused by invalid model-generated
// arguments, so Message can be returned to the model verbatim for correction;
// therefore Message must not contain internal details. It does not change the
// Error() text, error code, or type.
func (e *AgentError) MarkModelArgument() *AgentError {
	if e == nil {
		return nil
	}
	e.modelArgument = true
	return e
}

// ModelArgumentMessage returns the argument error message that can be returned
// to the model; the second value is false if the error was not marked by
// MarkModelArgument.
func (e *AgentError) ModelArgumentMessage() (string, bool) {
	if e == nil {
		return "", false
	}
	return e.Message, e.modelArgument
}

// WithContext adds a single key-value pair to the context
func (e *AgentError) WithContext(key string, value interface{}) *AgentError {
	if e == nil {
		return nil
	}
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	e.Context[key] = value
	return e
}

// WithContextMap adds multiple key-value pairs to the context
func (e *AgentError) WithContextMap(ctx map[string]interface{}) *AgentError {
	if e == nil {
		return nil
	}
	if e.Context == nil {
		e.Context = make(map[string]interface{})
	}
	for k, v := range ctx {
		e.Context[k] = v
	}
	return e
}

// GetCode extracts the error code from any error.
// It returns an empty string when err is nil; it takes the first framework
// error code in error chain order and returns CodeInternal for unknown errors.
func GetCode(err error) ErrorCode {
	if err == nil {
		return ""
	}
	if code, ok := findCode(err); ok {
		return code
	}
	return CodeInternal
}

// 按标准错误树的深度优先顺序逐节点取码，同时保留自定义 As 的类型投影。
func findCode(err error) (ErrorCode, bool) {
	for err != nil {
		if coded, ok := err.(CodedError); ok {
			return coded.ErrorCode(), true
		}
		if projected, ok := err.(interface{ As(any) bool }); ok {
			var coded CodedError
			if projected.As(&coded) && coded != nil {
				return coded.ErrorCode(), true
			}
			var business *AgentError
			if projected.As(&business) && business != nil {
				return business.Code, true
			}
			var signal *SentinelError
			if projected.As(&signal) && signal != nil {
				return signal.ErrorCode(), true
			}
		}
		switch node := err.(type) {
		case interface{ Unwrap() error }:
			err = node.Unwrap()
		case interface{ Unwrap() []error }:
			for _, child := range node.Unwrap() {
				if code, ok := findCode(child); ok {
					return code, true
				}
			}
			return "", false
		default:
			return "", false
		}
	}
	return "", false
}

// GetOperation extracts the operation from any error
func GetOperation(err error) string {
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr != nil {
		return agentErr.Operation
	}
	return ""
}

// GetComponent extracts the component from any error
func GetComponent(err error) string {
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr != nil {
		return agentErr.Component
	}
	return ""
}

// GetContext extracts the context from any error
func GetContext(err error) map[string]interface{} {
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr != nil {
		return agentErr.Context
	}
	return nil
}

// IsCode checks if an error has the specified code
func IsCode(err error, code ErrorCode) bool {
	return GetCode(err) == code
}

// IsAgentError checks if an error is an AgentError
func IsAgentError(err error) bool {
	var agentErr *AgentError
	return errors.As(err, &agentErr)
}

// callers 记录调用 New/Wrap 等构造函数处的调用栈，只能由构造函数直接调用。
func callers() []uintptr {
	const maxDepth = 32
	var pcs [maxDepth]uintptr
	// 跳过 runtime.Callers、callers 与构造函数本身
	n := runtime.Callers(3, pcs[:])
	return append([]uintptr(nil), pcs[:n]...)
}

// StackFrames returns the call stack at the point the error was created,
// resolved on each call.
func (e *AgentError) StackFrames() []StackFrame {
	if e == nil || len(e.pcs) == 0 {
		return nil
	}
	frames := make([]StackFrame, 0, len(e.pcs))
	callersFrames := runtime.CallersFrames(e.pcs)
	for {
		frame, more := callersFrames.Next()
		frames = append(frames, StackFrame{
			File:     frame.File,
			Line:     frame.Line,
			Function: frame.Function,
		})
		if !more {
			break
		}
	}
	return frames
}

// FormatStack formats the stack trace for logging
func (e *AgentError) FormatStack() string {
	frames := e.StackFrames()
	if len(frames) == 0 {
		return ""
	}

	var sb strings.Builder
	sb.WriteString("Stack trace:\n")
	for _, frame := range frames {
		fmt.Fprintf(&sb, "  %s:%d %s\n", frame.File, frame.Line, frame.Function)
	}
	return sb.String()
}

// ErrorChain returns all errors in the error chain
func ErrorChain(err error) []error {
	var chain []error
	for err != nil {
		chain = append(chain, err)
		err = errors.Unwrap(err)
	}
	return chain
}

// RootCause returns the root cause of the error chain
func RootCause(err error) error {
	for {
		unwrapped := errors.Unwrap(err)
		if unwrapped == nil {
			return err
		}
		err = unwrapped
	}
}
