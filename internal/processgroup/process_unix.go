//go:build unix

// Package processgroup 为命令执行入口提供同一进程组的取消与清理。
package processgroup

import (
	"os"
	"os/exec"
	"syscall"
)

// Configure 配置由 exec.CommandContext 创建的命令；超时时终止整个进程组。
// 主动用 setsid 脱离进程组的后代不在此范围。
func Configure(cmd *exec.Cmd) {
	cmd.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	cmd.Cancel = func() error { return Kill(cmd) }
}

// Kill 清理命令及仍在同一组的后代。尚未启动或进程组不存在时返回 os.ErrProcessDone。
func Kill(cmd *exec.Cmd) error {
	if cmd.Process == nil {
		return os.ErrProcessDone
	}
	if err := syscall.Kill(-cmd.Process.Pid, syscall.SIGKILL); err != syscall.ESRCH {
		return err
	}
	return os.ErrProcessDone
}
