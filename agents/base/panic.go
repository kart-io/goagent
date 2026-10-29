package base

import (
	"errors"

	agentErrors "github.com/kart-io/goagent/errors"
)

// IsPanic 判断 err 链中是否含 *PanicError。降级分支（默认值、跳过、回退、重试）须先用它放行已恢复的 panic，
// 返回该错误由调用方中止本次执行，不能当作普通失败吞掉。
func IsPanic(err error) bool {
	var panicErr *agentErrors.PanicError
	return errors.As(err, &panicErr)
}
