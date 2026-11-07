package errors

// CodedError 让领域错误显式提供分类，避免把控制信号混入 Unwrap。
type CodedError interface {
	error
	ErrorCode() ErrorCode
}

// SentinelError 是带分类的固定控制信号，文本和分类不可修改。
// 不实现按类别比较的 Is，保持每个对象独立的哨兵身份。
type SentinelError struct {
	code    ErrorCode
	message string
}

func newSentinel(code ErrorCode, message string) *SentinelError {
	return &SentinelError{code: code, message: message}
}

func (e *SentinelError) Error() string {
	if e == nil {
		return "<nil>"
	}
	return e.message
}

// ErrorCode 返回信号所属类别；同类别的不同信号仍须通过 errors.Is 分别判断。
func (e *SentinelError) ErrorCode() ErrorCode {
	if e == nil {
		return CodeInternal
	}
	return e.code
}

// ErrorCode 使业务错误参与统一分类提取；Code 字段保留原有公开形态。
func (e *AgentError) ErrorCode() ErrorCode {
	if e == nil {
		return CodeInternal
	}
	return e.Code
}
