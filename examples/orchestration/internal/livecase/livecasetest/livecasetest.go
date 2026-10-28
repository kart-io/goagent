// Package livecasetest 收纳依赖 testing 的子进程测试辅助函数，避免示例程序链接 testing。
package livecasetest

import (
	"encoding/json"
	"errors"
	"os"
	"os/exec"
	"strings"
	"testing"

	"github.com/kart-io/goagent/examples/orchestration/internal/livecase"
)

// Child 以子进程重新运行当前测试二进制中的 testName，并要求其以 livecase.ChildExit 退出。
// env 追加到继承的环境变量之后，用来选择子进程模式。
func Child(t *testing.T, testName string, env ...string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), env...)
	b, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != livecase.ChildExit {
		t.Fatalf("子进程应以 %d 退出，实际 %v\n%s", livecase.ChildExit, err, b)
	}
	return string(b)
}

// Result 解析子进程输出中最后一行 RESULT=<json>。
func Result(t *testing.T, out string, v any) {
	t.Helper()
	i := strings.LastIndex(out, "RESULT=")
	if i < 0 {
		t.Fatalf("子进程没有输出 RESULT：\n%s", out)
	}
	line := strings.SplitN(out[i+len("RESULT="):], "\n", 2)[0]
	if err := json.Unmarshal([]byte(strings.TrimSpace(line)), v); err != nil {
		t.Fatalf("解析 RESULT 失败：%v\n%s", err, out)
	}
}
