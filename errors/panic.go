package errors

import "runtime/debug"

// PanicError 是从 panic 恢复得到的错误，保留 panic 值与发生 panic 时的调用栈，可用 errors.As 识别。
// Error 只含位置，不含 panic 值与调用栈：错误文本可能进入日志、事件或交给模型，不应泄露实现细节；
// 排查时用 errors.As 取 Value 与 Stack。
type PanicError struct {
	// Op 描述发生 panic 的位置，例如 `工具 "lookup"`。
	Op    string
	Value any
	Stack []byte
}

func (e *PanicError) Error() string { return e.Op + " 发生 panic" }

// NewPanicError 必须在 defer 的 recover 处调用，Stack 才指向 panic 发生处。
// value 已是 *PanicError（内层恢复后重新抛出）时原样返回，保留最初的位置与调用栈。
func NewPanicError(op string, value any) *PanicError {
	if p, ok := value.(*PanicError); ok {
		return p
	}
	return &PanicError{Op: op, Value: value, Stack: debug.Stack()}
}
