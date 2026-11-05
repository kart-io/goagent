//go:build !unix

package processgroup

import "os/exec"

// Configure 在非 Unix 平台保留 CommandContext 的父进程取消。
func Configure(*exec.Cmd) {}

// Kill 在非 Unix 平台不提供额外的进程树清理。
func Kill(*exec.Cmd) error { return nil }
