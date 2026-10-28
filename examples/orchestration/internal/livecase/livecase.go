// Package livecase 收纳真实模型编排案例共用的宿主侧辅助函数。
package livecase

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"testing"

	"github.com/kart-io/goagent/interfaces"
	"github.com/kart-io/goagent/tools"
)

// DispatchTools 让工具经过 Dispatcher：获得 Schema 校验、调用日志与回放。
// 返回顺序与输入一致，便于宿主控制工具在提示中的排列。
func DispatchTools(ts []interfaces.Tool) ([]interfaces.Tool, error) {
	reg := tools.NewRegistry()
	for _, t := range ts {
		if err := reg.Register(t); err != nil {
			return nil, err
		}
	}
	d, err := tools.NewDispatcher(reg, tools.DispatchConfig{MaxConcurrency: 1})
	if err != nil {
		return nil, err
	}
	byName := map[string]interfaces.Tool{}
	for _, t := range d.Tools() {
		byName[t.Name()] = t
	}
	out := make([]interfaces.Tool, 0, len(ts))
	for _, t := range ts {
		out = append(out, byName[t.Name()])
	}
	return out, nil
}

var conclusion = regexp.MustCompile(`结论[:：]\s*(\S+?)(?:[\s，。,.;；]|$)`)

// Decision 从模型回答中取出“结论：X”，且 X 必须在允许集合内。
// 模型输出不可信，取不到合法结论就报错，不猜测。
func Decision(answer string, allowed ...string) (string, error) {
	m := conclusion.FindStringSubmatch(answer)
	if m == nil {
		return "", fmt.Errorf("回答缺少“结论：”行")
	}
	got := strings.Trim(m[1], "*`")
	for _, a := range allowed {
		if got == a {
			return got, nil
		}
	}
	return "", fmt.Errorf("结论 %q 不在允许值 %v 中", got, allowed)
}

// ChildExit 是子进程约定的退出码，用来区分“按计划结束或崩溃”与测试框架自身的失败。
const ChildExit = 86

// Child 以子进程重新运行当前测试二进制中的 testName，并要求其以 ChildExit 退出。
// env 追加到继承的环境变量之后，用来选择子进程模式。
func Child(t *testing.T, testName string, env ...string) string {
	t.Helper()
	cmd := exec.Command(os.Args[0], "-test.run=^"+testName+"$")
	cmd.Env = append(os.Environ(), env...)
	b, err := cmd.CombinedOutput()
	var exit *exec.ExitError
	if !errors.As(err, &exit) || exit.ExitCode() != ChildExit {
		t.Fatalf("子进程应以 %d 退出，实际 %v\n%s", ChildExit, err, b)
	}
	return string(b)
}

// ExitWithResult 由子进程调用：输出 RESULT=<json> 后以 ChildExit 退出。
func ExitWithResult(v any) {
	data, err := json.Marshal(v)
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(2)
	}
	fmt.Printf("\nRESULT=%s\n", data)
	os.Exit(ChildExit)
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
