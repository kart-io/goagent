//go:build unix

package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"syscall"
	"testing"
	"time"

	"github.com/kart-io/goagent/codec/json"
)

func TestCompatcheckSignalHelperProcess(t *testing.T) {
	if os.Getenv("GOAGENT_COMPATCHECK_SIGNAL_HELPER") != "1" {
		return
	}
	flag.CommandLine = flag.NewFlagSet("compatcheck", flag.ExitOnError)
	os.Args = []string{"compatcheck", "-root", os.Getenv("COMPATCHECK_ROOT"), "-out", os.Getenv("COMPATCHECK_OUT"), "-timeout", os.Getenv("COMPATCHECK_TIMEOUT")}
	main()
}

func delayedEffectFixture(t *testing.T, root string) (started, allow, effect string) {
	t.Helper()
	started, allow, effect = filepath.Join(root, "started"), filepath.Join(root, "allow"), filepath.Join(root, "effect")
	// 让已启动的测试等待父进程放行副作用，避免依赖任意 sleep 来判定进程存活。
	writeFixture(t, root, "goagent-contrib/retrieval/value_test.go", fmt.Sprintf(`package consumer
import ("os"; "testing"; "time")
func TestDelayedEffect(t *testing.T) {
 if err := os.WriteFile(%q, []byte("started"), 0600); err != nil { t.Fatal(err) }
 for end := time.Now().Add(20*time.Second); time.Now().Before(end); {
  if _, err := os.Stat(%q); err == nil {
   if err := os.WriteFile(%q, []byte("取消后仍执行了副作用"), 0600); err != nil { t.Fatal(err) }
   return
  }
  time.Sleep(10*time.Millisecond)
 }
 t.Fatal("没有收到副作用放行信号")
}
`, started, allow, effect))
	return
}

func TestRunningCancellationStopsDescendantsAndKeepsEvidence(t *testing.T) {
	for _, mode := range []string{"interrupt", "terminate", "timeout"} {
		t.Run(mode, func(t *testing.T) {
			root := compatibilityFixture(t)
			started, allow, effect := delayedEffectFixture(t, root)
			out := filepath.Join(t.TempDir(), "report")
			tmp := filepath.Join(t.TempDir(), "temporary modfiles")
			if err := os.Mkdir(tmp, 0o700); err != nil {
				t.Fatal(err)
			}
			timeout := "15s"
			if mode == "timeout" {
				timeout = "5s"
			}
			ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
			defer cancel()
			cmd := exec.CommandContext(ctx, os.Args[0], "-test.run=^TestCompatcheckSignalHelperProcess$")
			cmd.Env = append(os.Environ(), "GOAGENT_COMPATCHECK_SIGNAL_HELPER=1", "COMPATCHECK_ROOT="+root, "COMPATCHECK_OUT="+out, "COMPATCHECK_TIMEOUT="+timeout, "TMPDIR="+tmp)
			log, err := os.Create(filepath.Join(t.TempDir(), "helper.log"))
			if err != nil {
				t.Fatal(err)
			}
			defer func() { _ = log.Close() }()
			cmd.Stdout, cmd.Stderr = log, log
			if err := cmd.Start(); err != nil {
				t.Fatal(err)
			}
			for end := time.Now().Add(15 * time.Second); ; time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(started); err == nil {
					break
				}
				if time.Now().After(end) {
					cancel()
					_ = cmd.Wait()
					t.Fatal("测试子进程没有启动")
				}
			}
			if mode != "timeout" {
				signal := os.Interrupt
				if mode == "terminate" {
					signal = syscall.SIGTERM
				}
				if err := cmd.Process.Signal(signal); err != nil {
					t.Fatal(err)
				}
			}
			if err := cmd.Wait(); err == nil || cmd.ProcessState.ExitCode() != 1 {
				t.Errorf("取消或超时必须退出 1: %v, %v", err, cmd.ProcessState)
			}
			if err := os.WriteFile(allow, []byte("continue"), 0o600); err != nil {
				t.Fatal(err)
			}
			for end := time.Now().Add(500 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(effect); err == nil {
					t.Error("验证命令结束后，测试后代仍产生了副作用")
					break
				}
			}
			data, err := os.ReadFile(filepath.Join(out, "summary.json"))
			if err != nil {
				t.Errorf("取消时丢失摘要: %v", err)
			} else {
				var report summary
				if err := json.Unmarshal(data, &report); err != nil || len(report.Modules) == 0 || report.Modules[0].Status != "failed" {
					t.Errorf("取消摘要没有保留失败: %s, %v", data, err)
				}
			}
			if dirs, err := filepath.Glob(filepath.Join(tmp, "goagent-compat-*")); err != nil || len(dirs) != 0 {
				t.Errorf("临时 modfile 未清理: %v, %v", dirs, err)
			}
		})
	}
}

