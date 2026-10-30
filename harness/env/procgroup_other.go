//go:build !unix

package env

import "os/exec"

func isolateProcessGroup(*exec.Cmd) {}

func killProcessGroup(*exec.Cmd) error { return nil }
