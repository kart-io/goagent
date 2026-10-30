package env

import "context"

// ReadOnly 包装 base，写入与删除一律返回 ErrReadOnly，读取照常。
func ReadOnly(base FileSystem) FileSystem { return readOnly{base} }

type readOnly struct{ FileSystem }

func (readOnly) WriteFile(context.Context, string, []byte, WriteIntent) (Version, error) {
	return "", ErrReadOnly
}

func (readOnly) Remove(context.Context, string) error { return ErrReadOnly }
