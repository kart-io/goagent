package livemodeltest

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestRequireSkipsWithoutOptIn(t *testing.T) {
	t.Setenv("GOAGENT_LIVE", "")
	ran := false
	t.Run("skip", func(t *testing.T) {
		Require(t)
		ran = true
	})
	require.False(t, ran, "未设置 GOAGENT_LIVE=1 时不得调用真实模型")
}
