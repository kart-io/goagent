package durable_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/durable"
	"github.com/kart-io/goagent/core/durable/storetest"
)

// 同一目录上的两个 FileStore 各自打开锁文件，flock 在它们之间互斥，与两个进程等价。
func TestFileStoreConformance(t *testing.T) {
	storetest.Run(t, func(t *testing.T) (durable.Store, durable.Store) {
		dir := t.TempDir()
		a, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		b, err := durable.NewFileStore(dir)
		require.NoError(t, err)
		return a, b
	}, storetest.Options{Scoped: true})
}
