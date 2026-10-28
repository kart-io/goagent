// Package livemodeltest 收纳依赖 testing 的真实模型测试辅助函数，避免示例程序链接 testing。
package livemodeltest

import (
	"os"
	"testing"

	"github.com/kart-io/goagent/examples/internal/livemodel"
)

// Require 在未设置 GOAGENT_LIVE=1 时跳过测试，避免默认测试产生费用。
func Require(t testing.TB) *livemodel.Client {
	t.Helper()
	if os.Getenv("GOAGENT_LIVE") != "1" {
		t.Skip("设置 GOAGENT_LIVE=1 以调用真实模型")
	}
	c, err := livemodel.FromEnv()
	if err != nil {
		t.Fatal(err)
	}
	return c
}
