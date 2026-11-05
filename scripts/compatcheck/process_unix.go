//go:build unix

package main

import (
	"os"
	"syscall"
)

func stopSignals() []os.Signal { return []os.Signal{os.Interrupt, syscall.SIGTERM} }
