package multiagent

import (
	"testing"

	"github.com/stretchr/testify/require"

	agentErrors "github.com/kart-io/goagent/errors"
	"github.com/kart-io/goagent/workflow"
)

// 构造参数与执行输出的故障分类不同，宿主可据此决定修正配置或报告执行失败。
func TestMultiagentErrorsExposeConfigurationAndOutputCodes(t *testing.T) {
	for _, tc := range []struct {
		name, key string
		run       func() error
	}{
		{"空顺序图", "steps", func() error { _, err := Sequential[int](workflow.Config{}); return err }},
		{"空循环图", "steps", func() error { _, err := Loop[int](workflow.Config{}, LoopConfig[int]{}); return err }},
		{"无效路由轮数", "max_rounds", func() error { _, err := RouterSupervisor[int](workflow.Config{}, RouterConfig[int]{}); return err }},
		{"缺少路由函数", "next", func() error { _, _, err := GuardRoute[int](nil, []string{"member"}, ""); return err }},
		{"空成员工具", "members", func() error { _, err := SupervisorTools(); return err }},
	} {
		t.Run(tc.name, func(t *testing.T) {
			err := tc.run()
			require.Equal(t, agentErrors.CodeInvalidConfig, agentErrors.GetCode(err))
			require.Equal(t, "multiagent", agentErrors.GetComponent(err))
			require.Equal(t, tc.key, agentErrors.GetContext(err)["config_key"])
		})
	}
	_, err := encodeAgentResult(nil)
	require.Equal(t, agentErrors.CodeInvalidOutput, agentErrors.GetCode(err))
	require.Equal(t, "multiagent", agentErrors.GetComponent(err))
	require.Equal(t, "encode_agent_result", agentErrors.GetOperation(err))
}
