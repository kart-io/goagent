package panictest

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"testing"
	"time"
)

const hungChildEnv = "GOAGENT_PANICTEST_HUNG_CHILD"

// TestHungChildHelper 是 TestCombinedOutputKillsHungChild 的子进程入口：打印标记后一直阻塞，模拟死锁的子进程。
func TestHungChildHelper(t *testing.T) {
	if os.Getenv(hungChildEnv) == "" {
		t.Skip("仅作为子进程运行")
	}
	fmt.Println("HUNG_CHILD_STARTED")
	time.Sleep(time.Hour)
}

// 子进程不退出时 CombinedOutput 按时限杀掉并回收它，返回已产生的输出与超时错误。
func TestCombinedOutputKillsHungChild(t *testing.T) {
	cmd := exec.Command(os.Args[0], "-test.run=^TestHungChildHelper$", "-test.v")
	cmd.Env = append(os.Environ(), hungChildEnv+"=1")
	start := time.Now()
	out, err := CombinedOutput(cmd, 2*time.Second)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("应返回超时错误，实际：%v\n%s", err, out)
	}
	if elapsed := time.Since(start); elapsed > 30*time.Second {
		t.Fatalf("超时后未及时返回：%v", elapsed)
	}
	if cmd.ProcessState == nil || cmd.ProcessState.Success() {
		t.Fatalf("子进程应已被杀掉并回收：%v", cmd.ProcessState)
	}
	if !strings.Contains(string(out), "HUNG_CHILD_STARTED") {
		t.Fatalf("应保留超时前的输出以便定位：\n%s", out)
	}
	if !strings.Contains(err.Error(), "TestHungChildHelper") {
		t.Fatalf("超时错误应包含子进程命令以便定位：%v", err)
	}
}

func TestCombinedOutputRejectsPresetOutput(t *testing.T) {
	cmd := exec.Command(os.Args[0])
	cmd.Stdout = os.Stdout
	if _, err := CombinedOutput(cmd, time.Second); err == nil {
		t.Fatal("Stdout 已设置时应返回错误")
	}
}

func TestChildTimeoutWithinTestDeadline(t *testing.T) {
	if got := ChildTimeout(t, time.Millisecond); got != time.Millisecond {
		t.Fatalf("应不超过 limit：%v", got)
	}
	got := ChildTimeout(t, 1000*time.Hour)
	if deadline, ok := t.Deadline(); ok && !time.Now().Add(got).Before(deadline) {
		t.Fatalf("应在测试截止时间前结束：%v", got)
	}
}

func TestRunChildSurvives(t *testing.T) {
	Run(t, func(t *testing.T) {})
}
