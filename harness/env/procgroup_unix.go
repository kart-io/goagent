//go:build unix

package env

import (
	"os/exec"
	"syscall"
)

// isolateProcessGroup 让命令在独立进程组中运行，ctx 结束时结束整个进程组，
// 命令自己放到后台的子进程也会被结束。
func isolateProcessGroup(c *exec.Cmd) {
	c.SysProcAttr = &syscall.SysProcAttr{Setpgid: true}
	c.Cancel = func() error { return killProcessGroup(c) }
}

// killProcessGroup 结束命令所在的进程组，命令结束后清理遗留的后台子进程。
func killProcessGroup(c *exec.Cmd) error {
	if c.Process == nil {
		return nil
	}
	return syscall.Kill(-c.Process.Pid, syscall.SIGKILL)
}
