//go:build !unix

package env

import "os"

// 非 Unix 平台没有 uid 映射，DockerRunner 不传 --user。
func fileUID(os.FileInfo) uint32 { return uint32(os.Getuid()) }
