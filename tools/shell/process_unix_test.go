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
	for _, parentDeadline := range []bool{false, true} {
		name := "工具超时"
		if parentDeadline {
			name = "调用方超时"
		}
		t.Run(name, func(t *testing.T) {
			marker := filepath.Join(t.TempDir(), "late-effect")
			ctx := context.Background()
			timeout := 50 * time.Millisecond
			if parentDeadline {
				var cancel context.CancelFunc
				ctx, cancel = context.WithTimeout(ctx, timeout)
				defer cancel()
				timeout = 10 * time.Second
			}
			tool := NewShellTool([]string{"sh"}, timeout)
			start := time.Now()
			out, err := tool.Invoke(ctx, &interfaces.ToolInput{Args: map[string]any{
				"command": "sh",
				"args":    []string{"-c", `(sleep 2; printf effect > "$1") & wait`, "shell-test", marker},
			}})
			if elapsed := time.Since(start); elapsed > time.Second {
				t.Errorf("命令的后代持有输出管道导致超时失效: %s", elapsed)
			}
			if !errors.Is(err, context.DeadlineExceeded) || (out != nil && out.Success) {
				t.Errorf("超时必须保留上下文错误且不能成功: out=%+v, err=%v", out, err)
			}
			// 保留工作区直到后代原定的写入时间之后，检查没有迟到效果。
			if remaining := time.Until(start.Add(2200 * time.Millisecond)); remaining > 0 {
				time.Sleep(remaining)
			}
			if _, err := os.Stat(marker); !errors.Is(err, os.ErrNotExist) {
				t.Errorf("超时后子进程仍写入了文件: %v", err)
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
