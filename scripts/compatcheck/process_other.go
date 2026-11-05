//go:build !unix

package main

import (
	"os"
	"os/exec"
)

// 非 Unix 平台沿用 CommandContext 的父进程取消；进程组回归只在 Unix 运行。
func configureProcess(*exec.Cmd) {}

func cleanupProcess(*exec.Cmd) {}

func stopSignals() []os.Signal { return []os.Signal{os.Interrupt} }
