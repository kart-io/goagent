package session_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kart-io/goagent/core/session"
	"github.com/kart-io/goagent/core/session/sessiontest"
)

func TestFileStoreConformance(t *testing.T) {
	sessiontest.Run(t, func(t *testing.T) (session.Store, session.Store) {
		// 同一目录上的两个实例，等价于两个进程。
		dir := t.TempDir()
		a, err := session.NewFileStore(dir)
		require.NoError(t, err)
		b, err := session.NewFileStore(dir)
		require.NoError(t, err)
		return a, b
	}, sessiontest.Options{})
}

func TestMemoryStoreConformance(t *testing.T) {
	sessiontest.Run(t, func(t *testing.T) (session.Store, session.Store) {
		m := session.NewMemoryStore()
		return m, m
	}, sessiontest.Options{})
}
