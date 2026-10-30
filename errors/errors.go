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

// ErrorCode defines different error categories for agent operations
type ErrorCode string

const (
	// Agent execution errors
	CodeAgentExecution      ErrorCode = "AGENT_EXECUTION"
	CodeAgentValidation     ErrorCode = "AGENT_VALIDATION"
	CodeAgentNotFound       ErrorCode = "AGENT_NOT_FOUND"
	CodeAgentInitialization ErrorCode = "AGENT_INITIALIZATION"

	// Tool errors
	CodeToolExecution      ErrorCode = "TOOL_EXECUTION"
	CodeToolNotFound       ErrorCode = "TOOL_NOT_FOUND"
	CodeToolValidation     ErrorCode = "TOOL_VALIDATION"
	CodeToolTimeout        ErrorCode = "TOOL_TIMEOUT"
	CodeToolRetryExhausted ErrorCode = "TOOL_RETRY_EXHAUSTED"

	// Middleware errors
	CodeMiddlewareExecution  ErrorCode = "MIDDLEWARE_EXECUTION"
	CodeMiddlewareChain      ErrorCode = "MIDDLEWARE_CHAIN"
	CodeMiddlewareValidation ErrorCode = "MIDDLEWARE_VALIDATION"

	// State management errors
	CodeStateLoad       ErrorCode = "STATE_LOAD"
	CodeStateSave       ErrorCode = "STATE_SAVE"
	CodeStateValidation ErrorCode = "STATE_VALIDATION"
	CodeStateCheckpoint ErrorCode = "STATE_CHECKPOINT"

	// Stream processing errors
	CodeStreamRead    ErrorCode = "STREAM_READ"
	CodeStreamWrite   ErrorCode = "STREAM_WRITE"
	CodeStreamTimeout ErrorCode = "STREAM_TIMEOUT"
	CodeStreamClosed  ErrorCode = "STREAM_CLOSED"

	// LLM errors
	CodeLLMRequest   ErrorCode = "LLM_REQUEST"
	CodeLLMResponse  ErrorCode = "LLM_RESPONSE"
	CodeLLMTimeout   ErrorCode = "LLM_TIMEOUT"
	CodeLLMRateLimit ErrorCode = "LLM_RATE_LIMIT"

	// Context errors
	CodeContextCanceled ErrorCode = "CONTEXT_CANCELED"
	CodeContextTimeout  ErrorCode = "CONTEXT_TIMEOUT"

	// General errors
	CodeInvalidInput   ErrorCode = "INVALID_INPUT"
	CodeInvalidConfig  ErrorCode = "INVALID_CONFIG"
	CodeNotImplemented ErrorCode = "NOT_IMPLEMENTED"
	CodeInternal       ErrorCode = "INTERNAL_ERROR"

	// Distributed/Network errors
	CodeDistributedConnection    ErrorCode = "DISTRIBUTED_CONNECTION"
	CodeDistributedSerialization ErrorCode = "DISTRIBUTED_SERIALIZATION"
	CodeDistributedCoordination  ErrorCode = "DISTRIBUTED_COORDINATION"
	CodeDistributedScheduling    ErrorCode = "DISTRIBUTED_SCHEDULING"
	CodeDistributedHeartbeat     ErrorCode = "DISTRIBUTED_HEARTBEAT"
	CodeDistributedRegistry      ErrorCode = "DISTRIBUTED_REGISTRY"

	// Retrieval/RAG errors
	CodeRetrievalSearch    ErrorCode = "RETRIEVAL_SEARCH"
	CodeRetrievalEmbedding ErrorCode = "RETRIEVAL_EMBEDDING"
	CodeDocumentNotFound   ErrorCode = "DOCUMENT_NOT_FOUND"
	CodeVectorDimMismatch  ErrorCode = "VECTOR_DIM_MISMATCH"

	// Planning errors
	CodePlanningFailed      ErrorCode = "PLANNING_FAILED"
	CodePlanValidation      ErrorCode = "PLAN_VALIDATION"
	CodePlanExecutionFailed ErrorCode = "PLAN_EXECUTION_FAILED"
	CodePlanNotFound        ErrorCode = "PLAN_NOT_FOUND"

	// Parser errors
	CodeParserFailed       ErrorCode = "PARSER_FAILED"
	CodeParserInvalidJSON  ErrorCode = "PARSER_INVALID_JSON"
	CodeParserMissingField ErrorCode = "PARSER_MISSING_FIELD"

	// MultiAgent errors
	CodeMultiAgentRegistration ErrorCode = "MULTIAGENT_REGISTRATION"
	CodeMultiAgentConsensus    ErrorCode = "MULTIAGENT_CONSENSUS"
	CodeMultiAgentMessage      ErrorCode = "MULTIAGENT_MESSAGE"

	// Store errors (supplemental)
	CodeStoreConnection    ErrorCode = "STORE_CONNECTION"
	CodeStoreSerialization ErrorCode = "STORE_SERIALIZATION"
	CodeStoreNotFound      ErrorCode = "STORE_NOT_FOUND"

	// Router errors
	CodeRouterNoMatch  ErrorCode = "ROUTER_NO_MATCH"
	CodeRouterFailed   ErrorCode = "ROUTER_FAILED"
	CodeRouterOverload ErrorCode = "ROUTER_OVERLOAD"

	// Plugin/Type system errors
	CodeTypeMismatch  ErrorCode = "TYPE_MISMATCH"
	CodeAlreadyExists ErrorCode = "ALREADY_EXISTS"
	CodeNotFound      ErrorCode = "NOT_FOUND"
	CodeInvalidOutput ErrorCode = "INVALID_OUTPUT"
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

// Error 实现 error 接口。文本会流入日志、事件与模型输入，因此不包含 Context；
// 需要 Context 时使用 GetContext 或结构化日志（LogValue）。
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

// LogValue 实现 slog.LogValuer，以 group 形式输出错误的结构化字段（含 Context），供结构化日志使用。
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
		attrs = append(attrs, slog.String("cause", e.Cause.Error()))
	}

	return slog.GroupValue(attrs...)
}

