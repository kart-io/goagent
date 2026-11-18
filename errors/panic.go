package errors

import "runtime/debug"

// PanicError is an error recovered from a panic, preserving the panic value and
// the call stack at the time of the panic; it can be identified with errors.As.
// Error contains only the location, not the panic value or call stack: error
// text may enter logs, events, or the model and should not leak implementation
// details. Use errors.As to get Value and Stack when debugging.
type PanicError struct {
	// Op describes where the panic occurred, for example "checkpoint decoding".
	Op    string
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return e.Op + " 发生 panic" }

// NewPanicError must be called at the deferred recover so that Stack points to
// where the panic occurred. When value is already a *PanicError (re-panicked
// after an inner recovery) it is returned as is, preserving the original
// location and call stack.
func NewPanicError(op string, value any) *PanicError {
	if p, ok := value.(*PanicError); ok {
		return p
	}
	return &PanicError{Op: op, Value: value, Stack: debug.Stack()}
}
