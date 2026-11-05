//go:build unix

package main

import (
	"os"
	"os/exec"
	"syscall"
)

// 与 harness/env.LocalExecutor 一样管理独立进程组，取消和结束时清理 Go 命令的后代。
func configureProcess(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error {
		if cmd.Process == nil {
			return os.ErrProcessDone
		}
		if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != syscall.ESRCH {
			return err
		}
		return os.ErrProcessDone
	}
}

func cleanupProcess(cmd *exec.Cmd) {
	if cmd.Process != nil {
		_ = syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL)
	}
}

func stopSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
