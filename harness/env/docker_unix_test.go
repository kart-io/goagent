//go:build unix

package env

import (
	"os"
	"syscall"
)

func fileUID(info os.FileInfo) uint32 {
	return info.Sys().(*syscall.Stat_t).Uid
}