// MarshalLogObject 实现 zapcore.ObjectMarshaler，输出与 LogValue 相同的字段（含 Context），
// 使 zap 及基于 zap 的日志引擎记录 *AgentError 时不只得到 Error() 文本。
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
		enc.AddString("cause", e.Cause.Error())
	}
	return nil
}

// Format 实现 fmt.Formatter：%v、%s 与 Error() 相同，%q 为其带引号形式；
// %+v 在 Error() 之后附带按键排序的 Context，仅用于本地调试与日志，不应交给模型。
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

// RedactURL 去掉 URL 中的 userinfo、query 与 fragment，用于把地址写入错误 Context 之前脱敏。
//   - 无法按 URL 解析出主机的输入（如 host:port）按同样规则做字符串裁剪；
//   - 逗号分隔的多地址（如 NATS 集群）逐段处理，query/fragment 中逗号之后的片段一并丢弃；
//   - 密码含未转义的 /、?、# 时，scheme 之后到首个空白前最后一个 @ 之前的内容整体丢弃，
//     因此 host:port/path 且路径含 @ 的地址会被过度裁剪；
//   - key=value 形式的 DSN（如 "host=h password=p dbname=d"）只保留 host、port、dbname。
//
// 不处理把凭据放在路径中的 URL（如 Slack webhook、Telegram bot token），这类地址不应写入错误与日志。
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

// RedactURLError 返回把错误链上首个 *url.Error 的 URL 经 RedactURL 脱敏后的错误，用于把底层 HTTP 错误
// （resty、net/http 的错误文本含完整请求 URL 与 query 中的凭据）返回调用方、写入工具输出或日志之前。
// 原错误不被修改；err 为 nil、链上没有 *url.Error 或 URL 无需脱敏时原样返回。
//   - *url.Error 位于顶层：返回其副本，Unwrap 仍得到原底层错误（如 *net.OpError），Timeout() 与
//     errors.Is(context.DeadlineExceeded) 不受影响；
//   - 位于 *AgentError 之下：返回该 *AgentError 的浅拷贝，Cause 递归脱敏，Code 等字段不变；
//   - 位于其他包装之下：返回的错误文本中 URL 被替换，errors.Is 按原错误链判断，
//     errors.As 只能取到脱敏后的 *url.Error 及其底层错误，中间的包装类型不再可见。
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
// err 为 nil 时返回 nil 的 *AgentError，With* 等链式方法对其安全；但不要把结果直接作为 error 返回，
// nil 的 *AgentError 装进 error 接口后不等于 nil。cause 可能为 nil 时先判断再包装。
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

// MarkModelArgument 标记此错误由模型生成的参数不合法引起，Message 可原样交还模型改正，
// 因此 Message 不得包含内部细节。不改变 Error() 文本、错误码与类型。
func (e *AgentError) MarkModelArgument() *AgentError {
	if e == nil {
		return nil
	}
	e.modelArgument = true
	return e
}

// ModelArgumentMessage 返回可交还模型的参数错误消息；未经 MarkModelArgument 标记时第二个值为 false。
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
// err 为 nil 时返回空字符串；不是 *AgentError（也未包装）时返回 CodeInternal。
func GetCode(err error) ErrorCode {
	if err == nil {
		return ""
	}
	var agentErr *AgentError
	if errors.As(err, &agentErr) && agentErr != nil {
		return agentErr.Code
	}
	return CodeInternal
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

// StackFrames 返回错误创建处的调用栈，每次调用时解析。
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
		sb.WriteString(fmt.Sprintf("  %s:%d %s\n", frame.File, frame.Line, frame.Function))
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
