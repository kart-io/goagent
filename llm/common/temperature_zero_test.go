package common_test

import (
	"testing"

	"github.com/kart-io/goagent/llm"
	"github.com/kart-io/goagent/llm/common"
	"github.com/stretchr/testify/require"
)

func TestConfiguredZeroTemperatureIsPreserved(t *testing.T) {
	configured := common.NewBaseProvider(llm.WithTemperature(0))
	require.Zero(t, configured.GetTemperature(0))
	copied := common.NewBaseProvider(common.ConfigToOptions(configured.Config)...)
	require.Zero(t, copied.GetTemperature(0), "配置转为选项后不能丢失显式零")
	require.Equal(t, 0.2, configured.GetTemperature(0.2))
	require.Equal(t, 0.7, common.NewBaseProvider().GetTemperature(0))
}
