package errors

// CodedError lets domain errors provide their category explicitly, avoiding
// mixing control signals into Unwrap.
type CodedError interface {
	error
	ErrorCode() ErrorCode
}

// SentinelError is a fixed, categorized control signal whose text and category
// cannot be modified. It does not implement a category-based Is, so each
// object keeps its own sentinel identity.
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

// ErrorCode returns the category of the signal; different signals of the same
// category must still be checked individually with errors.Is.
func (e *SentinelError) ErrorCode() ErrorCode {
	if e == nil {
		return CodeInternal
	}
	return e.code
}

// ErrorCode lets business errors take part in unified category extraction; the
// Code field keeps its existing public form.
func (e *AgentError) ErrorCode() ErrorCode {
	if e == nil {
		return CodeInternal
	}
	return e.Code
}
