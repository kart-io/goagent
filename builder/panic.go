package builder

import agentErrors "github.com/kart-io/goagent/errors"

// recoverPanic 把 panic 转为 *agentErrors.PanicError 写入 *err，用于公开入口边界与宿主回调调用点。
// 必须直接 defer recoverPanic(...)，recover 才能生效、调用栈才指向 panic 发生处。
func recoverPanic(op string, err *error) {
	if v := recover(); v != nil {
		*err = agentErrors.NewPanicError(op, v)
	}
}