func TestGoCommandDeadlineStopsDescendantBeforeItsOwnTimeout(t *testing.T) {
	root := compatibilityFixture(t)
	started, allow, effect := delayedEffectFixture(t, root)
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	cmd := goCommand(ctx, filepath.Join(root, "goagent-contrib/retrieval"), "test", "-mod=mod", "-timeout=20s", "./...")
	log, err := os.Create(filepath.Join(t.TempDir(), "command.log"))
	if err != nil {
		t.Fatal(err)
	}
	defer func() { _ = log.Close() }()
	cmd.Stdout, cmd.Stderr = log, log
	done := make(chan error, 1)
	go func() { done <- cmd.Run() }()
	for {
		if _, err := os.Stat(started); err == nil {
			break
		}
		select {
		case err := <-done:
			t.Fatalf("测试尚未启动命令就已退出: %v", err)
		case <-time.After(10 * time.Millisecond):
		}
	}
	if err := <-done; err == nil {
		t.Fatal("超时不能成功")
	}
	if err := os.WriteFile(allow, []byte("continue"), 0o600); err != nil {
		t.Fatal(err)
	}
	for end := time.Now().Add(500 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
		if _, err := os.Stat(effect); err == nil {
			t.Fatal("Go 命令超时后，测试后代仍产生了副作用")
		}
	}
}

// 启动阶段必须服从配置时限；外层看门狗仅用于让旧实现可结束并留下红证据。
func TestVerifyStartupDeadlineStopsDiscoveryAndVersion(t *testing.T) {
	for _, stage := range []string{"mod", "version"} {
		t.Run(stage, func(t *testing.T) {
			root := compatibilityFixture(t)
			realGo, err := exec.LookPath("go")
			if err != nil {
				t.Fatal(err)
			}
			bin := t.TempDir()
			started, allow, effect := filepath.Join(bin, "started"), filepath.Join(bin, "allow"), filepath.Join(bin, "effect")
			writeFixture(t, bin, "go", `#!/bin/sh
if [ "$1" != "$COMPATCHECK_BLOCK_STAGE" ]; then
 exec "$COMPATCHECK_REAL_GO" "$@"
fi
(
 : > "$COMPATCHECK_STARTED"
 while [ ! -f "$COMPATCHECK_ALLOW" ]; do sleep 0.01; done
 : > "$COMPATCHECK_EFFECT"
) &
wait
`)
			if err := os.Chmod(filepath.Join(bin, "go"), 0o700); err != nil {
				t.Fatal(err)
			}
			t.Setenv("PATH", bin+string(os.PathListSeparator)+os.Getenv("PATH"))
			t.Setenv("COMPATCHECK_BLOCK_STAGE", stage)
			t.Setenv("COMPATCHECK_REAL_GO", realGo)
			t.Setenv("COMPATCHECK_STARTED", started)
			t.Setenv("COMPATCHECK_ALLOW", allow)
			t.Setenv("COMPATCHECK_EFFECT", effect)
			ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
			defer cancel()
			begin := time.Now()
			err = verify(ctx, root, filepath.Join(t.TempDir(), "report"), time.Second)
			t.Logf("阶段=%s，耗时=%v，结果=%v", stage, time.Since(begin), err)
			if err == nil {
				t.Error("阻塞的启动阶段不能通过")
			}
			if ctx.Err() != nil {
				t.Error("配置超时未生效，最终由外层看门狗终止")
			}
			if _, err := os.Stat(started); err != nil {
				t.Fatalf("未到达指定阻塞点: %v", err)
			}
			writeFixture(t, bin, "allow", "允许后代执行")
			for end := time.Now().Add(200 * time.Millisecond); time.Now().Before(end); time.Sleep(10 * time.Millisecond) {
				if _, err := os.Stat(effect); err == nil {
					t.Fatal("启动阶段超时后，后代仍产生副作用")
				}
			}
		})
	}
}
