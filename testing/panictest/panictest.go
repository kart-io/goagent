// Package panictest 在子进程中运行可能让进程退出的测试：回归时只有子进程崩溃，测试二进制本身照常报告失败。
package panictest

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"sync"
	"testing"
	"time"
)

const childEnv = "GOAGENT_PANICTEST_CHILD"

// survivedMarker 由子进程在 fn 返回后打印，父进程据此确认进程存活到最后。
const survivedMarker = "PANICTEST_CHILD_SURVIVED"

// Run 在子进程中重新执行当前测试并运行 fn，父进程断言子进程正常退出且没有未恢复的 panic。
// fn 内用子进程的 t 做断言；只能在顶层测试中调用，子进程只运行该测试本身。
func Run(t *testing.T, fn func(t *testing.T)) {
	t.Helper()
	if os.Getenv(childEnv) == t.Name() {
		fn(t)
		fmt.Println(survivedMarker)
		return
	}
	cmd := exec.Command(os.Args[0], "-test.run=^"+regexp.QuoteMeta(t.Name())+"$", "-test.v", "-test.count=1")
	cmd.Env = append(os.Environ(), childEnv+"="+t.Name())
	out, err := cmd.CombinedOutput()
	text := string(out)
	if err != nil || strings.Contains(text, "panic:") || !strings.Contains(text, survivedMarker) {
		t.Fatalf("子进程未正常存活：%v\n%s", err, text)
	}
}

// Event 是全局 PanicHandler 收到的一次 panic。
type Event struct {
	Component, Operation string
	Value                any
}

// Recorder 记录全局 PanicHandler 收到的 panic，传给 core.SetGlobalMetricsCollector 使用。
// 方法签名只用标准库类型，本包因此不依赖 core，core 自身的测试也能使用。
type Recorder struct {
	mu     sync.Mutex
	events []Event
}

func (r *Recorder) RecordPanic(_ context.Context, component, operation string, value any) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.events = append(r.events, Event{Component: component, Operation: operation, Value: value})
}

// Wait 等到至少收到 n 次 panic 或超时，返回已收到的副本。
func (r *Recorder) Wait(n int, timeout time.Duration) []Event {
	deadline := time.Now().Add(timeout)
	for {
		r.mu.Lock()
		events := append([]Event(nil), r.events...)
		r.mu.Unlock()
		if len(events) >= n || time.Now().After(deadline) {
			return events
		}
		time.Sleep(5 * time.Millisecond)
	}
}
