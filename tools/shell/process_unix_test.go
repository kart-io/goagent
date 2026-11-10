//go:build unix

package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/kart-io/goagent/interfaces"
)

func TestShellCancellationStopsDescendantsAndKeepsContextCause(t *testing.T) {
	for _, mode := range []string{"工具超时", "调用方超时", "调用方取消"} {
		t.Run(mode, func(t *testing.T) {
			dir := t.TempDir()
			marker, ready := filepath.Join(dir, "late-effect"), filepath.Join(dir, "ready")
			ctx := context.Background()
			cancel := func() {}
			timeout := 500 * time.Millisecond
			want := context.DeadlineExceeded
			switch mode {
			case "调用方超时":
				ctx, cancel = context.WithTimeout(ctx, timeout)
				timeout = 10 * time.Second
			case "调用方取消":
				ctx, cancel = context.WithCancel(ctx)
				timeout, want = 10*time.Second, context.Canceled
			}
			defer cancel()
			tool := NewShellTool([]string{"sh"}, timeout)
			type result struct {
				out *interfaces.ToolOutput
				err error
			}
			done := make(chan result, 1)
			start := time.Now()
			go func() {
				out, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{
					"command": "sh",
					"args":    []string{"-c", `(printf ready > "$1"; sleep 2; printf effect > "$2") & wait`, "shell-test", ready, marker},
				}})
				done <- result{out, err}
			}()
			var readyAt time.Time
			for {
				if _, err := os.Stat(ready); err == nil {
					readyAt = time.Now()
					break
				}
				select {
				case r := <-done:
					t.Fatalf("后代尚未就绪，不能将取消前未启动当作清理成功: %v", r.err)
				case <-time.After(10 * time.Millisecond):
				}
				if time.Since(start) > 5*time.Second {
					cancel()
					t.Fatal("测试后代没有发出就绪信号")
				}
			}
			if mode == "调用方取消" {
				cancel()
			}
			r := <-done
			if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
				t.Errorf("命令的后代持有输出管道导致超时失效: %s", elapsed)
			}
			if !errors.Is(r.err, want) || (r.out != nil && r.out.Success) {
				t.Errorf("超时或取消必须保留上下文错误且不能成功: out=%+v, err=%v", r.out, r.err)
			}
			// 从已确认后代就绪的时刻观察，避免启动调度延迟削弱迟到效果检查。
			if remaining := time.Until(readyAt.Add(2200 * time.Millisecond)); remaining > 0 {
				time.Sleep(remaining)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("超时或取消后子进程仍写入了文件: %v", err)
			}
		})
	}
}

func TestShellParentExitCleansBackgroundAndKeepsExitStatus(t *testing.T) {
	for _, exitCode := range []int{0, 7} {
		t.Run(fmt.Sprint(exitCode), func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "late-effect")
			tool := NewShellTool([]string{"sh"}, 10*time.Second)
			start := time.Now()
			out, err := tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]any{
				"command": "sh",
				"args":    []string{"-c", `(sleep 2; printf effect > "$1") & printf parent-done; exit "$2"`, "shell-test", marker, fmt.Sprint(exitCode)},
			}})
			if elapsed := time.Since(start); elapsed > 1500*time.Millisecond {
				t.Errorf("父命令结束后不应无限等待后台管道: %s", elapsed)
			}
			if out == nil || out.Success != (exitCode == 0) || (err == nil) != (exitCode == 0) {
				t.Fatalf("父命令退出状态丢失: out=%+v, err=%v", out, err)
			}
			result := out.Result.(map[string]any)
			if result["exit_code"] != exitCode || !strings.Contains(result["output"].(string), "parent-done") {
				t.Errorf("父命令输出或退出码丢失: %+v", result)
			}
			if exitCode != 0 {
				var exited *exec.ExitError
				if !errors.As(err, &exited) || exited.ExitCode() != exitCode {
					t.Errorf("必须保留 ExitError 错误链: %v", err)
				}
			}
			if remaining := time.Until(start.Add(2200 * time.Millisecond)); remaining > 0 {
				time.Sleep(remaining)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("父命令结束后后台进程仍写入了文件: %v", err)
			}
		})
	}
}

// 子进程默认只拿到最小环境，宿主的密钥类变量须显式透传才可见。
func TestShellDoesNotLeakHostEnvironment(t *testing.T) {
	t.Setenv("GOAGENT_TEST_SECRET", "s3cret")
	t.Setenv("GOAGENT_TEST_SHARED", "visible")
	for _, test := range []struct {
		name          string
		tool          *ShellTool
		shared, leaks bool
	}{
		{"默认", NewShellTool([]string{"env"}, 5*time.Second), false, false},
		{"显式透传", NewShellToolBuilder().WithAllowedCommands("env").WithEnv("GOAGENT_TEST_SHARED").Build(), true, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			out, err := test.tool.Invoke(context.Background(), &interfaces.ToolInput{Args: map[string]interface{}{"command": "env"}})
			if err != nil || !out.Success {
				t.Fatalf("执行失败: %v %v", err, out)
			}
			env := out.Result.(map[string]interface{})["output"].(string)
			if !strings.Contains(env, "PATH=") {
				t.Fatalf("应保留 PATH: %q", env)
			}
			if strings.Contains(env, "GOAGENT_TEST_SECRET") != test.leaks || strings.Contains(env, "GOAGENT_TEST_SHARED=visible") != test.shared {
				t.Fatalf("环境变量不符合预期: %q", env)
			}
		})
	}
}
