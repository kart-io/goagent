package store

import (
	"strconv"
	"strings"
)

// NamespaceKey 使用字节长度前缀编码；nil 与空数组表示同一根空间，空组件仍独立。
func NamespaceKey(namespace []string) string {
	var b strings.Builder
	for _, component := range namespace {
		b.WriteString(strconv.Itoa(len(component)))
		b.WriteByte(':')
		b.WriteString(component)
	}
	return b.String()
}
